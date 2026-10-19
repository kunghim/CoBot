package browser

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// These tests run a real Chromium-based browser, so they skip on a machine
// without one. They are what proves the steps do what they say.

const testPage = `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><title>测试页</title></head><body>
<h1>测试页</h1>
<form id="f"><label for="t">新待办</label><input id="t" placeholder="例如：买牛奶"><button type="submit">添加</button></form>
<ul id="list"></ul>
<p id="status">就绪</p>
<button id="boom" onclick="console.error('boom happened')">制造错误</button>
<button id="ask" onclick="if(confirm('真的删除吗？')){document.getElementById('status').textContent='已删除'}">删除</button>
<button id="later" onclick="setTimeout(()=>{document.getElementById('status').textContent='稍后出现'},400)">延迟</button>
<select id="s"><option value="a">甲</option><option value="b">乙</option></select>
<input id="pw" type="password" aria-label="密码">
<input id="file" type="file" aria-label="附件">
<a href="/two">第二页</a>
<div id="cover" style="position:fixed;inset:0;background:#0000;display:none"></div>
<button id="hide" onclick="document.getElementById('cover').style.display='block'">盖住</button>
<div style="height:2500px"></div><p id="bottom">页面底部</p>
<script>
const key='todos';
const list=document.getElementById('list');
function render(){
  const items=JSON.parse(localStorage.getItem(key)||'[]');
  list.innerHTML='';
  items.forEach((it,i)=>{const li=document.createElement('li');const c=document.createElement('input');c.type='checkbox';c.checked=it.done;c.setAttribute('aria-label',it.text);
    c.onchange=()=>{items[i].done=c.checked;localStorage.setItem(key,JSON.stringify(items));};li.appendChild(c);li.appendChild(document.createTextNode(' '+it.text));list.appendChild(li);});
}
document.getElementById('f').onsubmit=(e)=>{e.preventDefault();const t=document.getElementById('t');if(!t.value.trim())return;
  const items=JSON.parse(localStorage.getItem(key)||'[]');items.push({text:t.value,done:false});localStorage.setItem(key,JSON.stringify(items));t.value='';render();};
render();
</script></body></html>`

const secondPage = `<!doctype html><html><head><meta charset="utf-8"><title>第二页</title></head><body><h1>第二页</h1><a href="/">回首页</a></body></html>`

var refPattern = regexp.MustCompile(`\[(e\d+)\] `)

// refOf finds the reference of the snapshot line that contains the text.
func refOf(t *testing.T, snapshot, contains string) string {
	t.Helper()
	for _, line := range strings.Split(snapshot, "\n") {
		if strings.Contains(line, contains) {
			if m := refPattern.FindStringSubmatch(line); m != nil {
				return m[1]
			}
		}
	}
	t.Fatalf("no control %q in the snapshot:\n%s", contains, snapshot)
	return ""
}

func stepFailed(r Result) (StepResult, bool) {
	for _, s := range r.Steps {
		if s.Error != "" {
			return s, true
		}
	}
	return StepResult{}, false
}

func TestRealBrowserDrivesAPage(t *testing.T) {
	if _, err := Find(); err != nil {
		t.Skip("no Chromium-based browser on this machine")
	}
	t.Setenv("COBOT_BROWSER_NO_SANDBOX", "1") // CI containers refuse Chrome's own sandbox
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, testPage)
	})
	mux.HandleFunc("/two", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, secondPage) })
	server := httptest.NewServer(mux)
	defer server.Close()

	teamDir := t.TempDir()
	profiles, _ := os.MkdirTemp("", "cobot-browser-test")
	m := NewManager(profiles, nil)
	defer func() {
		m.Shutdown()
		// Edge releases its profile files a moment after closing.
		for i := 0; i < 20 && os.RemoveAll(profiles) != nil; i++ {
			time.Sleep(250 * time.Millisecond)
		}
	}()
	env := Env{TeamID: "team-1", MemberID: "member-aaaa1111", TeamDir: teamDir}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	run := func(req Request) Result {
		t.Helper()
		res, err := m.Run(ctx, env, req)
		if err != nil {
			t.Fatalf("run %+v: %v", req, err)
		}
		return res
	}

	// Opening a page returns its outline, with references for the controls.
	first := run(Request{URL: server.URL + "/"})
	if first.Title != "测试页" || first.Failed {
		t.Fatalf("first call = %+v", first)
	}
	for _, want := range []string{"# 测试页", `textbox "新待办"`, `button "添加"`, `combobox`, `value=(空)`} {
		if !strings.Contains(first.Snapshot, want) {
			t.Fatalf("snapshot lacks %q:\n%s", want, first.Snapshot)
		}
	}
	tab := first.Tab

	// Typing and submitting go through the page's own handlers.
	box := refOf(t, first.Snapshot, `textbox "新待办"`)
	added := run(Request{Steps: []Step{
		{Action: "type", Ref: box, Text: "买牛奶", Submit: true},
		{Action: "type", Ref: box, Text: "写周报"},
		{Action: "key", Key: "Enter"},
	}})
	if s, bad := stepFailed(added); bad {
		t.Fatalf("typing failed: %+v", s)
	}
	for _, want := range []string{`checkbox "买牛奶"`, `checkbox "写周报"`} {
		if !strings.Contains(added.Snapshot, want) {
			t.Fatalf("after adding, snapshot lacks %q:\n%s", want, added.Snapshot)
		}
	}
	if added.Tab != tab {
		t.Fatalf("the member's tab changed: %s -> %s", tab, added.Tab)
	}

	// A click, a reload, and what the page kept.
	milk := refOf(t, added.Snapshot, `checkbox "买牛奶"`)
	reloaded := run(Request{Steps: []Step{{Action: "click", Ref: milk}, {Action: "reload"}}})
	if s, bad := stepFailed(reloaded); bad {
		t.Fatalf("click/reload failed: %+v", s)
	}
	if !regexp.MustCompile(`checkbox "买牛奶" checked`).MatchString(reloaded.Snapshot) || strings.Contains(reloaded.Snapshot, `checkbox "写周报" checked`) {
		t.Fatalf("state after reload:\n%s", reloaded.Snapshot)
	}
	stored := run(Request{Steps: []Step{{Action: "eval", Expression: `localStorage.getItem('todos')`}}, Snapshot: ptr(false)})
	if got := stored.Steps[0].Detail; !strings.Contains(got, `"done":true`) {
		t.Fatalf("localStorage = %q", got)
	}

	t.Run("console errors and dialogs", func(t *testing.T) {
		snap := run(Request{}).Snapshot
		boom := refOf(t, snap, `"制造错误"`)
		ask := refOf(t, snap, `"删除"`)
		res := run(Request{Steps: []Step{{Action: "click", Ref: boom}}})
		if len(res.Console) == 0 || !strings.Contains(strings.Join(res.Console, "|"), "boom happened") {
			t.Fatalf("console = %v", res.Console)
		}
		// A confirm is dismissed unless the member says otherwise.
		res = run(Request{Steps: []Step{{Action: "click", Ref: ask}}})
		if !strings.Contains(strings.Join(res.Console, "|"), "dismissed") || strings.Contains(res.Snapshot, "已删除") {
			t.Fatalf("dismissed confirm: console=%v\n%s", res.Console, res.Snapshot)
		}
		yes := true
		res = run(Request{Steps: []Step{{Action: "dialog", Accept: &yes}, {Action: "click", Ref: ask}}})
		if !strings.Contains(res.Snapshot, "已删除") {
			t.Fatalf("accepted confirm:\n%s", res.Snapshot)
		}
	})

	t.Run("waiting, selecting, scrolling", func(t *testing.T) {
		snap := run(Request{URL: server.URL + "/"}).Snapshot
		later := refOf(t, snap, `"延迟"`)
		sel := refOf(t, snap, "combobox")
		res := run(Request{Steps: []Step{
			{Action: "click", Ref: later},
			{Action: "wait", Text: "稍后出现"},
			{Action: "select", Ref: sel, Value: "乙"},
			{Action: "scroll", Value: "bottom"},
			{Action: "wait", Ms: 50},
		}})
		if s, bad := stepFailed(res); bad {
			t.Fatalf("step failed: %+v", s)
		}
		if !strings.Contains(res.Snapshot, `value="乙"`) || !strings.Contains(res.Snapshot, "页面底部") {
			t.Fatalf("after the steps:\n%s", res.Snapshot)
		}
		// Waiting for something that never comes ends in a clear failure.
		res = run(Request{Steps: []Step{{Action: "wait", Text: "永远不会出现", TimeoutMs: 600}}, Snapshot: ptr(false)})
		if s, bad := stepFailed(res); !bad || !strings.Contains(s.Error, "超时") {
			t.Fatalf("a wait that never ends: %+v", res.Steps)
		}
	})

	t.Run("credentials are not handled", func(t *testing.T) {
		snap := run(Request{URL: server.URL + "/"}).Snapshot
		pw := refOf(t, snap, `"密码"`)
		res := run(Request{Steps: []Step{{Action: "type", Ref: pw, Text: "hunter2"}}, Snapshot: ptr(false)})
		if s, bad := stepFailed(res); !bad || !strings.Contains(s.Error, "密码") {
			t.Fatalf("typing a password: %+v", res.Steps)
		}
	})

	t.Run("an overlay blocks a click unless forced", func(t *testing.T) {
		snap := run(Request{URL: server.URL + "/"}).Snapshot
		hide, ask := refOf(t, snap, `"盖住"`), refOf(t, snap, `"删除"`)
		res := run(Request{Steps: []Step{{Action: "click", Ref: hide}, {Action: "click", Ref: ask}}, Snapshot: ptr(false)})
		if s, bad := stepFailed(res); !bad || !strings.Contains(s.Error, "挡住") {
			t.Fatalf("a covered click: %+v", res.Steps)
		}
		if got := res.Steps[len(res.Steps)-1]; !got.Skipped && res.Steps[1].Error == "" {
			t.Fatalf("steps after the failure: %+v", res.Steps)
		}
	})

	t.Run("navigation, history and tabs", func(t *testing.T) {
		res := run(Request{URL: server.URL + "/", Steps: []Step{{Action: "click", Text: "第二页"}, {Action: "back"}, {Action: "forward"}}, Snapshot: ptr(false)})
		if s, bad := stepFailed(res); bad || !strings.HasSuffix(res.URL, "/two") || res.Title != "第二页" {
			t.Fatalf("link, back, forward: %+v at %s (%v)", res.Steps, res.URL, s)
		}
		res = run(Request{Steps: []Step{{Action: "newTab", URL: server.URL + "/"}}, Snapshot: ptr(false)})
		if res.Tab == tab || len(res.Tabs) != 2 || !res.Tabs[1].Current {
			t.Fatalf("after newTab: %+v", res.Tabs)
		}
		other := res.Tab
		// Another member neither sees nor may use this member's tabs.
		_, err := m.Run(ctx, Env{TeamID: "team-1", MemberID: "member-bbbb2222", TeamDir: teamDir}, Request{Tab: other, Snapshot: ptr(false)})
		if err == nil || !strings.Contains(err.Error(), "another member") {
			t.Fatalf("using another member's tab = %v", err)
		}
		res = run(Request{Steps: []Step{{Action: "closeTab"}}, Snapshot: ptr(false)})
		if res.Tab != tab || len(res.Tabs) != 1 {
			t.Fatalf("after closeTab: tab %s, tabs %+v", res.Tab, res.Tabs)
		}
	})

	t.Run("screenshots, and what may not be opened or uploaded", func(t *testing.T) {
		res := run(Request{URL: server.URL + "/", Screenshot: true, Snapshot: ptr(false)})
		if len(res.Screenshots) != 1 || !strings.HasPrefix(res.Screenshots[0], filepath.Join(teamDir, ".cobot", "browser")) {
			t.Fatalf("screenshots = %v", res.Screenshots)
		}
		if info, err := os.Stat(res.Screenshots[0]); err != nil || info.Size() < 1000 {
			t.Fatalf("screenshot file: %v %v", info, err)
		}
		bad := run(Request{Steps: []Step{{Action: "goto", URL: "chrome://settings"}}, Snapshot: ptr(false)})
		if s, failed := stepFailed(bad); !failed || !strings.Contains(s.Error, "chrome") {
			t.Fatalf("a browser-internal page: %+v", bad.Steps)
		}
		snap := run(Request{URL: server.URL + "/"}).Snapshot
		file := refOf(t, snap, `"附件"`)
		outside := filepath.Join(t.TempDir(), "secret.txt")
		os.WriteFile(outside, []byte("x"), 0o600)
		up := run(Request{Steps: []Step{{Action: "upload", Ref: file, Files: []string{outside}}}, Snapshot: ptr(false)})
		if s, failed := stepFailed(up); !failed || !strings.Contains(s.Error, "团队目录") {
			t.Fatalf("an upload from outside the team folder: %+v", up.Steps)
		}
		inside := filepath.Join(teamDir, "note.txt")
		os.WriteFile(inside, []byte("hello"), 0o600)
		up = run(Request{Steps: []Step{{Action: "upload", Ref: file, Files: []string{inside}}}, Snapshot: ptr(false)})
		if s, failed := stepFailed(up); failed {
			t.Fatalf("an upload from inside: %+v", s)
		}
	})

	t.Run("the profile outlives the browser", func(t *testing.T) {
		m.CloseTeam("team-1")
		res := run(Request{URL: server.URL + "/"})
		if !regexp.MustCompile(`checkbox "买牛奶" checked`).MatchString(res.Snapshot) {
			t.Fatalf("after restarting the browser, the page lost its state:\n%s", res.Snapshot)
		}
	})
}

func ptr[T any](v T) *T { return &v }

func TestRequestsAreCheckedBeforeAnyBrowserStarts(t *testing.T) {
	m := NewManager(t.TempDir(), nil)
	m.find = func() (Installed, error) { return Installed{}, ErrNotFound }
	env := Env{TeamID: "t", MemberID: "m", TeamDir: t.TempDir()}
	ctx := context.Background()
	if _, err := m.Run(ctx, env, Request{Steps: []Step{{Action: "teleport"}}}); err == nil || !strings.Contains(err.Error(), "unknown action") {
		t.Fatalf("unknown action = %v", err)
	}
	many := make([]Step, maxSteps+1)
	for i := range many {
		many[i] = Step{Action: "wait", Ms: 1}
	}
	if _, err := m.Run(ctx, env, Request{Steps: many}); err == nil || !strings.Contains(err.Error(), "at most") {
		t.Fatalf("too many steps = %v", err)
	}
	if _, err := m.Run(ctx, Env{TeamID: "t"}, Request{URL: "https://example.com"}); err == nil {
		t.Fatal("a call without a member ran")
	}
	// With no browser installed the call says so, plainly.
	if _, err := m.Run(ctx, env, Request{URL: "https://example.com"}); err != ErrNotFound {
		t.Fatalf("no browser = %v", err)
	}
}

func TestAddressesAreCheckedAndCompleted(t *testing.T) {
	for raw, want := range map[string]string{
		"https://github.com/openai":   "https://github.com/openai",
		"github.com/openai":           "https://github.com/openai",
		"localhost:3000":              "http://localhost:3000",
		"127.0.0.1:8080/app?x=1":      "http://127.0.0.1:8080/app?x=1",
		"[::1]:9000":                  "http://[::1]:9000",
		"app.localhost/x":             "http://app.localhost/x",
		"file:///C:/work/index.html":  "file:///C:/work/index.html",
		"about:blank":                 "about:blank",
		"data:text/html,<b>hi</b>":    "data:text/html,<b>hi</b>",
		"  https://example.com/a b  ": "https://example.com/a b",
	} {
		got, err := checkURL(raw)
		if err != nil || got != want {
			t.Errorf("checkURL(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	for _, raw := range []string{"", "chrome://settings", "chrome-extension://abc/x.html", "javascript:alert(1)", "view-source:https://a.b", "edge://flags", "ftp://example.com/x", "about:config"} {
		if got, err := checkURL(raw); err == nil {
			t.Errorf("checkURL(%q) = %q, want a refusal", raw, got)
		}
	}
}

func TestKeysParse(t *testing.T) {
	cases := map[string]struct {
		key  string
		mods int
	}{
		"Enter": {"Enter", 0}, "a": {"a", 0}, "A": {"A", modShift}, "Control+a": {"a", modCtrl},
		"Shift+Tab": {"Tab", modShift}, "ctrl+shift+z": {"z", modCtrl | modShift}, "5": {"5", 0},
	}
	for spec, want := range cases {
		def, mods, err := parseKey(spec)
		if err != nil || def.key != want.key || mods != want.mods {
			t.Errorf("parseKey(%q) = %q %d, %v; want %q %d", spec, def.key, mods, err, want.key, want.mods)
		}
	}
	for _, spec := range []string{"", "Hyper+a", "Banana"} {
		if _, _, err := parseKey(spec); err == nil {
			t.Errorf("parseKey(%q) was accepted", spec)
		}
	}
}
