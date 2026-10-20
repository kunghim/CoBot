package browser

import (
	"context"
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
