package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/platform"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

type mergeBinding struct {
	domain.MergeApprovalDetails
	Version       int    `json:"bindingVersion"`
	TeamDirectory string `json:"teamDirectory"`
	EvidenceHash  string `json:"evidenceHash"`
}

func mergeEvidence(all []domain.Evidence, ids []string, candidate string) ([]domain.Evidence, error) {
	if len(ids) == 0 {
		return nil, domain.InvalidRequest("error.merge.needs_evidence", nil)
	}
	have := map[string]domain.Evidence{}
	for _, e := range all {
		have[e.ID] = e
	}
	var selected []domain.Evidence
	hasTest, hasReview := false, false
	seen := map[string]bool{}
	for _, id := range ids {
		e, ok := have[id]
		if !ok || seen[id] {
			return nil, domain.InvalidRequest("error.task.evidence_unknown", map[string]any{"evidenceId": id})
		}
		if e.CandidateRef != candidate {
			return nil, domain.InvalidRequest("error.merge.evidence_candidate_mismatch", map[string]any{"evidenceId": id})
		}
		seen[id] = true
		selected = append(selected, e)
		hasTest = hasTest || strings.TrimSpace(e.TestResult) != ""
		hasReview = hasReview || strings.TrimSpace(e.ReviewResult) != ""
	}
	if !hasTest || !hasReview {
		return nil, domain.InvalidRequest("error.merge.needs_test_review", nil)
	}
	return selected, nil
}

func evidenceFingerprint(evidence []domain.Evidence) string {
	ordered := append([]domain.Evidence(nil), evidence...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	raw, _ := json.Marshal(ordered)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// SubmitMergeRequest records immutable candidate/target/task/evidence facts.
// Directory checks do not execute Git and do not expand the team's sandbox.
func (a *App) SubmitMergeRequest(ctx context.Context, p auth.Principal, taskID, candidate, target, directory string, ids []string) (domain.Approval, error) {
	cached, unlock, err := beginWrite[domain.Approval](ctx, a, p)
	defer unlock()
	if err != nil {
		return domain.Approval{}, err
	}
	if cached != nil {
		return *cached, nil
	}
	if p.Kind != auth.KindBot {
		return domain.Approval{}, domain.Forbidden("error.forbidden.bot_only", nil)
	}
	candidate, target = strings.TrimSpace(candidate), strings.TrimSpace(target)
	if candidate == "" {
		return domain.Approval{}, domain.InvalidRequest("error.merge.candidate_required", nil)
	}
	if target == "" {
		return domain.Approval{}, domain.InvalidRequest("error.merge.target_required", nil)
	}
	task, err := sqlite.GetTask(ctx, a.DB, taskID)
	if err != nil {
		return domain.Approval{}, sqlite.NormalizeError(err)
	}
	team, _, err := sqlite.GetTeam(ctx, a.DB, task.TeamID)
	if err != nil {
		return domain.Approval{}, err
	}
	if !filepath.IsAbs(directory) {
		return domain.Approval{}, domain.InvalidRequest("error.merge.directory_invalid", nil)
	}
	root, err := platform.CanonicalDirFrom(team.Directory)
	if err != nil {
		return domain.Approval{}, err
	}
	execution, err := platform.CanonicalDirFrom(directory)
	if err != nil || !root.Contains(execution) {
		return domain.Approval{}, domain.InvalidRequest("error.merge.directory_invalid", nil)
	}
	if stat, err := os.Stat(execution.Display); err != nil || !stat.IsDir() {
		return domain.Approval{}, domain.InvalidRequest("error.merge.directory_invalid", nil)
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Approval{}, err
	}
	defer tx.Rollback()
	task, err = sqlite.GetTask(ctx, tx, taskID)
	if err != nil {
		return domain.Approval{}, err
	}
	freshTeam, _, err := sqlite.GetTeam(ctx, tx, task.TeamID)
	if err != nil {
		return domain.Approval{}, err
	}
	if freshTeam.Status != domain.TeamStatusActive || freshTeam.Directory != team.Directory {
		return domain.Approval{}, domain.Conflict("error.merge.directory_changed", nil)
	}
	if p.TeamID != task.TeamID || (p.BotID != task.OwnerID && p.BotID != freshTeam.LeaderID) {
		return domain.Approval{}, domain.Forbidden("error.merge.requester_only", nil)
	}
	if task.Status == domain.TaskCancelled {
		return domain.Approval{}, domain.Conflict("error.task.terminal", nil)
	}
	evidence, err := sqlite.ListEvidence(ctx, tx, task.ID)
	if err != nil {
		return domain.Approval{}, err
	}
	selected, err := mergeEvidence(evidence, ids, candidate)
	if err != nil {
		return domain.Approval{}, err
	}
	binding := mergeBinding{Version: 1, TeamDirectory: team.Directory, EvidenceHash: evidenceFingerprint(evidence), MergeApprovalDetails: domain.MergeApprovalDetails{TaskID: task.ID, TaskRevision: task.Revision, CandidateRef: candidate, TargetRef: target, ExecutionDirectory: execution.Display, EvidenceIDs: ids, Evidence: selected}}
	bound, err := json.Marshal(binding)
	if err != nil {
		return domain.Approval{}, err
	}
	approval := domain.Approval{ID: domain.NewID(), TeamID: task.TeamID, RequesterBotID: p.BotID, RunID: p.RunID, EngineRequestID: bound, Type: domain.ApprovalMerge, Summary: "approval.card.merge", Reason: "candidate=" + candidate + " target=" + target, Revision: 1, CreatedAt: time.Now().UTC(), Options: []domain.ApprovalOption{{ID: "approve", MessageKey: "approval.option.merge.approve"}, {ID: "reject", MessageKey: "approval.option.merge.reject"}}}
	if err := sqlite.InsertApproval(ctx, tx, approval); err != nil {
		return domain.Approval{}, err
	}
	approval, err = sqlite.GetApproval(ctx, tx, approval.ID)
	if err != nil {
		return domain.Approval{}, err
	}
	if err := a.saveWriteResult(ctx, tx, p, approval); err != nil {
		return domain.Approval{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Approval{}, err
	}
	_ = a.recordEvent(ctx, domain.Event{Type: domain.EventApprovalCreated, TeamID: task.TeamID, BotID: p.BotID, ResourceID: approval.ID})
	a.Hub.Publish()
	return approval, nil
}

func (a *App) validateMergeDecision(ctx context.Context, q sqlite.Queryer, approval domain.Approval) (mergeBinding, error) {
	var binding mergeBinding
	if json.Unmarshal(approval.EngineRequestID, &binding) != nil || binding.Version != 1 || binding.TaskID == "" || binding.TaskRevision < 1 {
		return binding, domain.Conflict("error.merge.binding_unverified", nil)
	}
	task, err := sqlite.GetTask(ctx, q, binding.TaskID)
	if err != nil {
		return binding, sqlite.NormalizeError(err)
	}
	team, _, err := sqlite.GetTeam(ctx, q, task.TeamID)
	if err != nil {
		return binding, err
	}
	if team.Status != domain.TeamStatusActive || task.TeamID != approval.TeamID || task.Revision != binding.TaskRevision || task.Status == domain.TaskCancelled || team.Directory != binding.TeamDirectory {
		return binding, domain.Conflict("error.merge.candidate_changed", nil)
	}
	evidence, err := sqlite.ListEvidence(ctx, q, task.ID)
	if err != nil {
		return binding, err
	}
	if evidenceFingerprint(evidence) != binding.EvidenceHash {
		return binding, domain.Conflict("error.merge.evidence_changed", nil)
	}
	if _, err := mergeEvidence(evidence, binding.EvidenceIDs, binding.CandidateRef); err != nil {
		return binding, err
	}
	return binding, nil
}
