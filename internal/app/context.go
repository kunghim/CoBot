package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/roles"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// What a member's model is told, and when. Codex keeps the whole session
// history and resends it on every model call (cached by its unchanged
// prefix), so CoBot says each thing once: the role, the team and the tools
// go into the thread's developer instructions when the thread starts, and a
// turn only adds what changed since the member last heard. Nothing already
// said is repeated or rewritten.

// contextDigest records what a member's thread has already been told, so
// later turns can send only what changed. It is stored with the session.
type contextDigest struct {
	Roster   string `json:"roster,omitempty"`
	Tasks    string `json:"tasks,omitempty"`
	Memories string `json:"memories,omitempty"`
}

func digestOf(text string) string {
	if text == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:8])
}

func parseDigest(raw string) contextDigest {
	var d contextDigest
	_ = json.Unmarshal([]byte(raw), &d)
	return d
}

func (d contextDigest) String() string {
	raw, _ := json.Marshal(d)
	return string(raw)
}

// rosterText lists the team's members with the ids the tools take.
func (a *App) rosterText(ctx context.Context, teamID string) string {
	members, err := sqlite.ListBots(ctx, a.DB, teamID)
	if err != nil {
		return ""
	}
	var b strings.Builder
	for _, m := range members {
		role := "成员"
		if m.IsLeader {
			role = "队长"
		}
		fmt.Fprintf(&b, "- %s（%s，id=%s）\n", m.Name, role, m.ID)
	}
	return b.String()
}

var taskStatusNames = map[string]string{
	domain.TaskTodo: "待办", domain.TaskDoing: "进行中", domain.TaskReview: "待验收",
	domain.TaskDone: "已完成", domain.TaskCancelled: "已取消",
}

// ownTasksText lists a member's unfinished tasks, saying which still wait
// for another task, so a member woken early knows its work has not started.
func (a *App) ownTasksText(ctx context.Context, teamID, botID string) string {
	lines, err := a.taskLines(ctx, teamID, botID)
	if err != nil {
		return ""
	}
	var b strings.Builder
	for _, line := range lines {
		if line.Status == domain.TaskDone || line.Status == domain.TaskCancelled {
			continue
		}
		status := taskStatusNames[line.Status]
		if line.Status == domain.TaskTodo && len(line.WaitingOn) == 0 {
			status = "刚交给你" // it turns to doing as this turn starts
		}
		fmt.Fprintf(&b, "- 「%s」（id=%s）%s", line.Title, line.ID, status)
		if len(line.WaitingOn) > 0 {
			fmt.Fprintf(&b, "，要等「%s」完成后才会送到你手上，现在不要开工", strings.Join(line.WaitingOn, "」「"))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (a *App) memoriesText(ctx context.Context, teamID, botID string) string {
	var b strings.Builder
	for _, m := range a.memoriesForContext(ctx, teamID, botID).Items {
		fmt.Fprintf(&b, "- %s\n", m.Body)
	}
	return b.String()
}

// toolGuide names the collaboration tools of a role with their arguments,
// so the model calls them directly instead of first searching the tool list
// (in Codex's code mode the tool schemas are not part of the prompt).
func toolGuide(leader bool) string {
	var b strings.Builder
	b.WriteString("\n# 协作工具\n你通过 CoBot 的工具和团队协作。工具名是 mcp__cobot__<名字>，在代码里调用时写作 tools.mcp__cobot__<名字>(参数)，不需要先搜索工具列表。\n")
	if leader {
		b.WriteString("- create_task({title, description, acceptanceCriteria, ownerId, dependencyIds?})：派活。用户确认方案后再用。\n")
		b.WriteString("- review_task({taskId, decision: \"accept\" 或 \"reject\", comment?})：验收交付；打回时 comment 写明缺什么。\n")
	} else {
		b.WriteString("- deliver_task({taskId, summary, artifactRefs?})：交付你负责的任务；summary 写清做了什么、怎么运行或查看、实际验证和结果。\n")
	}
	b.WriteString("- send_message({targetBotIds, text, notifyOnly?})：给同事发消息。\n")
	b.WriteString("- view_tasks({taskId?})：查看任务；不传 taskId 列出全部任务。\n")
	b.WriteString("- ask_user({question, options?})：向用户提问，然后结束这一轮。\n")
	return b.String()
}

// threadInstructions is a member's developer instructions for a new thread,
// with the digest of what they told it.
func (a *App) threadInstructions(ctx context.Context, team domain.Team, bot domain.Bot, role roles.Role) (string, contextDigest) {
	roster := a.rosterText(ctx, team.ID)
	memories := a.memoriesText(ctx, team.ID, bot.ID)
	var b strings.Builder
	b.WriteString(role.Instructions)
	b.WriteString("\n\n# 团队\n")
	fmt.Fprintf(&b, "团队名称：%s\n", team.Name)
	fmt.Fprintf(&b, "团队工作目录：%s\n", team.Directory)
	fmt.Fprintf(&b, "你的名字：%s（角色：%s）\n", bot.Name, role.Name)
	b.WriteString("成员：\n")
	b.WriteString(roster)
	b.WriteString("和用户交流使用用户的语言。\n")
	if memories != "" {
		b.WriteString("\n# 你的记忆（工作方法与偏好）\n")
		b.WriteString(memories)
	}
	b.WriteString(toolGuide(bridgeRole(team, bot) == "leader"))
	b.WriteString(collaborationEtiquette)
	return b.String(), contextDigest{Roster: digestOf(roster), Memories: digestOf(memories)}
}

// turnContext is what a turn adds before the incoming messages: only what
// changed since the thread last heard (the team, the member's own tasks, its
// memories) and the notes colleagues left without waking it since its last
// turn. It returns the digest to store once the turn is accepted.
func (a *App) turnContext(ctx context.Context, bot domain.Bot, runID string, seen contextDigest) (string, contextDigest) {
	next := seen
	var b strings.Builder
	if roster := a.rosterText(ctx, bot.TeamID); digestOf(roster) != seen.Roster {
		b.WriteString("# 团队成员有变化，当前成员：\n")
		b.WriteString(roster)
		next.Roster = digestOf(roster)
	}
	if !bot.IsLeader {
		tasks := a.ownTasksText(ctx, bot.TeamID, bot.ID)
		if digestOf(tasks) != seen.Tasks {
			if tasks == "" {
				b.WriteString("# 你名下的任务都已结束\n")
			} else {
				b.WriteString("# 你名下的任务\n")
				b.WriteString(tasks)
			}
			next.Tasks = digestOf(tasks)
		}
	}
	if memories := a.memoriesText(ctx, bot.TeamID, bot.ID); digestOf(memories) != seen.Memories {
		b.WriteString("# 你的记忆有更新\n")
		b.WriteString(memories)
		next.Memories = digestOf(memories)
	}
	if notes := a.unseenNotes(ctx, bot, runID); notes != "" {
		b.WriteString("# 同事留给你的告知（不需要回复）\n")
		b.WriteString(notes)
	}
	return strings.TrimRight(b.String(), "\n"), next
}

// unseenNotes are the notify-only messages a member received since its
// previous turn started: they wake nobody, so a turn is where they are read.
func (a *App) unseenNotes(ctx context.Context, bot domain.Bot, runID string) string {
	since := ""
	_ = a.DB.QueryRowContext(ctx, `SELECT started_at FROM runs WHERE bot_id = ? AND id != ? ORDER BY started_at DESC LIMIT 1`, bot.ID, runID).Scan(&since)
	rows, err := a.DB.QueryContext(ctx, `SELECT id FROM messages WHERE bot_id = ? AND kind = ? AND delivery_status = '' AND source_bot_id != '' AND created_at > ? ORDER BY created_at`,
		bot.ID, domain.MessageKindCollaboration, since)
	if err != nil {
		return ""
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	names := map[string]string{}
	var b strings.Builder
	for _, id := range ids {
		message, err := sqlite.GetMessage(ctx, a.DB, id)
		if err != nil {
			continue
		}
		name, ok := names[message.SourceBotID]
		if !ok {
			if sender, err := sqlite.GetBot(ctx, a.DB, message.SourceBotID); err == nil {
				name = sender.Name
			}
			names[message.SourceBotID] = name
		}
		fmt.Fprintf(&b, "- 来自%s：%s\n", name, message.RenderText())
	}
	return b.String()
}
