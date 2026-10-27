package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/workspace"
)

func TestWorkspaceFilesAreScopedAndContentSupportsRanges(t *testing.T) {
	h := newM2Harness(t)
	team, _ := h.createTeam(t, nil)
	path := filepath.Join(team.Directory, "sample.txt")
	if err := os.WriteFile(path, []byte("0123456789"), 0600); err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/workspaces/team/" + team.ID
	unauthenticated, err := http.Get(h.api.URL + base + "/file?path=sample.txt")
	if err != nil {
		t.Fatal(err)
	}
	unauthenticated.Body.Close()
	if unauthenticated.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated: %d", unauthenticated.StatusCode)
	}
	get := func(path string, headers map[string]string) (int, http.Header, []byte) {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, h.api.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+h.bearer)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, resp.Header, data
	}
	status, _, data := get(base+"/file?path=sample.txt%3A4%3A2", nil)
	var metadata struct {
		Data workspace.File `json:"data"`
	}
	json.Unmarshal(data, &metadata)
	if status != 200 || metadata.Data.Path != "sample.txt" || metadata.Data.Line != 4 || metadata.Data.Column != 2 {
		t.Fatalf("metadata %d %s", status, data)
	}
	status, headers, data := get(base+"/content?path=sample.txt", map[string]string{"Range": "bytes=2-5"})
	if status != 206 || string(data) != "2345" || headers.Get("Content-Range") != "bytes 2-5/10" {
		t.Fatalf("range %d %v %q", status, headers, data)
	}
	if headers.Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(headers.Get("Content-Security-Policy"), "sandbox") {
		t.Fatal(headers)
	}
	status, _, data = get(base+"/content?path=sample.txt", map[string]string{"If-None-Match": headers.Get("ETag")})
	if status != 304 || len(data) != 0 {
		t.Fatalf("conditional %d %s", status, data)
	}
	status, _, _ = get(base+"/content?path=sample.txt", map[string]string{"If-Match": `"an-older-version"`})
	if status != http.StatusPreconditionFailed {
		t.Fatalf("stale file read: %d", status)
	}
	status, _, _ = get(base+"/content?path=sample.txt", map[string]string{"Range": "bytes=50-60"})
	if status != 416 {
		t.Fatal(status)
	}
	status, _, data = get(base+"/entries", nil)
	if status != 200 || !strings.Contains(string(data), "sample.txt") {
		t.Fatalf("directory %d %s", status, data)
	}
	for _, target := range []string{"../outside", t.TempDir()} {
		status, _, data = get(base+"/file?path="+url.QueryEscape(target), nil)
		if status != 403 && status != 404 {
			t.Fatalf("escape %q: %d %s", target, status, data)
		}
	}
	status, _, _ = get(base+"/content?path=", nil)
	if status != 400 {
		t.Fatalf("directory content: %d", status)
	}
	if err := os.WriteFile(filepath.Join(team.Directory, "page.html"), []byte("<script>unsafe()</script>"), 0600); err != nil {
		t.Fatal(err)
	}
	status, headers, _ = get(base+"/content?path=page.html", nil)
	if status != 200 || !strings.HasPrefix(headers.Get("Content-Type"), "text/plain") {
		t.Fatal(status, headers)
	}
	large, err := os.Create(filepath.Join(team.Directory, "large.bin"))
	if err != nil {
		t.Fatal(err)
	}
	err = large.Truncate(workspace.MaxPreviewBytes + 1)
	large.Close()
	if err != nil {
		t.Fatal(err)
	}
	status, _, data = get(base+"/content?path=large.bin", nil)
	if status != 400 || !strings.Contains(string(data), "error.workspace.too_large") {
		t.Fatalf("large: %d %s", status, data)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	status, _, _ = get(base+"/file?path=sample.txt", nil)
	if status != 404 {
		t.Fatal(status)
	}
}

func TestChatWorkspaceFilesCannotReadATeam(t *testing.T) {
	h := newM2Harness(t)
	attachChats(t, h, nil)
	team, _ := h.createTeam(t, nil)
	sent, err := h.app.SendChat(context.Background(), h.user, "", app.ChatSendInput{RequestID: "workspace-create", Text: "create"})
	if err != nil {
		t.Fatal(err)
	}
	teamPath := filepath.Join(team.Directory, "team-only.txt")
	os.WriteFile(teamPath, []byte("team"), 0600)
	status, body := h.userGet(t, "/api/v1/workspaces/chat/"+sent.Chat.ID+"/file?path="+url.QueryEscape(teamPath))
	if status != 403 {
		t.Fatalf("cross-scope: %d %+v", status, body)
	}
	os.WriteFile(filepath.Join(sent.Chat.Workspace, "chat-only.txt"), []byte("chat"), 0600)
	status, body = h.userGet(t, "/api/v1/workspaces/chat/"+sent.Chat.ID+"/file?path=chat-only.txt")
	if status != 200 {
		t.Fatalf("chat file: %d %+v", status, body)
	}
}
