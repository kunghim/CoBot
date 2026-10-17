package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine"
	"github.com/kunghim/CoBot/internal/engine/codex"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// Risk evaluation (G03): when a chain's next wake falls inside the risk
// zone, the model judges it in a separate read-only session that has no
// collaboration tools and is not a team member. Only a structured "continue"
// lets the wake proceed; "ask", "unknown", a parse failure, a crashed eval
// or a timeout all fall back to the user's confirmation card. The hard limit
// is never presented to the model: over-limit wakes skip the eval and go
// straight to the user.

// riskEvalEvalInstructions is the eval thread's role. It may judge; it may
// not act.
const riskEvalInstructions = "你是协作链风险评估器。你只输出一行 JSON 判断，不执行任何操作、不使用任何工具。"

// riskEvalTimeout bounds one evaluation; past it the user decides.
const riskEvalTimeout = 90 * time.Second

// riskEval tracks one in-flight evaluation thread.
type riskEval struct {
	TeamID     string
	ChainID    string
	DeliveryID string
	BotID      string
	Text       strings.Builder
}

// EvalThreadTeam reports whether a thread belongs to an in-flight risk
// evaluation and which team it belongs to; usage events for such threads are
// booked as risk_eval, never as a member's turn (G06).
func (s *Scheduler) EvalThreadTeam(threadID string) (string, bool) {
	s.evalMu.Lock()
	defer s.evalMu.Unlock()
	e, ok := s.evals[threadID]
	if !ok {
		return "", false
	}
	return e.TeamID, true
}

func (s *Scheduler) riskEvalFor(threadID string) *riskEval {
	s.evalMu.Lock()
	defer s.evalMu.Unlock()
	return s.evals[threadID]
}

// StartRiskEval judges one chain wake in a fresh read-only session. It runs
// inline with the dispatch loop (engine calls already do), while its
// completion arrives asynchronously.
func (s *Scheduler) StartRiskEval(ctx context.Context, delivery sqlite.DeliveryRecord) {
	chain, err := sqlite.GetChain(ctx, s.app.DB, delivery.ChainID)
	if err != nil || chain.Status != domain.ChainActive {
		return // the chain moved on; nothing to judge
	}
	// Mark the chain first: any other queued delivery of this chain now
	// skips the gate instead of starting a second evaluation.
	tx, err := s.app.DB.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	if err := sqlite.SetChainStatus(ctx, tx, chain.ID, domain.ChainEvaluating, ""); err != nil {
		tx.Rollback()
		return
	}
	if err := tx.Commit(); err != nil {
		return
	}

	team, directory, err := sqlite.GetTeam(ctx, s.app.DB, chain.TeamID)
	if err != nil {
		s.riskEvalFallback(ctx, chain, delivery, "team_unavailable")
		return
	}
	server, err := s.EnsureTeamServer(ctx, team.ID, directory)
	if err != nil {
		s.riskEvalFallback(ctx, chain, delivery, "engine_unavailable")
		return
	}
	settings, _ := s.app.GetSettings(ctx)
	model := settings.EvalModel
	if model == "" {
		model = settings.DefaultModel
	}
	// The eval session is deliberately not a member: read-only sandbox, no
	// collaboration tools, no approvals, and it never enters provider_sessions.
	info, err := server.StartThread(ctx, engine.ThreadSpec{
		Cwd: directory, Sandbox: "read-only", ApprovalPolicy: "never",
		Model: model, DeveloperInstructions: riskEvalInstructions,
		Config: codex.ThreadConfig(nil),
	})
	if err != nil {
		s.riskEvalFallback(ctx, chain, delivery, "thread_failed")
		return
	}
	s.evalMu.Lock()
	s.evals[info.ThreadID] = &riskEval{
		TeamID: chain.TeamID, ChainID: chain.ID, DeliveryID: delivery.ID, BotID: delivery.TargetBotID,
	}
	s.evalMu.Unlock()

	// A hung eval must not strand the chain: past the timeout the user
	// decides instead.
	threadID := info.ThreadID
	// The watchdog outlives the dispatch context that started the turn: a
	// hung eval must still resolve the chain.
	go func() {
		timer := time.NewTimer(riskEvalTimeout)
		defer timer.Stop()
		select {
		case <-timer.C:
			if s.riskEvalFor(threadID) != nil {
				s.logger.Warn("risk evaluation timed out; asking the user", "threadId", threadID)
				s.finishRiskEval(threadID, "ask", "timeout")
			}
		case <-s.stop:
		}
	}()

	prompt := s.riskEvalPrompt(ctx, chain, delivery)
	if _, err := server.StartTurn(ctx, threadID, prompt, engine.TurnOptions{Model: model}); err != nil {
		// Turn start failed outright: the user decides.
		s.finishRiskEval(threadID, "ask", "turn_start_failed")
	}
}

// riskEvalPrompt is the whole model interface: chain facts, the pending
// message, and the exact JSON answer shape.
func (s *Scheduler) riskEvalPrompt(ctx context.Context, chain domain.Chain, delivery sqlite.DeliveryRecord) string {
	message, err := sqlite.GetMessage(ctx, s.app.DB, delivery.MessageID)
	text := ""
	if err == nil {
		text = message.RenderText()
	}
	if runes := []rune(text); len(runes) > 300 {
		text = string(runes[:300]) + "…"
	}
	return "一条协作链即将进行第 " + strconv.FormatInt(chain.WakeCount+1, 10) + " 次唤醒（链上限 " + strconv.FormatInt(chain.LimitCount, 10) +
		"，风险区从第 " + strconv.FormatInt(chain.RiskStart, 10) + " 次开始）。\n待唤醒成员：" + delivery.TargetBotID +
		"\n触发消息：" + text +
		"\n仅输出一行 JSON：{\"decision\":\"continue\" 或 \"ask\",\"reason\":\"一句话理由\"}。" +
		"continue 表示链可以按当前方向继续；ask 表示应交由用户确认。"
}

// onRiskEvalNotification buffers the eval's answer and decides on completion.
func (s *Scheduler) onRiskEvalNotification(epoch string, n engine.Notification) {
	e := s.riskEvalFor(n.ThreadID)
	if e == nil {
		return
	}
	switch n.Method {
	case "thread/tokenUsage/updated":
		// The eval's spend is real usage of the team's budget (G06); the
		// run-shaped ingest books it as risk_eval through this thread map.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := s.app.ingestTokenUsage(ctx, e.TeamID, epoch, n.TokenUsage); err != nil {
			s.logger.Warn("record risk-eval usage", "error", err)
		}
		cancel()
	case "item/completed":
		if n.ItemType == "agentMessage" {
			s.evalMu.Lock()
			e.Text.WriteString(n.Text)
			s.evalMu.Unlock()
		}
	case "turn/completed":
		s.evalMu.Lock()
		answer := e.Text.String()
		s.evalMu.Unlock()
		switch n.TurnStatus {
		case "failed", "interrupted":
			s.finishRiskEval(n.ThreadID, "ask", "eval_"+n.TurnStatus)
		default:
			decision, reason := parseRiskDecision(answer)
			s.finishRiskEval(n.ThreadID, decision, reason)
		}
	}
}

// parseRiskDecision extracts the model's structured answer. Anything that is
// not a clean "continue" is treated as "ask": the fallback is the user, and
// the hard limit is never overridden either way.
func parseRiskDecision(answer string) (decision, reason string) {
	text := strings.TrimSpace(answer)
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start >= 0 && end > start {
		text = text[start : end+1]
	}
	var parsed struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return "ask", "unparseable_answer"
	}
	if parsed.Decision == "continue" {
		return "continue", parsed.Reason
	}
	return "ask", parsed.Reason
}

// finishRiskEval applies one evaluation outcome. "continue" counts the bound
// wake (idempotently) and lets it dispatch; everything else falls back to the
// user's confirmation card. Both paths only act while the chain still waits
// on this evaluation.
func (s *Scheduler) finishRiskEval(threadID, decision, reason string) {
	s.evalMu.Lock()
	e := s.evals[threadID]
	delete(s.evals, threadID)
	s.evalMu.Unlock()
	if e == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	tx, err := s.app.DB.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	chain, err := sqlite.GetChain(ctx, tx, e.ChainID)
	if err != nil {
		tx.Rollback()
		return
	}
	if chain.Status != domain.ChainEvaluating {
		tx.Rollback() // the chain was stopped or decided meanwhile
		return
	}
	if decision != "continue" {
		tx.Rollback()
		// Ask (or anything unclean): the user decides, in its own
		// transaction, exactly like the pre-eval path did.
		tx2, err := s.app.DB.BeginTx(ctx, nil)
		if err != nil {
			return
		}
		events, perr := s.app.persistChainDecisionForEval(ctx, tx2, chain, e)
		if perr != nil {
			tx2.Rollback()
			return
		}
		if cerr := tx2.Commit(); cerr != nil {
			return
		}
		for _, ev := range events {
			s.app.recordEvent(ctx, ev)
		}
		s.app.Hub.Publish()
		return
	}
	// Model continue: the bound wake was authorized inside the budget —
	// counted exactly once, never double-counted by a later gate pass.
	delivery, derr := sqlite.GetDelivery(ctx, tx, e.DeliveryID)
	if derr != nil {
		tx.Rollback()
		return
	}
	if delivery.Status == domain.DeliveryQueued {
		counted, cerr := sqlite.CountChainWake(ctx, tx, chain.ID, e.DeliveryID, e.DeliveryID, e.BotID)
		if cerr != nil {
			tx.Rollback()
			return
		}
		if counted {
			if ierr := sqlite.IncrementChainWake(ctx, tx, chain.ID); ierr != nil {
				tx.Rollback()
				return
			}
		}
	}
	if serr := sqlite.SetChainStatus(ctx, tx, chain.ID, domain.ChainActive, ""); serr != nil {
		tx.Rollback()
		return
	}
	if err := tx.Commit(); err != nil {
		return
	}
	s.app.recordEvent(ctx, domain.Event{Type: domain.EventChainUpdated, TeamID: chain.TeamID, BotID: e.BotID, ResourceID: chain.ID,
		Payload: mustJSON(map[string]any{"decision": "continue", "reason": reason})})
	s.app.Hub.Publish()
	if delivery.Status == domain.DeliveryQueued && s.app.Scheduler != nil {
		s.app.Scheduler.Wake(e.BotID)
	}
}

// riskEvalFallback is the honest degradation path (G03): when the eval
// session itself cannot start — team gone, engine down, thread failure —
// the chain asks the user instead of silently proceeding or silently
// blocking. The evaluation was never registered, so the card is raised
// directly; the trigger reason stays on the card's record.
func (s *Scheduler) riskEvalFallback(ctx context.Context, chain domain.Chain, delivery sqlite.DeliveryRecord, reason string) {
	tx, err := s.app.DB.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	events, perr := s.app.requestChainDecision(ctx, tx, chain, delivery, false)
	if perr != nil {
		tx.Rollback()
		return
	}
	if cerr := tx.Commit(); cerr != nil {
		return
	}
	for _, ev := range events {
		s.app.recordEvent(ctx, ev)
	}
	s.app.Hub.Publish()
}

// persistChainDecisionForEval raises the user's confirmation card for a wake
// the model escalated. overLimit is always false here: the model is never
// shown a wake beyond the hard limit (G03), so this binding never grants
// one.
func (a *App) persistChainDecisionForEval(ctx context.Context, tx *sql.Tx, chain domain.Chain, e *riskEval) ([]domain.Event, error) {
	delivery := sqlite.DeliveryRecord{ID: e.DeliveryID, TargetBotID: e.BotID, ChainID: chain.ID}
	return a.requestChainDecision(ctx, tx, chain, delivery, false)
}
