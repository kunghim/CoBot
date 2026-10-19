package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine"
	"github.com/kunghim/CoBot/internal/roles"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// What a member reads with a card: the requirement, the route with the card's
// place on it, what the stage must deliver, the requirement document and what
// earlier stages handed off. A card carries everything it needs, so a member
// never has to piece the work together from messages.

const (
	maxExecutionCommands = 20
	maxExecutionFiles    = 40
)

// collectExecutionLocked is what the engine reported a card's owner ran and
// changed since it was handed the card: the commands with their exit codes
// and the files touched. It comes from the run steps, never from the model.
func collectExecutionLocked(ctx context.Context, q sqlite.Queryer, card domain.Task) (*domain.Execution, error) {
	rows, err := q.QueryContext(ctx, `SELECT s.kind, s.detail FROM run_steps s
		WHERE s.bot_id = ? AND s.kind IN (?, ?) AND s.started_at >= ? ORDER BY s.started_at, s.seq`,
		card.OwnerID, engine.StepCommand, engine.StepFileChange, card.CreatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	execution := domain.Execution{Commands: []domain.ExecutedCommand{}, Files: []string{}}
	seen := map[string]bool{}
	for rows.Next() {
		var kind, detail string
		if err := rows.Scan(&kind, &detail); err != nil {
			return nil, err
		}
		switch kind {
		case engine.StepCommand:
			var c struct {
				Command  string `json:"command"`
				ExitCode *int   `json:"exitCode"`
			}
			if json.Unmarshal([]byte(detail), &c) != nil || c.Command == "" {
				continue
			}
			execution.Commands = append(execution.Commands, domain.ExecutedCommand{Command: c.Command, ExitCode: c.ExitCode})
		case engine.StepFileChange:
			var f struct {
				Changes []struct {
					Path string `json:"path"`
				} `json:"changes"`
			}
			if json.Unmarshal([]byte(detail), &f) != nil {
				continue
			}
			for _, change := range f.Changes {
				if change.Path != "" && !seen[change.Path] {
					seen[change.Path] = true
					execution.Files = append(execution.Files, change.Path)
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// The latest commands say most about how the work ended.
	if n := len(execution.Commands); n > maxExecutionCommands {
		execution.Commands = execution.Commands[n-maxExecutionCommands:]
	}
	if len(execution.Files) > maxExecutionFiles {
		execution.Files = execution.Files[:maxExecutionFiles]
	}
	if len(execution.Commands) == 0 && len(execution.Files) == 0 {
		return nil, nil
	}
	return &execution, nil
}

// stageLabel names where a card sits, for the messages its handoff causes.
func stageLabel(req domain.Requirement, card domain.Task) string {
	if card.StageIndex < 0 {
		return "需求文档"
	}
	if card.StageIndex < len(req.Stages) {
		return fmt.Sprintf("第 %d 环「%s」", card.StageIndex+1, req.Stages[card.StageIndex].Name)
	}
	return "「" + card.Title + "」"
}

func memberNames(ctx context.Context, q sqlite.Queryer, teamID string) map[string]string {
	names := map[string]string{}
	if bots, err := sqlite.ListBots(ctx, q, teamID); err == nil {
		for _, bot := range bots {
			names[bot.ID] = bot.Name
		}
	}
	return names
}

func stageOwnerName(stage domain.Stage, names map[string]string) string {
	if stage.Kind == domain.StageConfirm {
		return "用户"
	}
	if name := names[stage.OwnerID]; name != "" {
		return name
	}
	return "已离开的成员"
}

// routeText is the route with the current position marked.
func routeText(req domain.Requirement, current int, names map[string]string) string {
	var b strings.Builder
	for i, stage := range req.Stages {
		marker := "  "
		if i == current {
			marker = "▶ "
		}
		kind := stageKindNames[stage.Kind]
		if stage.Kind == domain.StageReview {
			kind = reviewTypeNames[stage.ReviewType] + kind
		}
		fmt.Fprintf(&b, "%s%d. %s（%s，%s）\n", marker, i+1, stage.Name, kind, stageOwnerName(stage, names))
	}
	return b.String()
}

var outcomeNames = map[string]string{
	domain.HandoffDone: "完成", domain.HandoffBack: "退回", domain.HandoffEscalate: "上报", domain.HandoffResume: "交回继续",
}

// handoffsText lists what earlier cards of a requirement handed off, oldest
// first, with what the engine saw them run.
func handoffsText(ctx context.Context, q sqlite.Queryer, req domain.Requirement, names map[string]string) (string, error) {
	cards, err := sqlite.ListRequirementTasks(ctx, q, req.ID)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, card := range cards {
		if card.Status != domain.TaskDone {
			continue
		}
		records, err := sqlite.ListEvidence(ctx, q, card.ID)
		if err != nil {
			return "", err
		}
		for _, r := range records {
			author := names[r.AuthorID]
			if author == "" {
				author = "已离开的成员"
			}
			fmt.Fprintf(&b, "- %s · %s（%s，%s）：%s\n", stageLabel(req, card), card.Title, author, outcomeNames[r.Outcome], r.Summary)
			if r.Suggestions != "" {
				fmt.Fprintf(&b, "  建议：%s\n", r.Suggestions)
			}
			if len(r.ArtifactRefs) > 0 {
				fmt.Fprintf(&b, "  产出：%s\n", strings.Join(r.ArtifactRefs, "、"))
			}
			if r.Execution != nil {
				if len(r.Execution.Files) > 0 {
					fmt.Fprintf(&b, "  改动文件：%s\n", strings.Join(r.Execution.Files, "、"))
				}
				for _, c := range r.Execution.Commands {
					if c.ExitCode != nil {
						fmt.Fprintf(&b, "  运行：%s（退出码 %d）\n", c.Command, *c.ExitCode)
					} else {
						fmt.Fprintf(&b, "  运行：%s\n", c.Command)
					}
				}
			}
		}
	}
	return b.String(), nil
}

// handoffHelp says how a card of this kind is handed off.
func handoffHelp(card domain.Task, req domain.Requirement) string {
	id := card.ID
	switch card.StageKind {
	case domain.CardDraft:
		return fmt.Sprintf("改好后用 handoff({cardId:%q, outcome:\"done\", summary, document}) 交回，document 是修改后的完整需求文档。", id)
	case domain.CardEscalation:
		return fmt.Sprintf("处理好后用 handoff({cardId:%q, outcome:\"resume\", summary}) 把这一环交回给原负责人继续，summary 写你的处理意见；需要用户拍板的先用 ask_user 问用户。", id)
	case domain.StageAccept:
		return fmt.Sprintf("逐条核对需求文档和上面的交接，确认无误后用 handoff({cardId:%q, outcome:\"done\", summary}) 交出验收汇报，summary 就是给用户的汇报（做了什么、怎么查看、验证结果、遗留问题）。", id)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "完成后用 handoff({cardId:%q, outcome:\"done\", summary, artifactRefs?}) 交接，summary 写清做了什么、产出在哪、实际验证了什么、结果如何。", id)
	if card.StageIndex >= 0 && card.StageIndex < len(req.Stages) && req.Stages[card.StageIndex].ChecksWork() {
		b.WriteString("通过但有不阻塞的意见时加 suggestions。不通过用 outcome:\"back\"，summary 写明问题和怎样算改好，mode 选 \"rerun\"（改完按链路重新往下走）或 \"return_to_me\"（改完直接回到你这里）。")
	}
	b.WriteString("做不下去或需要项目经理决定时用 outcome:\"escalate\"，summary 写明卡在哪里。")
	return b.String()
}

// cardText is the message that hands a member a card.
func (a *App) cardText(ctx context.Context, q sqlite.Queryer, req domain.Requirement, card domain.Task, note string) (string, error) {
	names := memberNames(ctx, q, req.TeamID)
	var b strings.Builder
	fmt.Fprintf(&b, "【接力卡片】%s（cardId=%s）\n", card.Title, card.ID)
	fmt.Fprintf(&b, "需求：%s（requirementId=%s）\n", req.Title, req.ID)
	if card.StageIndex >= 0 {
		b.WriteString("\n链路：\n")
		b.WriteString(routeText(req, card.StageIndex, names))
	}
	if card.StageKind == domain.CardDraft {
		b.WriteString("\n这是需求文档的返工：按下面的意见修改需求文档。\n")
	}
	if card.ReturnTo >= 0 && card.ReturnTo < len(req.Stages) {
		fmt.Fprintf(&b, "\n这是返工卡片，交接后直接回到第 %d 环「%s」。\n", card.ReturnTo+1, req.Stages[card.ReturnTo].Name)
	}
	if note != "" {
		fmt.Fprintf(&b, "\n说明：%s\n", note)
	}
	if card.AcceptanceCriteria != "" {
		fmt.Fprintf(&b, "\n这一环的完成标准：\n%s\n", card.AcceptanceCriteria)
	}
	if card.StageKind == domain.StageReview {
		if checklist := roles.Checklist(card.ReviewType); checklist != "" {
			fmt.Fprintf(&b, "\n评审清单（%s）：\n%s\n", reviewTypeNames[card.ReviewType], checklist)
		}
	}
	fmt.Fprintf(&b, "\n需求文档：\n%s\n", req.Document)
	handoffs, err := handoffsText(ctx, q, req, names)
	if err != nil {
		return "", err
	}
	if handoffs != "" {
		b.WriteString("\n之前的交接：\n")
		b.WriteString(handoffs)
	}
	b.WriteString("\n")
	b.WriteString(handoffHelp(card, req))
	return b.String(), nil
}

// confirmText is the question a confirm stage puts to the user: what is to be
// confirmed, with the route so the user sees what follows.
func (a *App) confirmText(ctx context.Context, q sqlite.Queryer, req domain.Requirement, idx int, note string) (string, error) {
	names := memberNames(ctx, q, req.TeamID)
	var b strings.Builder
	fmt.Fprintf(&b, "## %s · %s\n\n", req.Title, req.Stages[idx].Name)
	if note != "" {
		fmt.Fprintf(&b, "%s\n\n", note)
	}
	if criteria := strings.TrimSpace(req.Stages[idx].Criteria); criteria != "" {
		fmt.Fprintf(&b, "请确认：%s\n\n", criteria)
	}
	if producer := producingStage(req, idx); producer < 0 {
		fmt.Fprintf(&b, "### 需求文档\n\n%s\n\n", req.Document)
	} else {
		cards, err := sqlite.ListRequirementTasks(ctx, q, req.ID)
		if err != nil {
			return "", err
		}
		for i := len(cards) - 1; i >= 0; i-- {
			if cards[i].StageIndex != producer || cards[i].Status != domain.TaskDone || cards[i].StageKind == domain.CardEscalation {
				continue
			}
			records, err := sqlite.ListEvidence(ctx, q, cards[i].ID)
			if err != nil {
				return "", err
			}
			if len(records) > 0 {
				r := records[len(records)-1]
				fmt.Fprintf(&b, "### %s的交付\n\n%s\n\n", req.Stages[producer].Name, r.Summary)
				if len(r.ArtifactRefs) > 0 {
					fmt.Fprintf(&b, "产出：%s\n\n", strings.Join(r.ArtifactRefs, "、"))
				}
			}
			break
		}
	}
	b.WriteString("### 链路\n\n")
	for _, line := range strings.Split(strings.TrimRight(routeText(req, idx, names), "\n"), "\n") {
		b.WriteString(line + "\n")
	}
	return b.String(), nil
}

// CardLine is one card as a member sees it in a list.
type CardLine struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Owner         string `json:"owner"`
	OwnerID       string `json:"ownerId"`
	Status        string `json:"status"`
	RequirementID string `json:"requirementId"`
	Stage         string `json:"stage"`
}

// RequirementView is a requirement as a member sees it: its route with the
// current position and its cards.
type RequirementView struct {
	ID       string     `json:"id"`
	Title    string     `json:"title"`
	Status   string     `json:"status"`
	Route    string     `json:"route"`
	Document string     `json:"document,omitempty"`
	Cards    []CardLine `json:"cards"`
}

// currentStage is the stage a requirement's work is at: the stage of its
// newest open card or open confirmation, else the end of the route.
func currentStage(ctx context.Context, q sqlite.Queryer, req domain.Requirement, cards []domain.Task) int {
	for i := len(cards) - 1; i >= 0; i-- {
		if !cards[i].IsTerminal() {
			return cards[i].StageIndex
		}
	}
	var idx int
	if err := q.QueryRowContext(ctx, `SELECT stage_index FROM questions WHERE requirement_id = ? AND status = ? ORDER BY created_at DESC LIMIT 1`,
		req.ID, domain.QuestionOpen).Scan(&idx); err == nil {
		return idx
	}
	return len(req.Stages)
}

func (a *App) requirementView(ctx context.Context, req domain.Requirement, names map[string]string, withDocument bool) (RequirementView, error) {
	cards, err := sqlite.ListRequirementTasks(ctx, a.DB, req.ID)
	if err != nil {
		return RequirementView{}, err
	}
	view := RequirementView{ID: req.ID, Title: req.Title, Status: req.Status, Cards: []CardLine{},
		Route: routeText(req, currentStage(ctx, a.DB, req, cards), names)}
	if withDocument {
		view.Document = req.Document
	}
	for _, card := range cards {
		view.Cards = append(view.Cards, CardLine{ID: card.ID, Title: card.Title, Owner: names[card.OwnerID], OwnerID: card.OwnerID,
			Status: card.Status, RequirementID: req.ID, Stage: stageLabel(req, card)})
	}
	return view, nil
}

// ViewTasks answers a member's look at the work: one card in full with its
// handoff records, one requirement with its document, route and cards, or
// every requirement still in progress.
func (a *App) ViewTasks(ctx context.Context, principal auth.Principal, requirementID, cardID string) (map[string]any, error) {
	if principal.Kind != auth.KindBot && principal.Kind != auth.KindUser {
		return nil, domain.Unauthorized("error.unauthorized.principal_required")
	}
	if cardID != "" {
		return a.GetTaskDetail(ctx, principal, cardID)
	}
	names := memberNames(ctx, a.DB, principal.TeamID)
	if requirementID != "" {
		req, err := sqlite.GetRequirement(ctx, a.DB, requirementID)
		if err != nil {
			return nil, sqlite.NormalizeError(err)
		}
		if req.TeamID != principal.TeamID {
			return nil, domain.Forbidden("error.message.other_team", map[string]any{"requirementId": requirementID})
		}
		view, err := a.requirementView(ctx, req, names, true)
		if err != nil {
			return nil, err
		}
		return map[string]any{"requirement": view}, nil
	}
	reqs, err := sqlite.ListRequirements(ctx, a.DB, principal.TeamID, "", 20)
	if err != nil {
		return nil, err
	}
	views := []RequirementView{}
	for _, req := range reqs {
		if req.Status == domain.RequirementDone || req.Status == domain.RequirementCancelled {
			continue
		}
		view, err := a.requirementView(ctx, req, names, false)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return map[string]any{"requirements": views}, nil
}

// ownCardsText lists the cards a member holds, for its turn context.
func (a *App) ownCardsText(ctx context.Context, teamID, botID string) string {
	cards, err := sqlite.ListTasks(ctx, a.DB, teamID, botID, 50)
	if err != nil {
		return ""
	}
	var b strings.Builder
	for i := len(cards) - 1; i >= 0; i-- {
		card := cards[i]
		if card.IsTerminal() {
			continue
		}
		status := "进行中"
		if card.Status == domain.TaskTodo {
			status = "刚交给你"
		}
		fmt.Fprintf(&b, "- 「%s」（cardId=%s）%s\n", card.Title, card.ID, status)
	}
	return b.String()
}
