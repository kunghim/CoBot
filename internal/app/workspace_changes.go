package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/kunghim/CoBot/internal/auth"
	patches "github.com/kunghim/CoBot/internal/changes"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

func (a *App) workspaceChangeSource(ctx context.Context, q sqlite.Queryer, chat bool, runID string) (sqlite.ChangeSource, error) {
	seq, metadata, complete, err := sqlite.LatestWorkspaceTurnDiff(ctx, q, chat, runID)
	if err != nil && !errors.Is(err, sqlite.ErrNotFound) {
		return sqlite.ChangeSource{}, err
	}
	hasTurn := err == nil
	if hasTurn && metadata && complete {
		return sqlite.ChangeSource{Kind: "turn_diff", Seq: seq, FilesComplete: true}, nil
	}
	hasFiles, err := sqlite.HasWorkspaceFileChanges(ctx, q, chat, runID)
	if err != nil {
		return sqlite.ChangeSource{}, err
	}
	if hasFiles || !hasTurn {
		return sqlite.ChangeSource{Kind: "file_change", FilesComplete: true}, nil
	}
	if metadata {
		return sqlite.ChangeSource{Kind: "turn_diff", Seq: seq, FilesComplete: false}, nil
	}
	diff, _, err := sqlite.ReadWorkspaceTurnDiff(ctx, q, chat, runID, seq)
	if err != nil {
		return sqlite.ChangeSource{}, err
	}
	files, _ := patches.ParseTurn(diff)
	for index := range files {
		// The old adapter did not retain pre-clipping statistics or a proof
		// that all file headers survived. Do not promote a plausible count.
		files[index].Additions = 0
		files[index].Deletions = 0
		files[index].StatsComplete = false
	}
	detail, err := json.Marshal(map[string]any{"changes": files})
	return sqlite.ChangeSource{Kind: "turn_diff", Seq: seq, FilesComplete: false, LegacyDetail: detail}, err
}

func (a *App) workspaceChangeRun(ctx context.Context, q sqlite.Queryer, chat bool, owner, runID string) (domain.WorkspaceChangeRun, sqlite.ChangeSource, error) {
	run, err := sqlite.GetWorkspaceChangeRun(ctx, q, chat, owner, runID)
	if err != nil {
		return run, sqlite.ChangeSource{}, sqlite.NormalizeError(err)
	}
	source, err := a.workspaceChangeSource(ctx, q, chat, run.ID)
	if err != nil {
		return run, source, err
	}
	run.Source, run.FilesComplete = source.Kind, source.FilesComplete
	run.FileCount, run.Additions, run.Deletions, run.StatsComplete, err = sqlite.WorkspaceChangeTotals(ctx, q, chat, run.ID, source)
	run.StatsComplete = run.StatsComplete && run.FilesComplete
	return run, source, err
}

func (a *App) ListWorkspaceChangeRuns(ctx context.Context, p auth.Principal, scope, owner, botID, cursor string) (domain.WorkspaceChangeRunsPage, error) {
	page := domain.WorkspaceChangeRunsPage{Runs: []domain.WorkspaceChangeRun{}}
	if _, err := a.workspaceDirectory(ctx, p, scope, owner); err != nil {
		return page, err
	}
	chat := scope == "chat"
	if !chat {
		if botID == "" {
			return page, domain.InvalidRequest("error.workspace.member_required", nil)
		}
		bot, err := sqlite.GetBot(ctx, a.DB, botID)
		if err != nil {
			return page, sqlite.NormalizeError(err)
		}
		if bot.TeamID != owner {
			return page, domain.NotFound("error.not_found", nil)
		}
	}
	if len(cursor) > 512 {
		return page, domain.InvalidRequest("error.workspace.invalid_cursor", nil)
	}
	snapshot, err := a.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return page, err
	}
	defer snapshot.Rollback()
	runs, err := sqlite.ListWorkspaceChangeRuns(ctx, snapshot, chat, owner, botID, cursor)
	if errors.Is(err, sqlite.ErrNotFound) {
		return page, domain.InvalidRequest("error.workspace.invalid_cursor", nil)
	}
	if err != nil {
		return page, err
	}
	if len(runs) > sqlite.ChangeRunPageSize {
		runs = runs[:sqlite.ChangeRunPageSize]
		page.NextCursor = runs[len(runs)-1].ID
	}
	for _, run := range runs {
		summary, _, err := a.workspaceChangeRun(ctx, snapshot, chat, owner, run.ID)
		if err != nil {
			return page, err
		}
		page.Runs = append(page.Runs, summary)
	}
	return page, nil
}

func (a *App) ListWorkspaceChangeFiles(ctx context.Context, p auth.Principal, scope, owner, runID, cursor string) (domain.WorkspaceChangeFilesPage, error) {
	page := domain.WorkspaceChangeFilesPage{Files: []domain.WorkspaceChangeFile{}}
	if _, err := a.workspaceDirectory(ctx, p, scope, owner); err != nil {
		return page, err
	}
	if len(cursor) > 128<<10 {
		return page, domain.InvalidRequest("error.workspace.invalid_cursor", nil)
	}
	snapshot, err := a.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return page, err
	}
	defer snapshot.Rollback()
	chat := scope == "chat"
	run, source, err := a.workspaceChangeRun(ctx, snapshot, chat, owner, runID)
	if err != nil {
		return page, err
	}
	page.Run = run
	files, err := sqlite.ListWorkspaceChangeFiles(ctx, snapshot, chat, runID, source, cursor)
	if err != nil {
		return page, err
	}
	if len(files) > sqlite.ChangeFilePageSize {
		files = files[:sqlite.ChangeFilePageSize]
		page.NextCursor = files[len(files)-1].Path
	}
	page.Files = files
	return page, nil
}

func (a *App) GetWorkspaceChangeDiff(ctx context.Context, p auth.Principal, scope, owner, runID, path string) (domain.WorkspaceChangeDiff, error) {
	result := domain.WorkspaceChangeDiff{}
	if _, err := a.workspaceDirectory(ctx, p, scope, owner); err != nil {
		return result, err
	}
	snapshot, err := a.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer snapshot.Rollback()
	chat := scope == "chat"
	run, source, err := a.workspaceChangeRun(ctx, snapshot, chat, owner, runID)
	if err != nil {
		return result, err
	}
	file, start, end, err := sqlite.GetWorkspaceChangeFile(ctx, snapshot, chat, runID, source, path)
	if err != nil {
		return result, sqlite.NormalizeError(err)
	}
	result.Run, result.File = run, file
	if source.Kind == "turn_diff" {
		diff, truncated, err := sqlite.ReadWorkspaceTurnDiff(ctx, snapshot, chat, runID, source.Seq)
		if err != nil {
			return result, err
		}
		if truncated {
			diff = strings.TrimSuffix(diff, "\n…")
		}
		if start < 0 || end < start {
			return result, domain.InvalidRequest("error.workspace.unreadable", nil)
		}
		result.DiffTruncated = file.DiffTruncated || end > len(diff)
		if start < len(diff) {
			result.Diff = diff[start:min(end, len(diff))]
		}
	} else {
		result.Diff, result.DiffTruncated, err = sqlite.ReadWorkspaceFilePatches(ctx, snapshot, chat, runID, path)
		if err != nil {
			return result, err
		}
	}
	return result, nil
}
