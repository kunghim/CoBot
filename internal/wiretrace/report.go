package wiretrace

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

// Summary is the overview of one recording: who took part, how much passed
// over each channel, which Codex messages CoBot does not act on, and every
// error, warning, retry and refusal with the row it points to.
type Summary struct {
	Session      string         `json:"session"`
	Start        string         `json:"start"`
	End          string         `json:"end"`
	Seconds      int            `json:"seconds"`
	Rows         int            `json:"rows"`
	Members      []string       `json:"members"`
	Channels     []Count        `json:"channels"`
	Messages     []Count        `json:"messages"`
	Unhandled    []Count        `json:"unhandled"`
	UnknownItems []Count        `json:"unknownItems"`
	Reconnects   []Reconnect    `json:"reconnects"`
	Anomalies    []AnomalyGroup `json:"anomalies"`
}

// Count is how often something occurred, and its first row.
type Count struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
	First int    `json:"first"`
}

// Reconnect is one run of Codex retrying its model connection.
type Reconnect struct {
	Who      string `json:"who"`
	Start    string `json:"start"`
	Attempts int    `json:"attempts"`
	Seconds  int    `json:"seconds"`
	Outcome  string `json:"outcome"`
	First    int    `json:"first"`
}

// AnomalyGroup is one kind of trouble with the same text.
type AnomalyGroup struct {
	Kind  string `json:"kind"`
	Text  string `json:"text"`
	Who   string `json:"who"`
	Count int    `json:"count"`
	First int    `json:"first"`
	Time  string `json:"time"`
}

// ChannelNames reads a channel/direction pair for a person.
var ChannelNames = map[string]string{
	"codex.stdio cobot→codex": "CoBot → Codex",
	"codex.stdio codex→cobot": "Codex → CoBot",
	"codex.stderr":            "Codex 调试日志",
	"codex.process":           "Codex 进程",
	"mcp.stdio":               "Codex ⇄ 协作工具桥",
	"mcp.http":                "协作工具桥 ⇄ CoBot",
	"mcp.process":             "协作工具桥进程",
	"mcp.stderr":              "协作工具桥日志",
	"hook.stdin":              "安全检查（Codex 的提问）",
	"hook.stdout":             "安全检查（答复）",
	"hook.process":            "安全检查进程",
	"http":                    "HTTP 请求与响应",
	"cobot.meta":              "录像说明",
	"codex.rollout":           "Codex 本地会话记录",
}

func channelName(row Row) string {
	if name, ok := ChannelNames[row.Channel+" "+row.Dir]; ok {
		return name
	}
	if name, ok := ChannelNames[row.Channel]; ok {
		return name
	}
	return row.Channel
}

func countUp(counts map[string]*Count, order *[]string, name string, row int) {
	c, ok := counts[name]
	if !ok {
		c = &Count{Name: name, First: row}
		counts[name] = c
		*order = append(*order, name)
	}
	c.Count++
}

func sorted(counts map[string]*Count, order []string) []Count {
	out := make([]Count, 0, len(order))
	for _, name := range order {
		out = append(out, *counts[name])
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	return out
}

// Summarize builds the overview. handled and handledItems are the message
// and item types CoBot acts on (from the Codex adapter).
func (idx *Index) Summarize(handled, handledItems []string) Summary {
	s := Summary{Session: baseName(idx.Dir), Rows: len(idx.Rows)}
	if len(idx.Rows) > 0 {
		first, last := idx.Rows[0].at, idx.Rows[len(idx.Rows)-1].at
		s.Start, s.End = first.Format(time.DateTime), last.Format(time.DateTime)
		s.Seconds = int(last.Sub(first).Seconds())
	}
	channels, messages, unhandled, items := map[string]*Count{}, map[string]*Count{}, map[string]*Count{}, map[string]*Count{}
	var channelOrder, messageOrder, unhandledOrder, itemOrder []string
	members := map[string]bool{}
	anomalies := map[string]*AnomalyGroup{}
	var anomalyOrder []string
	type retryRun struct {
		r    *Reconnect
		last time.Time
		at   time.Time
	}
	open := map[string]*retryRun{}
	// A reconnect that starts before the turn produced anything kept the
	// user waiting from the turn's start, so it is timed from there.
	turnStart := map[string]time.Time{}
	turnWorked := map[string]bool{}
	for _, row := range idx.Rows {
		if row.Channel == "codex.stdio" && row.Dir == "codex→cobot" && row.thread != "" {
			switch row.method {
			case "turn/started":
				turnStart[row.thread], turnWorked[row.thread] = row.at, false
			case "item/started", "item/agentMessage/delta":
				// The user's own input is echoed as an item; it is not work.
				if row.itemType != "userMessage" {
					turnWorked[row.thread] = true
				}
			}
		}
		countUp(channels, &channelOrder, channelName(row), row.ID)
		if row.Who != "" {
			members[row.Who] = true
		}
		if row.Channel == "codex.stdio" && row.method != "" {
			countUp(messages, &messageOrder, row.method, row.ID)
			incoming := row.Dir == "codex→cobot" && !strings.HasPrefix(row.Kind, "↩")
			if incoming && !slices.Contains(handled, row.method) {
				countUp(unhandled, &unhandledOrder, row.method, row.ID)
			}
			if incoming && row.itemType != "" && !slices.Contains(handledItems, row.itemType) {
				countUp(items, &itemOrder, row.itemType, row.ID)
			}
		}
		// Retries of one thread form one reconnect; it ends with the next
		// message of that thread that is not a retry.
		if row.Channel == "codex.stdio" && row.Dir == "codex→cobot" && row.thread != "" {
			run := open[row.thread]
			switch {
			case row.retry && run == nil:
				start := row.at
				if at, ok := turnStart[row.thread]; ok && !turnWorked[row.thread] {
					start = at
				}
				run = &retryRun{r: &Reconnect{Who: row.Who, Start: start.Format("15:04:05.000"), First: row.ID}, at: start}
				open[row.thread] = run
				fallthrough
			case row.retry:
				run.r.Attempts++
				run.last = row.at
			case run != nil:
				run.r.Seconds = int(row.at.Sub(run.at).Seconds())
				run.r.Outcome = clip(row.Kind+" "+row.Summary, 80)
				s.Reconnects = append(s.Reconnects, *run.r)
				delete(open, row.thread)
			}
		}
		if row.Anomaly != "" && !row.retry {
			text := row.Summary
			if row.Channel == "codex.stderr" {
				text = row.status // the log message without its time and span
			}
			// Repeats that differ only in numbers (attempt 2/5, ids, delays)
			// are one kind of trouble; the first one stands for the group.
			key := row.Anomaly + "|" + row.Who + "|" + digits.ReplaceAllString(text, "#")
			group, ok := anomalies[key]
			if !ok {
				group = &AnomalyGroup{Kind: row.Anomaly, Text: text, Who: row.Who, First: row.ID, Time: row.Time}
				anomalies[key] = group
				anomalyOrder = append(anomalyOrder, key)
			}
			group.Count++
		}
	}
	for _, run := range open {
		run.r.Seconds = int(run.last.Sub(run.at).Seconds())
		run.r.Outcome = "录像结束时仍在重连"
		s.Reconnects = append(s.Reconnects, *run.r)
	}
	sort.Slice(s.Reconnects, func(i, j int) bool { return s.Reconnects[i].First < s.Reconnects[j].First })
	for name := range members {
		s.Members = append(s.Members, name)
	}
	sort.Strings(s.Members)
	s.Channels = sorted(channels, channelOrder)
	s.Messages = sorted(messages, messageOrder)
	s.Unhandled = sorted(unhandled, unhandledOrder)
	s.UnknownItems = sorted(items, itemOrder)
	for _, key := range anomalyOrder {
		s.Anomalies = append(s.Anomalies, *anomalies[key])
	}
	return s
}

var digits = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f-]{27}|[0-9]+(\.[0-9]+)?(ms|s)?`)

// logBody drops the timestamp and span prefix of a Codex log line (the
// spans end with "}: " and the target with ": ") so that repeats of one
// message group together.
func logBody(line string) string {
	if m := logLevel.FindStringIndex(line); m != nil {
		line = line[m[1]:]
	}
	if i := strings.LastIndex(line, "}: "); i >= 0 {
		line = line[i+3:]
	}
	if target, rest, ok := strings.Cut(line, ": "); ok && !strings.ContainsAny(target, " {") {
		line = rest
	}
	return line
}

func baseName(dir string) string {
	dir = strings.TrimRight(dir, `/\`)
	if i := strings.LastIndexAny(dir, `/\`); i >= 0 {
		return dir[i+1:]
	}
	return dir
}

// Markdown renders the overview as the report.md kept in the recording.
func (s Summary) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# 录像报告 %s\n\n%s — %s，共 %d 秒，%d 条记录。  \n成员：%s\n\n", s.Session, s.Start, s.End, s.Seconds, s.Rows, strings.Join(s.Members, "、"))
	table := func(title string, counts []Count, limit int) {
		fmt.Fprintf(&b, "## %s\n\n", title)
		if len(counts) == 0 {
			b.WriteString("无\n\n")
			return
		}
		b.WriteString("| 名称 | 次数 | 首次出现（记录号） |\n|---|---|---|\n")
		for i, c := range counts {
			if limit > 0 && i == limit {
				fmt.Fprintf(&b, "| …其余 %d 项 | | |\n", len(counts)-limit)
				break
			}
			fmt.Fprintf(&b, "| `%s` | %d | #%d |\n", c.Name, c.Count, c.First)
		}
		b.WriteString("\n")
	}
	table("CoBot 没有处理的 Codex 消息", s.Unhandled, 0)
	table("CoBot 不展示的条目类型", s.UnknownItems, 0)
	b.WriteString("## 重连\n\n")
	if len(s.Reconnects) == 0 {
		b.WriteString("无\n\n")
	}
	for _, r := range s.Reconnects {
		fmt.Fprintf(&b, "- %s %s：重试 %d 次，%d 秒后 %s（#%d）\n", r.Start, r.Who, r.Attempts, r.Seconds, r.Outcome, r.First)
	}
	b.WriteString("\n## 异常\n\n")
	if len(s.Anomalies) == 0 {
		b.WriteString("无\n\n")
	}
	for _, a := range s.Anomalies {
		fmt.Fprintf(&b, "- %s %s【%s】%s ×%d（#%d）\n", a.Time, a.Who, a.Kind, a.Text, a.Count, a.First)
	}
	b.WriteString("\n")
	table("各通道记录数", s.Channels, 0)
	table("CoBot 与 Codex 之间的消息类型", s.Messages, 0)
	return b.String()
}
