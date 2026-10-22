package browser

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLiveViewShowsAndDrivesATab(t *testing.T) {
	if _, err := Find(); err != nil {
		t.Skip("no Chromium-based browser on this machine")
	}
	t.Setenv("COBOT_BROWSER_NO_SANDBOX", "1")
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `<!doctype html><title>面板页</title><input id="q" style="position:absolute;left:20px;top:20px;width:200px;height:30px">
<button id="b" style="position:absolute;left:20px;top:80px;width:120px;height:40px" onclick="document.title='点过了'">点我</button>`)
	}))
	defer site.Close()
	profiles, _ := os.MkdirTemp("", "cobot-browser-test")
	m := NewManager(profiles, nil)
	defer func() {
		m.Shutdown()
		for i := 0; i < 20 && os.RemoveAll(profiles) != nil; i++ {
			time.Sleep(250 * time.Millisecond)
		}
	}()
	env := Env{TeamID: "team-p", MemberID: "member-1", TeamDir: t.TempDir()}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if running, _ := m.Panel(ctx, "team-p"); running {
		t.Fatal("the panel reported a browser nobody started")
	}
	if _, err := m.Run(ctx, env, Request{URL: site.URL, Snapshot: ptr(false)}); err != nil {
		t.Fatal(err)
	}
	running, tabs := m.Panel(ctx, "team-p")
	if !running || len(tabs) != 1 || tabs[0].Title != "面板页" || tabs[0].Member != "member-1" {
		t.Fatalf("panel = %v %+v", running, tabs)
	}
	tabID := tabs[0].ID

	frames := make(chan Frame, 4)
	watchCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		done <- m.Watch(watchCtx, "team-p", tabID, func(f Frame) {
			select {
			case frames <- f:
			default:
			}
		})
	}()
	select {
	case f := <-frames:
		if len(f.JPEG) < 500 || f.Width < 300 || f.Height < 200 {
			t.Fatalf("frame = %d bytes %dx%d", len(f.JPEG), f.Width, f.Height)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("no frame arrived")
	}

	// The user clicks the button and types into the field.
	click := func(x, y float64) {
		for _, action := range []string{"move", "down", "up"} {
			if err := m.Input(ctx, "team-p", tabID, Input{Kind: "mouse", Action: action, X: x, Y: y, ClickCount: 1}); err != nil {
				t.Fatal(err)
			}
		}
	}
	click(60, 100)
	click(100, 35)
	for _, ch := range "hi" {
		in := Input{Kind: "key", Key: string(ch), Code: "Key" + strings.ToUpper(string(ch)), VK: int(ch - 'a' + 'A'), Text: string(ch)}
		for _, action := range []string{"down", "up"} {
			in.Action = action
			if err := m.Input(ctx, "team-p", tabID, in); err != nil {
				t.Fatal(err)
			}
		}
	}
	// The page changed, so the view must show it: a new picture arrives.
	changed := false
	deadline := time.After(10 * time.Second)
	for !changed {
		select {
		case f := <-frames:
			changed = len(f.JPEG) > 0
		case <-deadline:
			t.Fatal("no new picture after the user's input")
		}
	}
	res, err := m.Run(ctx, env, Request{Steps: []Step{{Action: "eval", Expression: `document.title + '|' + document.getElementById('q').value`}}, Snapshot: ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Steps[0].Detail; !strings.Contains(got, "点过了|hi") {
		t.Fatalf("after the user's input the page says %q", got)
	}
	// The member is told the user touched its tab, once.
	if !strings.Contains(res.Note, "用户刚在") {
		t.Fatalf("no notice after user input: %q", res.Note)
	}
	res, _ = m.Run(ctx, env, Request{Snapshot: ptr(false)})
	if res.Note != "" {
		t.Fatalf("the notice repeated: %q", res.Note)
	}

	// Navigation from the address bar goes through the same address check.
	if err := m.Input(ctx, "team-p", tabID, Input{Kind: "nav", To: "chrome://settings"}); err == nil {
		t.Fatal("a browser-internal address was opened from the live view")
	}
	if err := m.Input(ctx, "team-p", tabID, Input{Kind: "nav", To: "reload"}); err != nil {
		t.Fatal(err)
	}
	stop()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Watch did not end")
	}
}

// TestUserOwnTabsAndViewport drives what the workspace's browser tabs stand
// on: the user's own tabs and their cap, the window size a tab renders at,
// the panel reporting owner and viewport, and a member using the user's page
// when it is pointed at one.
func TestUserOwnTabsAndViewport(t *testing.T) {
	if _, err := Find(); err != nil {
		t.Skip("no Chromium-based browser on this machine")
	}
	t.Setenv("COBOT_BROWSER_NO_SANDBOX", "1")
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `<!doctype html><meta name="viewport" content="width=device-width, initial-scale=1"><title>用户页</title><h1>用户的页面</h1>`)
	}))
	defer site.Close()
	profiles, _ := os.MkdirTemp("", "cobot-browser-test")
	defer func() { time.Sleep(time.Second); _ = os.RemoveAll(profiles) }()
	m := NewManager(profiles, nil)
	defer m.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	env := Env{TeamID: "team-u", MemberID: "member-1", TeamDir: profiles}

	// The user opens their own page and the browser comes up for them.
	opened, err := m.OpenUserTab(ctx, "team-u", site.URL)
	if err != nil {
		t.Fatal(err)
	}
	if opened.Owner != "user" || opened.Member != "" || !strings.Contains(opened.Title, "用户页") {
		t.Fatalf("opened tab = %+v", opened)
	}
	running, tabs := m.Panel(ctx, "team-u")
	if !running || len(tabs) != 1 || tabs[0].Owner != "user" || tabs[0].Viewport != nil {
		t.Fatalf("panel = %v %+v", running, tabs)
	}

	// Their cap is their own: the sixth tab is refused...
	for i := 1; i < maxTabsPerUser; i++ {
		if _, err := m.OpenUserTab(ctx, "team-u", "about:blank"); err != nil {
			t.Fatalf("user tab %d: %v", i+1, err)
		}
	}
	if _, err := m.OpenUserTab(ctx, "team-u", "about:blank"); !errors.Is(err, ErrTooMany) {
		t.Fatalf("the sixth user tab = %v, want ErrTooMany", err)
	}

	// The user's page renders at the phone size: ask the page itself, which
	// is the layout viewport the emulation set (a screencast frame would
	// only mirror it, on the screencast's own schedule).
	viewport, err := m.SetViewport(ctx, "team-u", opened.ID, ViewportRequest{Preset: "phone"})
	if err != nil {
		t.Fatal(err)
	}
	if viewport == nil || viewport.Width != 390 || viewport.Height != 844 || !viewport.Mobile {
		t.Fatalf("viewport = %+v", viewport)
	}
	res, err := m.Run(ctx, env, Request{Tab: opened.ID,
		Steps:    []Step{{Action: "eval", Expression: "innerWidth + 'x' + innerHeight"}},
		Snapshot: ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Steps[0].Detail != "390x844" {
		t.Fatalf("the page lays out at %s, want 390x844", res.Steps[0].Detail)
	}
	if _, tabs := m.Panel(ctx, "team-u"); tabs[0].Viewport == nil || tabs[0].Viewport.Width != 390 {
		t.Fatalf("the panel does not report the new viewport: %+v", tabs)
	}

	// The member finds the user's tabs when asked, and only then.
	res, err = m.Run(ctx, env, Request{UserTabs: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.UserTabs) != maxTabsPerUser {
		t.Fatalf("userTabs listed %d tabs, want %d", len(res.UserTabs), maxTabsPerUser)
	}
	if !strings.Contains(res.UserTabs[0].Title, "用户页") {
		t.Fatalf("the user's page is not first in the list: %+v", res.UserTabs[0])
	}

	// Pointed at one, the member works in the user's page.
	res, err = m.Run(ctx, env, Request{Tab: opened.ID,
		Steps:    []Step{{Action: "eval", Expression: "document.title"}},
		Snapshot: ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Steps[0].Detail, "用户页") {
		t.Fatalf("the member's step on the user's tab = %+v", res.Steps[0])
	}

	// Closing takes it away from the panel and from the list.
	if err := m.CloseTab("team-u", opened.ID); err != nil {
		t.Fatal(err)
	}
	if _, tabs := m.Panel(ctx, "team-u"); len(tabs) != maxTabsPerUser-1 {
		t.Fatalf("panel lists %d tabs after closing, want %d", len(tabs), maxTabsPerUser-1)
	}
	res, err = m.Run(ctx, env, Request{UserTabs: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.UserTabs) != maxTabsPerUser-1 {
		t.Fatalf("userTabs lists %d tabs after closing, want %d", len(res.UserTabs), maxTabsPerUser-1)
	}
}
