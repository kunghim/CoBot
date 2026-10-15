package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// userPost drives a user route with the bearer token, mirroring userGet.
func (h *m2Harness) userPost(t *testing.T, path string, body map[string]any) (int, map[string]any) {
	t.Helper()
	if body == nil {
		body = map[string]any{}
	}
	body["requestId"] = "req-post-" + time.Now().Format("150405.000000000")
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, h.api.URL+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+h.bearer)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var parsed map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&parsed)
	return resp.StatusCode, parsed
}

// TestWebSurfaceActivityMemoriesAuthorizations covers the M5 completeness
// endpoints: the activity feed lists real events, the memories page lists
// and deletes a member's memories, and authorizations list and revoke the
// live bridge credentials without leaking token values.
func TestWebSurfaceActivityMemoriesAuthorizations(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev", "qa-engineer"})
	captain, member := bots[0], bots[1]

	// Activity: a real action produces real events for the team feed.
	if _, err := h.app.CreateTask(ctx, h.user, false, app.CreateTaskInput{
		TeamID: team.ID, OwnerID: member.ID, Title: "活动任务", AcceptanceCriteria: "出现在动态",
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	status, payload := h.userGet(t, "/api/v1/teams/"+team.ID+"/activity")
	if status != http.StatusOK {
		t.Fatalf("activity status = %d body=%v", status, payload)
	}
	data := payload["data"].(map[string]any)
	items := data["items"].([]any)
	if len(items) == 0 {
		t.Fatal("activity feed must contain the task.created event")
	}

	// Memories: the member remembers through the app; the page lists and
	// deletes it.
	principal := auth.NewPrincipalBot(member.ID, team.ID)
	mem, err := h.app.RememberMemory(ctx, principal, "提交前先 gofmt", "")
	if err != nil {
		t.Fatalf("remember: %v", err)
	}
	status, payload = h.userGet(t, "/api/v1/bots/"+member.ID+"/memories")
	if status != http.StatusOK {
		t.Fatalf("memories status = %d body=%v", status, payload)
	}
	listed := payload["data"].(map[string]any)["items"].([]any)
	if len(listed) != 1 {
		t.Fatalf("memories = %d, want 1", len(listed))
	}
	req, _ := http.NewRequest(http.MethodDelete, h.api.URL+"/api/v1/bots/"+member.ID+"/memories/"+mem.ID, nil)
	req.Header.Set("Authorization", "Bearer "+h.bearer)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete memory status = %d", resp.StatusCode)
	}
	status, payload = h.userGet(t, "/api/v1/bots/"+member.ID+"/memories")
	listed = payload["data"].(map[string]any)["items"].([]any)
	if len(listed) != 0 {
		t.Fatalf("memories after delete = %d, want 0", len(listed))
	}

	// Authorizations: minted credentials appear without their token value,
	// and revoking the member invalidates them for real.
	h.botToken(t, captain)
	status, payload = h.userGet(t, "/api/v1/authorizations")
	if status != http.StatusOK {
		t.Fatalf("authorizations status = %d", status)
	}
	raw, _ := json.Marshal(payload)
	if bytes.Contains(raw, []byte(h.bearer)) {
		t.Fatal("authorizations must never include a token value")
	}
	authorizations := payload["data"].(map[string]any)["items"].([]any)
	if len(authorizations) == 0 {
		t.Fatal("minted credential must be listed")
	}
	status, _ = h.userPost(t, "/api/v1/authorizations/"+captain.ID+"/revoke", nil)
	if status != http.StatusOK {
		t.Fatalf("revoke status = %d", status)
	}
	if len(h.app.CollaborationTokens.List()) != 0 {
		t.Fatal("revocation must invalidate every live credential of the member")
	}
}

// TestWebSurfaceDirectoriesAndRebind covers the Web directory picker checks
// and the explicit rebind: conflicts and foreign markers are refused the
// same way creation refuses them, and a clean move rewrites the marker.
func TestWebSurfaceDirectoriesAndRebind(t *testing.T) {
	h := newM2Harness(t)
	team, _ := h.createTeam(t, []string{"backend-dev"})

	// Listing children returns names only.
	child := filepath.Join(h.teamDir, "child-sub")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	status, payload := h.userGet(t, "/api/v1/directories?path="+strings.ReplaceAll(h.teamDir, "\\", "/"))
	if status != http.StatusOK {
		t.Fatalf("directories status = %d body=%v", status, payload)
	}
	children := payload["data"].(map[string]any)["children"].([]any)
	found := false
	for _, c := range children {
		if strings.Contains(c.(string), "child") {
			found = true
		}
	}
	if !found {
		t.Fatalf("children = %v, want the new subdirectory", children)
	}

	// Check: the bound directory is reported as duplicate.
	body, _ := json.Marshal(map[string]any{"path": h.teamDir})
	req, _ := http.NewRequest(http.MethodPost, h.api.URL+"/api/v1/directories/check", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+h.bearer)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var verdict map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&verdict)
	resp.Body.Close()
	check := verdict["data"].(map[string]any)
	if check["duplicate"] != true {
		t.Fatalf("check verdict = %v, want duplicate for the bound directory", check)
	}

	// Rebind to a clean directory: the marker is rewritten for this
	// instance and the binding moves.
	newDir := h.teamDir + "-moved"
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatal(err)
	}
	status, payload = h.userPost(t, "/api/v1/teams/"+team.ID+"/directory/rebind", map[string]any{"directory": newDir})
	if status != http.StatusOK {
		t.Fatalf("rebind status = %d body=%v", status, payload)
	}
	bound, _, err := sqlite.GetTeam(context.Background(), h.app.DB, team.ID)
	if err != nil {
		t.Fatalf("get team: %v", err)
	}
	if !strings.Contains(bound.Directory, "moved") {
		t.Fatalf("team directory = %q, want the moved folder", bound.Directory)
	}
	data, err := os.ReadFile(newDir + "/.cobot/team.json")
	if err != nil {
		t.Fatalf("marker after rebind: %v", err)
	}
	if !strings.Contains(string(data), h.app.InstanceID) {
		t.Fatal("rebind must rewrite the marker for this instance")
	}

	// A foreign marker on the target is refused without adoption.
	foreign := h.teamDir + "-foreign"
	if err := os.MkdirAll(foreign+"/.cobot", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(foreign+"/.cobot/team.json", []byte(`{"instanceId":"someone-else"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	status, _ = h.userPost(t, "/api/v1/teams/"+team.ID+"/directory/rebind", map[string]any{"directory": foreign})
	if status != http.StatusBadRequest {
		t.Fatalf("foreign-marker rebind status = %d, want 400", status)
	}
}
