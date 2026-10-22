package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/browser"
)

// TestMemberBrowserToolThroughTheCore drives the browser tool the way a member
// reaches it: over the collaboration route, into the team's own browser.
func TestMemberBrowserToolThroughTheCore(t *testing.T) {
	if _, err := browser.Find(); err != nil {
		t.Skip("no Chromium-based browser on this machine")
	}
	t.Setenv("COBOT_BROWSER_NO_SANDBOX", "1")
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `<!doctype html><title>演示页</title><h1>你好</h1><button onclick="document.title='已点击'">点我</button>`)
	}))
	defer site.Close()

	h := newM2Harness(t)
	profiles, _ := os.MkdirTemp("", "cobot-browser-test")
	// Edge keeps its profile files a moment after closing; removal is best effort.
	defer func() { time.Sleep(time.Second); _ = os.RemoveAll(profiles) }()
	h.app.Browser = browser.NewManager(profiles, nil)
	defer h.app.Browser.Shutdown()
	_, bots := h.createTeam(t, []string{"fullstack-engineer"})
	leader, member := bots[0], bots[1]

	// The first real call of a run gets the guide back and runs nothing.
	// One execution (one run) for the member's calls; callTool would start a
	// fresh run for each.
	binding, _ := h.tokens.Resolve(h.botToken(t, member))
	token, finish := h.scriptExecution(t, binding)
	defer finish()
	status, out := h.callToolRaw(t, token, "browser", map[string]any{"url": site.URL})
	raw, _ := json.Marshal(out)
	if status != http.StatusOK || !strings.Contains(string(raw), "团队浏览器使用说明") || strings.Contains(string(raw), "演示页") {
		t.Fatalf("first call should return the guide only: %d %s", status, raw)
	}

	status, out = h.callToolRaw(t, token, "browser", map[string]any{"url": site.URL})
	raw, _ = json.Marshal(out)
	if status != http.StatusOK || !strings.Contains(string(raw), "演示页") || !strings.Contains(string(raw), `button \"点我\"`) {
		t.Fatalf("member's browser call = %d %s", status, raw)
	}

	// A second call reuses the member's tab and the refs of the last snapshot.
	status, out = h.callToolRaw(t, token, "browser", map[string]any{
		"steps": []map[string]any{{"action": "click", "text": "点我"}}, "snapshot": false})
	raw, _ = json.Marshal(out)
	if status != http.StatusOK || !strings.Contains(string(raw), "已点击") {
		t.Fatalf("second call = %d %s", status, raw)
	}

	// The user sees the member's tab in the live view and can drive it.
	team := leader.TeamID
	status, panel := h.userGet(t, "/api/v1/teams/"+team+"/browser")
	raw, _ = json.Marshal(panel)
	if status != http.StatusOK || !strings.Contains(string(raw), `"running":true`) || !strings.Contains(string(raw), member.ID) {
		t.Fatalf("panel = %d %s", status, raw)
	}
	status, out = h.userPost(t, "/api/v1/teams/"+team+"/browser/input", map[string]any{"tab": "t1", "kind": "nav", "to": "reload"})
	if status != http.StatusOK {
		t.Fatalf("input = %d %v", status, out)
	}
	status, _ = h.userPost(t, "/api/v1/teams/"+team+"/browser/input", map[string]any{"tab": "t1", "kind": "nav", "to": "chrome://settings"})
	if status != http.StatusBadRequest {
		t.Fatalf("a browser-internal address from the live view = %d", status)
	}

	// The project manager plans and reports; it has no browser.
	status, out = h.callTool(t, h.botToken(t, leader), "browser", map[string]any{"url": site.URL})
	raw, _ = json.Marshal(out)
	if !strings.Contains(string(raw), "error.browser.leader") {
		t.Fatalf("leader's browser call = %d %s", status, raw)
	}
}
