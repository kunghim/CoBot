package wiretrace

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Row is one recorded message as the viewer lists it. The raw bytes stay
// in the recording file; a row only knows where they are and what can be
// read off them without keeping them.
type Row struct {
	ID      int    `json:"id"`
	Time    string `json:"t"`
	Channel string `json:"ch"`
	Dir     string `json:"dir"`
	From    string `json:"from"`
	To      string `json:"to"`
	// Kind is the message type: a JSON-RPC method, "↩ method" for its
	// response, an HTTP request line, a log level.
	Kind    string `json:"kind"`
	Summary string `json:"summary"`
	// Who is the member the message concerns, when one can be told.
	Who     string `json:"who,omitempty"`
	Anomaly string `json:"anomaly,omitempty"`
	// Fold groups consecutive streaming deltas of one item.
	Fold string `json:"fold,omitempty"`

	at       time.Time
	file     int
	offset   int64
	length   int
	seq      int64
	proc     string
	pid      int
	peer     string
	thread   string
	botID    string
	reqKey   string
	respKey  string
	method   string
	itemType string
	nameHint string // member name read off a thread/start request
	retry    bool
	status   string
	params   json.RawMessage
}

// Index lists every message of one recording in time order.
type Index struct {
	Dir   string
	Files []string
	Rows  []Row
}

// rpcMessage is the shape shared by Codex and MCP JSON-RPC lines.
type rpcMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

var (
	ansi         = regexp.MustCompile("\x1b\\[[0-9;]*m")
	logLevel     = regexp.MustCompile(`^\S+\s+(TRACE|DEBUG|INFO|WARN|ERROR)\s`)
	rolloutID    = regexp.MustCompile(`([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\.jsonl$`)
	memberInText = regexp.MustCompile(`你的名字：([^（\n]+)（角色：`)
	logThread    = regexp.MustCompile(`(?:thread_id|thread\.id|conversation\.id)=([0-9a-f]{8}-[0-9a-f-]{27})`)
)

// Load reads a recording folder: CoBot's per-process files and the session
// rollouts Codex wrote meanwhile.
func Load(dir string) (*Index, error) {
	idx := &Index{Dir: dir}
	traceFiles, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return nil, err
	}
	sort.Strings(traceFiles)
	var rollouts []string
	_ = filepath.WalkDir(filepath.Join(dir, "codex-home"), func(path string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && strings.HasPrefix(entry.Name(), "rollout-") && strings.HasSuffix(entry.Name(), ".jsonl") {
			rollouts = append(rollouts, path)
		}
		return nil
	})
	sort.Strings(rollouts)
	for _, path := range traceFiles {
		if err := idx.readFile(path, idx.traceRow); err != nil {
			return nil, err
		}
	}
	for _, path := range rollouts {
		thread := ""
		if m := rolloutID.FindStringSubmatch(path); m != nil {
			thread = m[1]
		}
		if err := idx.readFile(path, func(line []byte) (Row, bool) { return rolloutRow(line, thread) }); err != nil {
			return nil, err
		}
	}
	sort.SliceStable(idx.Rows, func(i, j int) bool {
		a, b := idx.Rows[i], idx.Rows[j]
		if !a.at.Equal(b.at) {
			return a.at.Before(b.at)
		}
		if a.file != b.file {
			return a.file < b.file
		}
		return a.seq < b.seq
	})
	for i := range idx.Rows {
		idx.Rows[i].ID = i
	}
	idx.label()
	return idx, nil
}

func (idx *Index) readFile(path string, parse func([]byte) (Row, bool)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	fileNo := len(idx.Files)
	idx.Files = append(idx.Files, path)
	reader := bufio.NewReaderSize(f, 1<<20)
	var offset int64
	for {
		line, err := reader.ReadBytes('\n')
		length := len(line)
		if trimmed := bytes.TrimRight(line, "\r\n"); len(trimmed) > 0 {
			if row, ok := parse(trimmed); ok {
				row.file, row.offset, row.length = fileNo, offset, len(trimmed)
				idx.Rows = append(idx.Rows, row)
			}
		}
		offset += int64(length)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// Raw returns the recorded bytes of one row exactly as they were sent:
// the "raw" of a CoBot entry, or the whole line of a Codex rollout.
func (idx *Index) Raw(id int) ([]byte, error) {
	if id < 0 || id >= len(idx.Rows) {
		return nil, fmt.Errorf("no row %d", id)
	}
	row := idx.Rows[id]
	f, err := os.Open(idx.Files[row.file])
	if err != nil {
		return nil, err
	}
	defer f.Close()
	line := make([]byte, row.length)
	if _, err := f.ReadAt(line, row.offset); err != nil {
		return nil, err
	}
	if row.Channel == "codex.rollout" {
		return line, nil
	}
	var entry Entry
	if err := json.Unmarshal(line, &entry); err != nil {
		return nil, err
	}
	if entry.RawBase64 != "" {
		return base64.StdEncoding.DecodeString(entry.RawBase64)
	}
	return []byte(entry.Raw), nil
}

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func (idx *Index) traceRow(line []byte) (Row, bool) {
	var entry Entry
	if json.Unmarshal(line, &entry) != nil {
		return Row{}, false
	}
	raw := []byte(entry.Raw)
	if entry.RawBase64 != "" {
		raw, _ = base64.StdEncoding.DecodeString(entry.RawBase64)
	}
	at, _ := time.Parse(time.RFC3339Nano, entry.Time)
	row := Row{
		Time: at.Format("15:04:05.000"), at: at, seq: entry.Seq,
		Channel: entry.Channel, Dir: entry.Dir, proc: entry.Proc, pid: entry.PID, peer: entry.Peer,
	}
	switch entry.Channel {
	case "codex.stdio", "mcp.stdio":
		parseRPC(&row, raw)
	case "codex.stderr":
		text := ansi.ReplaceAllString(string(raw), "")
		row.Kind = "log"
		if m := logLevel.FindStringSubmatch(text); m != nil {
			row.Kind = m[1]
		}
		if m := logThread.FindStringSubmatch(text); m != nil {
			row.thread = m[1]
		}
		if row.Kind == "ERROR" || row.Kind == "WARN" {
			row.Anomaly = "Codex 日志 " + row.Kind
			row.status = clip(logBody(text), 160)
		}
		row.Summary = clip(text, 200)
	case "http", "mcp.http":
		head, _, _ := strings.Cut(string(raw), "\n")
		head = strings.TrimSpace(head)
		row.Kind = entry.Dir
		row.Summary = clip(head, 160)
		if strings.HasPrefix(entry.Dir, "response") && strings.HasPrefix(head, "HTTP/") {
			if fields := strings.Fields(head); len(fields) > 1 {
				row.status = fields[1]
				if code, _ := strconv.Atoi(fields[1]); code >= 400 {
					row.Anomaly = "HTTP " + fields[1]
				}
			}
		}
		if entry.Dir == "response-chunk" {
			row.Summary = clip(string(raw), 160)
		}
	case "hook.stdin":
		var input struct {
			Session string          `json:"session_id"`
			Tool    string          `json:"tool_name"`
			Input   json.RawMessage `json:"tool_input"`
		}
		_ = json.Unmarshal(raw, &input)
		row.thread = input.Session
		row.Kind = "PreToolUse " + input.Tool
		row.Summary = clip(string(input.Input), 160)
	case "hook.stdout":
		row.Kind = "放行"
		if bytes.Contains(raw, []byte(`"deny"`)) {
			row.Kind = "拒绝"
			row.Anomaly = "安全检查拒绝"
		}
		row.Summary = clip(string(raw), 160)
	case "cobot.meta":
		var meta struct {
			ThreadID string `json:"threadId"`
			BotID    string `json:"botId"`
			BotName  string `json:"botName"`
		}
		_ = json.Unmarshal(raw, &meta)
		row.thread, row.botID, row.nameHint = meta.ThreadID, meta.BotID, meta.BotName
		row.Kind = entry.Dir
		row.Summary = clip(string(raw), 160)
	case "mcp.process":
		var meta struct {
			BotID string `json:"botId"`
		}
		_ = json.Unmarshal(raw, &meta)
		row.botID = meta.BotID
		row.Kind = entry.Dir
		row.Summary = clip(string(raw), 160)
	default: // codex.process, hook.process, mcp.stderr
		row.Kind = entry.Dir
		row.Summary = clip(string(raw), 160)
		if entry.Channel == "codex.process" && entry.Dir == "exit" && !bytes.Contains(raw, []byte(`"exitCode":0`)) {
			row.Anomaly = "Codex 进程异常退出"
		}
		if entry.Channel == "codex.process" && entry.Dir == "start-failed" {
			row.Anomaly = "Codex 进程启动失败"
		}
	}
	return row, true
}

// parseRPC reads a JSON-RPC line: its method, the id that pairs a request
// with its response, the thread it concerns, and a short summary.
func parseRPC(row *Row, raw []byte) {
	var msg rpcMessage
	if json.Unmarshal(raw, &msg) != nil {
		row.Kind = "无法解析"
		row.Anomaly = "无法解析的行"
		row.Summary = clip(string(raw), 160)
		return
	}
	id := string(msg.ID)
	scope := fmt.Sprintf("%s/%d/%s/%s", row.proc, row.pid, row.peer, row.Channel)
	switch {
	case msg.Method != "" && id != "":
		row.method, row.Kind = msg.Method, msg.Method
		row.reqKey = scope + "/" + row.Dir + "/" + id
	case msg.Method != "":
		row.method, row.Kind = msg.Method, msg.Method
	default:
		row.respKey = scope + "/" + id
		row.Kind = "↩ 回复"
		if len(msg.Error) > 0 {
			row.Anomaly = "请求出错"
			row.Summary = clip(string(msg.Error), 160)
		}
	}
	row.params = msg.Params
	var p struct {
		ThreadID string `json:"threadId"`
		ItemID   string `json:"itemId"`
		Delta    string `json:"delta"`
		Name     string `json:"name"`
		Input    []struct {
			Text string `json:"text"`
		} `json:"input"`
		Item struct {
			Type    string `json:"type"`
			ID      string `json:"id"`
			Command string `json:"command"`
			Text    string `json:"text"`
		} `json:"item"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message             string `json:"message"`
		Summary             string `json:"summary"`
		WillRetry           bool   `json:"willRetry"`
		DeveloperInstructns string `json:"developerInstructions"`
		Status              struct {
			Type string `json:"type"`
		} `json:"status"`
		Turn struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"turn"`
	}
	_ = json.Unmarshal(msg.Params, &p)
	row.thread = p.ThreadID
	if row.thread == "" && msg.Method == "thread/started" {
		var started struct {
			Thread struct {
				ID string `json:"id"`
			} `json:"thread"`
		}
		_ = json.Unmarshal(msg.Params, &started)
		row.thread = started.Thread.ID
	}
	row.itemType = p.Item.Type
	if m := memberInText.FindStringSubmatch(p.DeveloperInstructns); m != nil {
		row.nameHint = strings.TrimSpace(m[1])
	}
	switch {
	case msg.Method == "turn/start" || msg.Method == "turn/steer":
		var texts []string
		for _, input := range p.Input {
			texts = append(texts, input.Text)
		}
		row.Summary = clip(strings.Join(texts, " "), 160)
	case strings.HasSuffix(msg.Method, "/delta") || strings.HasSuffix(msg.Method, "Delta"):
		row.Summary = clip(p.Delta, 160)
		row.Fold = msg.Method + "|" + p.ThreadID + "|" + p.ItemID
	case msg.Method == "item/started" || msg.Method == "item/completed":
		row.Summary = clip(strings.TrimSpace(p.Item.Type+" "+p.Item.Command+" "+p.Item.Text), 160)
	case msg.Method == "error":
		row.Summary = clip(p.Error.Message, 160)
		row.retry = p.WillRetry
		row.Anomaly = "Codex 报错"
		if p.WillRetry {
			row.Anomaly = "重连"
		}
	case msg.Method == "warning":
		row.Summary = clip(p.Message, 160)
		row.Anomaly = "Codex 警告"
	case msg.Method == "deprecationNotice":
		row.Summary = clip(p.Summary, 160)
		row.Anomaly = "过时提示"
	case msg.Method == "thread/status/changed":
		row.Summary = p.Status.Type
	case msg.Method == "turn/started" || msg.Method == "turn/completed":
		row.Summary = strings.TrimSpace(p.Turn.ID + " " + p.Turn.Status)
	case msg.Method == "tools/call":
		row.Summary = p.Name
	case row.Summary == "":
		row.Summary = clip(string(msg.Params), 160)
		if msg.Method == "" {
			row.Summary = clip(string(msg.Result), 160)
		}
	}
}

func rolloutRow(line []byte, thread string) (Row, bool) {
	var record struct {
		Timestamp string `json:"timestamp"`
		Type      string `json:"type"`
		Payload   struct {
			Type string `json:"type"`
			Role string `json:"role"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &record) != nil {
		return Row{}, false
	}
	at, _ := time.Parse(time.RFC3339Nano, record.Timestamp)
	at = at.Local()
	kind := record.Type
	if record.Payload.Type != "" {
		kind += "/" + record.Payload.Type
	}
	if record.Payload.Role != "" {
		kind += " " + record.Payload.Role
	}
	return Row{Time: at.Format("15:04:05.000"), at: at, Channel: "codex.rollout", Dir: "Codex 本地记录", Kind: kind, Summary: clip(string(line), 160), thread: thread}, true
}

// label works out both ends of every row and the member it concerns, from
// what the recording itself says: CoBot's thread bindings, the member name
// in a thread/start request, a bridge's bot, a hook's session id.
func (idx *Index) label() {
	threadName := map[string]string{}
	botName := map[string]string{}
	requests := map[string]*Row{}
	for i := range idx.Rows {
		row := &idx.Rows[i]
		if row.Channel == "cobot.meta" && row.thread != "" {
			threadName[row.thread] = row.nameHint
			botName[row.botID] = row.nameHint
		}
		if row.reqKey != "" {
			requests[row.reqKey] = row
		}
	}
	// A thread/start response names the thread its request was for.
	for i := range idx.Rows {
		row := &idx.Rows[i]
		if row.respKey == "" {
			continue
		}
		requestDir := "cobot→codex"
		if row.Dir == "cobot→codex" {
			requestDir = "codex→cobot"
		}
		if row.Channel == "mcp.stdio" {
			requestDir = "codex→bridge"
			if row.Dir == "codex→bridge" {
				requestDir = "bridge→codex"
			}
		}
		key := strings.Replace(row.respKey, "/"+row.Channel+"/", "/"+row.Channel+"/"+requestDir+"/", 1)
		request, ok := requests[key]
		if !ok {
			continue
		}
		row.method = request.method
		row.Kind = "↩ " + request.method
		if row.thread == "" {
			row.thread = request.thread
		}
		if request.method == "thread/start" && request.nameHint != "" {
			raw, err := idx.Raw(row.ID)
			if err == nil {
				var res struct {
					Result struct {
						Thread struct {
							ID string `json:"id"`
						} `json:"thread"`
					} `json:"result"`
				}
				if json.Unmarshal(raw, &res) == nil && res.Result.Thread.ID != "" {
					row.thread = res.Result.Thread.ID
					if _, known := threadName[row.thread]; !known {
						threadName[row.thread] = request.nameHint
					}
				}
			}
		}
	}
	bridgeBot := map[string]string{} // proc/pid -> bot name
	for i := range idx.Rows {
		row := &idx.Rows[i]
		if row.Channel == "mcp.process" && row.botID != "" {
			bridgeBot[fmt.Sprint(row.pid)] = botName[row.botID]
		}
	}
	codex := func(row *Row) string {
		if name := threadName[row.thread]; name != "" {
			return name + "(Codex)"
		}
		if row.nameHint != "" { // a thread/start request, before the thread has an id
			return row.nameHint + "(Codex)"
		}
		if row.thread != "" {
			return "Codex·" + row.thread[:min(8, len(row.thread))]
		}
		return "Codex"
	}
	for i := range idx.Rows {
		row := &idx.Rows[i]
		row.Who = threadName[row.thread]
		if row.Who == "" && row.Channel == "codex.stdio" {
			row.Who = row.nameHint
		}
		switch row.Channel {
		case "codex.stdio":
			if row.Dir == "cobot→codex" {
				row.From, row.To = "CoBot", codex(row)
			} else {
				row.From, row.To = codex(row), "CoBot"
			}
		case "codex.stderr":
			row.From, row.To = "Codex 日志", "CoBot"
		case "codex.process":
			row.From, row.To = "CoBot", "Codex 进程"
		case "mcp.stdio", "mcp.stderr", "mcp.process", "mcp.http":
			bridge := "协作工具桥"
			if name := bridgeBot[fmt.Sprint(row.pid)]; name != "" {
				bridge = name + "的协作工具桥"
				row.Who = name
			}
			switch {
			case row.Channel == "mcp.http" && row.Dir == "request":
				row.From, row.To = bridge, "CoBot"
			case row.Channel == "mcp.http":
				row.From, row.To = "CoBot", bridge
			case row.Dir == "codex→bridge":
				row.From, row.To = "Codex", bridge
			default:
				row.From, row.To = bridge, "Codex"
			}
		case "hook.stdin":
			row.From, row.To = codex(row), "安全检查"
		case "hook.stdout", "hook.process":
			row.From, row.To = "安全检查", "Codex"
		case "http":
			if row.Dir == "request" {
				row.From, row.To = "客户端", "CoBot"
			} else {
				row.From, row.To = "CoBot", "客户端"
			}
		case "cobot.meta":
			row.From, row.To = "CoBot", "录像说明"
		case "codex.rollout":
			row.From, row.To = codex(row), "Codex 本地记录"
		default:
			row.From, row.To = row.proc, row.Channel
		}
	}
	// A hook answer belongs to the member whose question it answers: the
	// hook process wrote both, in order.
	lastHookWho := map[int]string{}
	for i := range idx.Rows {
		row := &idx.Rows[i]
		if row.Channel == "hook.stdin" {
			lastHookWho[row.pid] = row.Who
		} else if strings.HasPrefix(row.Channel, "hook.") && row.Who == "" {
			row.Who = lastHookWho[row.pid]
		}
	}
}
