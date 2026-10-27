package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

func seedWorkspaceChangeRun(t *testing.T, h *m2Harness, teamID, botID string, steps ...domain.RunStep) string {
	t.Helper()
	ctx := context.Background()
	run := domain.Run{ID: domain.NewID(), TeamID: teamID, BotID: botID, Status: domain.RunCompleted, StartedAt: time.Now().UTC()}
	tx, err := h.app.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := sqlite.InsertRun(ctx, tx, run); err != nil {
		t.Fatal(err)
	}
	for index, step := range steps {
		step.ID = fmt.Sprintf("step-%d", index)
		step.RunID = run.ID
		step.TeamID = teamID
		step.BotID = botID
		if step.Status == "" {
			step.Status = "completed"
		}
		if _, err := sqlite.UpsertRunStep(ctx, tx, step); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return run.ID
}

func changeDetail(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestWorkspaceChangesUseOneSourceAndPreserveHistoricalCompleteness(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	stored := "diff --git a/old.txt b/new.txt\nrename from old.txt\nrename to new.txt\n@@ -1 +1 @@\n-before\n+after\n"
	fileChange := domain.RunStep{Kind: "file_change", Detail: changeDetail(t, map[string]any{"changes": []any{map[string]any{"path": "old.txt", "kind": "update", "diff": "+duplicated", "additions": 999, "deletions": 0, "statsComplete": true}}})}
	turn := domain.RunStep{Kind: "diff", Detail: changeDetail(t, map[string]any{"diff": stored + "\n…", "filesComplete": true, "diffTruncated": true, "changes": []any{
		map[string]any{"path": "old.txt", "movePath": "new.txt", "kind": "update", "additions": 50, "deletions": 30, "statsComplete": true, "diffTruncated": true, "start": 0, "end": 100000},
		map[string]any{"path": "deleted.txt", "kind": "delete", "additions": 0, "deletions": 2, "statsComplete": true, "diffTruncated": true, "start": 100000, "end": 101000},
	}})}
	runID := seedWorkspaceChangeRun(t, h, team.ID, bots[0].ID, fileChange, turn)
	page, err := h.app.ListWorkspaceChangeRuns(ctx, h.user, "team", team.ID, bots[0].ID, "")
	if err != nil || len(page.Runs) != 1 {
		t.Fatalf("runs=%+v err=%v", page, err)
	}
	run := page.Runs[0]
	if run.Source != "turn_diff" || run.FileCount != 2 || run.Additions != 50 || run.Deletions != 32 || !run.StatsComplete {
		t.Fatalf("duplicated or recounted clipped stats: %+v", run)
	}
	diff, err := h.app.GetWorkspaceChangeDiff(ctx, h.user, "team", team.ID, runID, "old.txt")
	if err != nil || diff.File.MovePath != "new.txt" || diff.Diff != stored || !diff.DiffTruncated {
		t.Fatalf("rename diff=%+v err=%v", diff, err)
	}
	deleted, err := h.app.GetWorkspaceChangeDiff(ctx, h.user, "team", team.ID, runID, "deleted.txt")
	if err != nil || deleted.File.Kind != "delete" || deleted.Diff != "" || !deleted.DiffTruncated {
		t.Fatalf("deleted diff=%+v err=%v", deleted, err)
	}
	// A different member and a different team cannot be used to find this run.
	other, err := h.app.ListWorkspaceChangeRuns(ctx, h.user, "team", team.ID, bots[1].ID, "")
	if err != nil || len(other.Runs) != 0 {
		t.Fatalf("member isolation: %+v err=%v", other, err)
	}
	foreign, _ := h.createTeamWithDir(t, "other", "changes-other", nil)
	if _, err := h.app.ListWorkspaceChangeFiles(ctx, h.user, "team", foreign.ID, runID, ""); err == nil {
		t.Fatal("cross-team run was accepted")
	}
	legacy := seedWorkspaceChangeRun(t, h, team.ID, bots[0].ID, domain.RunStep{Kind: "file_change", Detail: changeDetail(t, map[string]any{"changes": []any{map[string]any{"path": "old-record.txt", "kind": "update", "diff": "+one\n-old\n"}}})})
	old, err := h.app.ListWorkspaceChangeFiles(ctx, h.user, "team", team.ID, legacy, "")
	if err != nil || len(old.Files) != 1 || old.Run.StatsComplete || old.Files[0].StatsComplete {
		t.Fatalf("legacy counts claimed exact: %+v err=%v", old, err)
	}
}

func TestWorkspaceChangeHistoryPagesRunsAndFiles(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, nil)
	files := []any{}
	for index := 0; index < 101; index++ {
		files = append(files, map[string]any{"path": fmt.Sprintf("file-%03d.go", index), "kind": "add", "diff": "+x\n", "additions": 1, "deletions": 0, "statsComplete": true})
	}
	runID := seedWorkspaceChangeRun(t, h, team.ID, bots[0].ID, domain.RunStep{Kind: "file_change", Detail: changeDetail(t, map[string]any{"changes": files})})
	first, err := h.app.ListWorkspaceChangeFiles(ctx, h.user, "team", team.ID, runID, "")
	if err != nil || len(first.Files) != 100 || first.NextCursor != "file-099.go" || first.Run.FileCount != 101 || first.Run.Additions != 101 {
		t.Fatalf("first file page=%+v err=%v", first, err)
	}
	last, err := h.app.ListWorkspaceChangeFiles(ctx, h.user, "team", team.ID, runID, first.NextCursor)
	if err != nil || len(last.Files) != 1 || last.Files[0].Path != "file-100.go" || last.NextCursor != "" {
		t.Fatalf("last file page=%+v err=%v", last, err)
	}
	for index := 0; index < 20; index++ {
		change := map[string]any{"path": "file-000.go", "kind": "update", "diff": strings.Repeat("+later\n", index+2), "additions": index + 2, "deletions": 0, "statsComplete": true}
		seedWorkspaceChangeRun(t, h, team.ID, bots[0].ID, domain.RunStep{Kind: "file_change", Detail: changeDetail(t, map[string]any{"changes": []any{change}})})
	}
	seedWorkspaceChangeRun(t, h, team.ID, bots[0].ID, domain.RunStep{Kind: "file_change", Detail: json.RawMessage(`{"changes":[]}`)})
	seedWorkspaceChangeRun(t, h, team.ID, bots[0].ID, domain.RunStep{Kind: "diff", Detail: json.RawMessage(`{"diff":"","changes":[],"filesComplete":true}`)})
	runs, err := h.app.ListWorkspaceChangeRuns(ctx, h.user, "team", team.ID, bots[0].ID, "")
	if err != nil || len(runs.Runs) != 20 || runs.NextCursor == "" {
		t.Fatalf("first run page len=%d next=%q err=%v", len(runs.Runs), runs.NextCursor, err)
	}
	latest, err := h.app.GetWorkspaceChangeDiff(ctx, h.user, "team", team.ID, runs.Runs[0].ID, "file-000.go")
	if err != nil || latest.File.Additions != 21 || strings.Count(latest.Diff, "+later") != 21 {
		t.Fatalf("latest run was mixed with earlier file changes: %+v err=%v", latest, err)
	}
	earlier, err := h.app.GetWorkspaceChangeDiff(ctx, h.user, "team", team.ID, runID, "file-000.go")
	if err != nil || earlier.File.Additions != 1 || earlier.Diff != "+x\n" {
		t.Fatalf("earlier run changed after later edits: %+v err=%v", earlier, err)
	}
	tail, err := h.app.ListWorkspaceChangeRuns(ctx, h.user, "team", team.ID, bots[0].ID, runs.NextCursor)
	if err != nil || len(tail.Runs) != 1 || tail.Runs[0].ID != runID || tail.NextCursor != "" {
		t.Fatalf("last run page=%+v err=%v", tail, err)
	}
	if _, err := h.app.ListWorkspaceChangeRuns(ctx, h.user, "team", team.ID, bots[0].ID, "unknown"); err == nil {
		t.Fatal("invalid run cursor accepted")
	}
}

func TestWorkspaceChatChangesAndHTTPAuthorization(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	chat := domain.Chat{ID: domain.NewID(), Workspace: h.teamDir, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := sqlite.SaveChat(ctx, h.app.DB, chat, true); err != nil {
		t.Fatal(err)
	}
	run := domain.ChatRun{ID: domain.NewID(), ChatID: chat.ID, Status: "completed", StartedAt: time.Now().UTC()}
	if err := sqlite.SaveChatRun(ctx, h.app.DB, run); err != nil {
		t.Fatal(err)
	}
	if err := sqlite.SaveChatStep(ctx, h.app.DB, run.ID, domain.ChatStep{ID: "change", Kind: "file_change", Status: "completed", Detail: changeDetail(t, map[string]any{"changes": []any{map[string]any{"path": "chat.txt", "kind": "add", "diff": "+chat\n", "additions": 1, "deletions": 0, "statsComplete": true}}})}); err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/workspaces/chat/" + chat.ID + "/changes"
	response, err := http.Get(h.api.URL + base)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatalf("unauthenticated status=%d", response.StatusCode)
	}
	for _, path := range []string{base, base + "/" + run.ID, base + "/" + run.ID + "/file?path=" + url.QueryEscape("chat.txt")} {
		request, _ := http.NewRequest("GET", h.api.URL+path, nil)
		request.Header.Set("Authorization", "Bearer "+h.bearer)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]json.RawMessage
		decodeErr := json.NewDecoder(response.Body).Decode(&body)
		response.Body.Close()
		if response.StatusCode != 200 || decodeErr != nil || len(body["data"]) == 0 {
			t.Fatalf("GET %s status=%d data=%s err=%v", path, response.StatusCode, body["data"], decodeErr)
		}
	}
	page, err := h.app.ListWorkspaceChangeRuns(ctx, h.user, "chat", chat.ID, "", "")
	if err != nil || len(page.Runs) != 1 || page.Runs[0].Additions != 1 {
		t.Fatalf("chat summary=%+v err=%v", page, err)
	}
	if _, err := h.app.GetWorkspaceChangeDiff(ctx, h.user, "chat", chat.ID, run.ID, "../../outside"); err == nil {
		t.Fatal("unrecorded path accepted")
	}
	old := domain.ChatRun{ID: domain.NewID(), ChatID: chat.ID, Status: "completed", StartedAt: time.Now().UTC()}
	if err := sqlite.SaveChatRun(ctx, h.app.DB, old); err != nil {
		t.Fatal(err)
	}
	legacyDiff := "diff --git a/legacy b/legacy\n--- a/legacy\n+++ b/legacy\n@@ -1 +1 @@\n-a\n+b\n"
	if err := sqlite.SaveChatStep(ctx, h.app.DB, old.ID, domain.ChatStep{ID: "old-diff", Kind: "diff", Status: "completed", Detail: changeDetail(t, map[string]any{"diff": legacyDiff, "files": 1})}); err != nil {
		t.Fatal(err)
	}
	legacy, err := h.app.GetWorkspaceChangeDiff(ctx, h.user, "chat", chat.ID, old.ID, "legacy")
	if err != nil || legacy.Run.StatsComplete || legacy.Run.FilesComplete || !strings.Contains(legacy.Diff, "+b") {
		t.Fatalf("legacy turn fallback=%+v err=%v", legacy, err)
	}
}

func TestWorkspaceChangeDiffUsesOneSnapshotDuringUpdates(t *testing.T) {
	h := newM2Harness(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	team, bots := h.createTeam(t, nil)
	detail := func(count int) json.RawMessage {
		body := "+first\n"
		if count == 2 {
			body = "+second\n+extra\n"
		}
		diff := fmt.Sprintf("diff --git a/changing.txt b/changing.txt\n--- a/changing.txt\n+++ b/changing.txt\n@@ -0,0 +1,%d @@\n%s", count, body)
		return changeDetail(t, map[string]any{"diff": diff, "filesComplete": true, "changes": []any{map[string]any{"path": "changing.txt", "kind": "update", "additions": count, "deletions": 0, "statsComplete": true, "start": 0, "end": len(diff)}}})
	}
	variants := []json.RawMessage{detail(1), detail(2)}
	runID := seedWorkspaceChangeRun(t, h, team.ID, bots[0].ID, domain.RunStep{Kind: "diff", Detail: variants[0]})
	done := make(chan error, 1)
	go func() {
		for index := 0; index < 100; index++ {
			_, err := sqlite.UpsertRunStep(ctx, h.app.DB, domain.RunStep{ID: "step-0", RunID: runID, TeamID: team.ID, BotID: bots[0].ID, Kind: "diff", Status: "completed", Detail: variants[index%2]})
			if err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	for index := 0; index < 50; index++ {
		diff, err := h.app.GetWorkspaceChangeDiff(ctx, h.user, "team", team.ID, runID, "changing.txt")
		if err != nil {
			t.Fatal(err)
		}
		expected := 1
		if strings.Contains(diff.Diff, "+second") {
			expected = 2
		}
		if diff.File.Additions != expected || diff.Run.Additions != expected {
			t.Fatalf("mixed snapshot: run=%d file=%d diff=%q", diff.Run.Additions, diff.File.Additions, diff.Diff)
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
