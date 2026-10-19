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
			role = "项目经理"
		}
		fmt.Fprintf(&b, "- %s（%s，id=%s）\n", m.Name, role, m.ID)
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
func toolGuide(role string) string {
	var b strings.Builder
	b.WriteString("\n# 协作工具\n你通过 CoBot 的工具协作。工具名是 mcp__cobot__<名字>，在代码里调用时写作 tools.mcp__cobot__<名字>(参数)，不需要先搜索工具列表。\n")
	if role == "leader" {
		b.WriteString("- start_requirement({title, document, stages})：提交需求文档和链路。stages 按顺序列出每一环：{name, kind, ownerId?, reviewType?, criteria}。" +
			"kind 取值：confirm（用户确认，不填 ownerId）、design（产品设计）、review（评审，必须填 reviewType：requirements、design、test_plan、code 或 general）、" +
			"test_cases（测试用例）、implement（实现）、test（测试执行）、accept（你的验收汇报，固定为最后一环，不填 ownerId）。" +
			"第一环必须是 confirm，前面最多只能有一环需求评审；除 confirm 和 accept 外每一环都要指定一名成员。\n")
	}
	b.WriteString("- handoff({cardId, outcome, summary, artifactRefs?, mode?, suggestions?, document?})：交出手上的卡片。\n")
	b.WriteString("  - outcome=\"done\"：完成，系统按链路交给下一环；评审或测试通过但有可选建议时写在 suggestions 里（带建议通过）。\n")
	b.WriteString("  - outcome=\"back\"：退回（只有评审和测试环节可用），系统交给产出这项工作的那一环；mode 填 \"rerun\"（改完重新往下走）或 \"return_to_me\"（改完直接回到你）；summary 写清缺什么、在哪里、怎样算改好。\n")
	if role == "leader" {
		b.WriteString("  - outcome=\"resume\"：处理完上报卡后交回原环节继续，summary 写处理意见。\n")
		b.WriteString("  - 需求文档的返工卡完成时，document 填修改后的完整需求文档。验收汇报卡完成时，summary 就是给用户的验收汇报。\n")
	} else {
		b.WriteString("  - outcome=\"escalate\"：上报项目经理，summary 写明卡在哪里。\n")
	}
	b.WriteString("- view_tasks({requirementId?, cardId?})：查看需求和卡片；不传参数列出进行中的需求。\n")
	b.WriteString("- send_message({targetBotIds, text})：给同事留言，只用于反馈和告知，对方不会被唤醒；不要用它派活、催人或代替交接。\n")
	if role == "leader" {
		b.WriteString("- ask_user({question, options?})：向用户提问，然后结束这一轮，用户回答后你会被唤醒。\n")
	} else {
		b.WriteString("- ask_user({question, options?})：只用于只有用户能决定的问题，然后结束这一轮，用户的回答会送回给你。需求范围的变化不要用它，用 handoff 上报项目经理。\n")
	}
	return b.String()
}

// threadInstructions is a member's developer instructions for a new thread,
// with the digest of what they told it. The order goes from the most shared
// to the most personal: the platform layer every member reads, the tools of
// its place in the team, its role, its team, and its memories.
func (a *App) threadInstructions(ctx context.Context, team domain.Team, bot domain.Bot, role roles.Role) (string, contextDigest) {
	roster := a.rosterText(ctx, team.ID)
	memories := a.memoriesText(ctx, team.ID, bot.ID)
	place := bridgeRole(team, bot)
	var b strings.Builder
	b.WriteString(platformLayer)
	if place != "leader" {
		b.WriteString(directRequestLayer)
	}
	b.WriteString(toolGuide(place))
	b.WriteString("\n# 你的角色\n")
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
	return b.String(), contextDigest{Roster: digestOf(roster), Memories: digestOf(memories)}
}

// turnContext is what a turn adds before the incoming messages: only what
// changed since the thread last heard (the team, the member's own cards, its
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
	if cards := a.ownCardsText(ctx, bot.TeamID, bot.ID); digestOf(cards) != seen.Tasks {
		if cards == "" {
			b.WriteString("# 你手上的卡片都已交接\n")
		} else {
			b.WriteString("# 你手上的卡片\n")
			b.WriteString(cards)
		}
		next.Tasks = digestOf(cards)
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
