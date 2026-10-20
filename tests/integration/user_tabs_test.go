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

// userDelete issues a DELETE as the signed-in user, like userPost.
func (h *m2Harness) userDelete(t *testing.T, path string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, h.api.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+h.bearer)
	req.Header.Set("Idempotency-Key", "req-delete-"+time.Now().Format("150405.000000000"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var parsed map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&parsed)
	return resp.StatusCode, parsed
}

// TestUserOwnTabsThroughTheCore drives the user's own browser tabs the way
// the workspace will: the user opens, sizes and closes a tab over HTTP, and a
// member finds it only when it asks for the user's tabs.
func TestUserOwnTabsThroughTheCore(t *testing.T) {
	if _, err := browser.Find(); err != nil {
		t.Skip("no Chromium-based browser on this machine")
	}
	t.Setenv("COBOT_BROWSER_NO_SANDBOX", "1")
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `<!doctype html><title>用户打开的页</title><h1>你好，用户</h1>`)
	}))
	defer site.Close()

	h := newM2Harness(t)
	profiles, _ := os.MkdirTemp("", "cobot-browser-test")
	defer func() { time.Sleep(time.Second); _ = os.RemoveAll(profiles) }()
	h.app.Browser = browser.NewManager(profiles, nil)
	defer h.app.Browser.Shutdown()
	_, bots := h.createTeam(t, []string{"fullstack-engineer"})
	member := bots[1]
	team := bots[0].TeamID

	// The user opens their own tab; it comes up owned by the user.
	status, out := h.userPost(t, "/api/v1/teams/"+team+"/browser/tabs", map[string]any{"url": site.URL})
	raw, _ := json.Marshal(out)
	if status != http.StatusCreated || !strings.Contains(string(raw), `"owner":"user"`) {
		t.Fatalf("open = %d %s", status, raw)
	}
	tabID := out["data"].(map[string]any)["tab"].(map[string]any)["id"].(string)

	// The panel shows it running, owned by the user, with no size set yet.
	status, panel := h.userGet(t, "/api/v1/teams/"+team+"/browser")
	raw, _ = json.Marshal(panel)
	if status != http.StatusOK || !strings.Contains(string(raw), `"running":true`) ||
		!strings.Contains(string(raw), `"owner":"user"`) || strings.Contains(string(raw), `"viewport"`) {
		t.Fatalf("panel = %d %s", status, raw)
	}

	// A member's first browser call still gets the guide and runs nothing;
	// the next one lists the user's tabs, which are hidden otherwise.
	binding, _ := h.tokens.Resolve(h.botToken(t, member))
	token, finish := h.scriptExecution(t, binding)
	defer finish()
	status, out = h.callToolRaw(t, token, "browser", map[string]any{"userTabs": true})
	raw, _ = json.Marshal(out)
	if status != http.StatusOK || !strings.Contains(string(raw), "团队浏览器使用说明") {
		t.Fatalf("first call = %d %s", status, raw)
	}
	status, out = h.callToolRaw(t, token, "browser", map[string]any{"userTabs": true})
	raw, _ = json.Marshal(out)
	if status != http.StatusOK || !strings.Contains(string(raw), "userTabs") ||
		!strings.Contains(string(raw), tabID) || !strings.Contains(string(raw), "用户打开的页") {
		t.Fatalf("userTabs = %d %s", status, raw)
	}

	// Pointed at the user's tab, the member works in it.
	status, out = h.callToolRaw(t, token, "browser", map[string]any{
		"tab": tabID, "steps": []map[string]any{{"action": "eval", "expression": "document.title"}},
		"snapshot": false})
	raw, _ = json.Marshal(out)
	if status != http.StatusOK || !strings.Contains(string(raw), "用户打开的页") {
		t.Fatalf("the member on the user's tab = %d %s", status, raw)
	}

	// The user switches the page to a phone size; the panel reports it.
	status, out = h.userPost(t, "/api/v1/teams/"+team+"/browser/tabs/"+tabID+"/viewport", map[string]any{"preset": "phone"})
	raw, _ = json.Marshal(out)
	if status != http.StatusOK || !strings.Contains(string(raw), `"width":390`) || !strings.Contains(string(raw), `"mobile":true`) {
		t.Fatalf("viewport = %d %s", status, raw)
	}
	status, panel = h.userGet(t, "/api/v1/teams/"+team+"/browser")
	raw, _ = json.Marshal(panel)
	if status != http.StatusOK || !strings.Contains(string(raw), `"width":390`) {
		t.Fatalf("panel after the viewport = %d %s", status, raw)
	}

	// A size that is not a preset is refused with a reason.
	status, out = h.userPost(t, "/api/v1/teams/"+team+"/browser/tabs/"+tabID+"/viewport", map[string]any{"preset": "watch"})
	raw, _ = json.Marshal(out)
	if status != http.StatusBadRequest || !strings.Contains(string(raw), "preset") {
		t.Fatalf("a bad preset = %d %s", status, raw)
	}

	// Closing takes the tab off the panel.
	status, out = h.userDelete(t, "/api/v1/teams/"+team+"/browser/tabs/"+tabID)
	if status != http.StatusOK {
		raw, _ = json.Marshal(out)
		t.Fatalf("close = %d %s", status, raw)
	}
	status, panel = h.userGet(t, "/api/v1/teams/"+team+"/browser")
	raw, _ = json.Marshal(panel)
	if status != http.StatusOK || strings.Contains(string(raw), tabID) {
		t.Fatalf("panel after closing = %d %s", status, raw)
	}

	// An address the browser itself owns never reaches the browser.
	status, out = h.userPost(t, "/api/v1/teams/"+team+"/browser/tabs", map[string]any{"url": "chrome://settings"})
	raw, _ = json.Marshal(out)
	if status != http.StatusBadRequest {
		t.Fatalf("a browser-internal address opened = %d %s", status, raw)
	}
}
