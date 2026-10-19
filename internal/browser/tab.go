package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// tab is one page a member drives. Its state is fed by the browser's events
// while the member's steps run.
type tab struct {
	id      string
	member  string
	target  string
	session string
	tb      *teamBrowser

	callMu sync.Mutex // one call at a time on a tab

	mu           sync.Mutex
	loads        int
	navStarts    int
	mainFrame    string
	inflight     map[string]struct{}
	lastNet      time.Time
	console      []string
	dialogPolicy *bool // nil: alert and beforeunload accepted, confirm and prompt dismissed
	crashed      bool
	lastURL      string
	lastTitle    string

	// The live view: receivers of the screencast, and when the user last
	// touched the tab against when the member's last call began.
	watchers      map[int]func(Frame)
	nextWatcher   int
	userAt        time.Time
	lastCallStart time.Time
}

const maxConsoleLines = 30

func (t *tab) addConsole(line string) {
	line = strings.TrimSpace(line)
	if len(line) > 300 {
		line = line[:300] + "…"
	}
	t.mu.Lock()
	if len(t.console) < maxConsoleLines {
		t.console = append(t.console, line)
	}
	t.mu.Unlock()
}

// resetCall forgets what the last call saw.
func (t *tab) resetCall() {
	t.mu.Lock()
	t.console = nil
	t.dialogPolicy = nil
	t.mu.Unlock()
}

func (t *tab) counters() (loads, navStarts int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.loads, t.navStarts
}

// cdp runs a command on this tab's page.
func (t *tab) cdp(ctx context.Context, method string, params, out any) error {
	conn := t.tb.getConn()
	if conn == nil {
		return fmt.Errorf("the browser is not running")
	}
	return conn.call(ctx, t.session, method, params, out)
}

func (tb *teamBrowser) getConn() *cdpConn {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	return tb.conn
}

// handleEvent routes a browser event to the tab it belongs to.
func (tb *teamBrowser) handleEvent(session, method string, params json.RawMessage) {
	if session == "" {
		switch method {
		case "Browser.downloadWillBegin":
			var p struct {
				GUID     string `json:"guid"`
				Filename string `json:"suggestedFilename"`
			}
			if json.Unmarshal(params, &p) == nil {
				tb.mu.Lock()
				tb.downloadNames[p.GUID] = p.Filename
				tb.mu.Unlock()
			}
		case "Browser.downloadProgress":
			var p struct {
				GUID  string `json:"guid"`
				State string `json:"state"`
			}
			if json.Unmarshal(params, &p) == nil && p.State == "completed" {
				tb.mu.Lock()
				tb.downloads = append(tb.downloads, tb.downloadNames[p.GUID])
				tb.mu.Unlock()
			}
		}
		return
	}
	tb.mu.Lock()
	t := tb.bySession[session]
	tb.mu.Unlock()
	if t != nil {
		t.onEvent(method, params)
	}
}

// loadingKinds are the requests that count toward "the network is quiet":
// long-lived sockets and event streams would otherwise never settle.
var loadingKinds = map[string]bool{
	"Document": true, "Stylesheet": true, "Image": true, "Media": true, "Font": true, "Script": true, "XHR": true, "Fetch": true,
}

func (t *tab) onEvent(method string, params json.RawMessage) {
	switch method {
	case "Page.screencastFrame":
		t.onScreencastFrame(params)
	case "Page.loadEventFired":
		t.mu.Lock()
		t.loads++
		t.mu.Unlock()
	case "Page.frameNavigated":
		var p struct {
			Frame struct {
				ID       string `json:"id"`
				ParentID string `json:"parentId"`
			} `json:"frame"`
		}
		if json.Unmarshal(params, &p) == nil && p.Frame.ParentID == "" {
			t.mu.Lock()
			t.mainFrame = p.Frame.ID
			t.mu.Unlock()
		}
	case "Page.frameStartedLoading":
		var p struct {
			FrameID string `json:"frameId"`
		}
		if json.Unmarshal(params, &p) == nil {
			t.mu.Lock()
			if t.mainFrame == "" || p.FrameID == t.mainFrame {
				t.navStarts++
			}
			t.mu.Unlock()
		}
	case "Page.javascriptDialogOpening":
		var p struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		}
		if json.Unmarshal(params, &p) != nil {
			return
		}
		t.mu.Lock()
		policy := t.dialogPolicy
		t.mu.Unlock()
		accept := p.Type == "alert" || p.Type == "beforeunload"
		if policy != nil {
			accept = *policy
		}
		outcome := "dismissed"
		if accept {
			outcome = "accepted"
		}
		t.addConsole(fmt.Sprintf("dialog %s %q %s", p.Type, p.Message, outcome))
		// The reader goroutine cannot wait for a reply to its own command.
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = t.cdp(ctx, "Page.handleJavaScriptDialog", map[string]any{"accept": accept}, nil)
		}()
	case "Network.requestWillBeSent":
		var p struct {
			RequestID string `json:"requestId"`
			Type      string `json:"type"`
		}
		if json.Unmarshal(params, &p) == nil && loadingKinds[p.Type] {
			t.mu.Lock()
			t.inflight[p.RequestID] = struct{}{}
			t.lastNet = time.Now()
			t.mu.Unlock()
		}
	case "Network.loadingFinished", "Network.loadingFailed":
		var p struct {
			RequestID string `json:"requestId"`
		}
		if json.Unmarshal(params, &p) == nil {
			t.mu.Lock()
			delete(t.inflight, p.RequestID)
			t.lastNet = time.Now()
			t.mu.Unlock()
		}
	case "Runtime.consoleAPICalled":
		var p struct {
			Type string `json:"type"`
			Args []struct {
				Value       any    `json:"value"`
				Description string `json:"description"`
			} `json:"args"`
		}
		if json.Unmarshal(params, &p) != nil || (p.Type != "error" && p.Type != "warning" && p.Type != "assert") {
			return
		}
		parts := make([]string, 0, len(p.Args))
		for _, a := range p.Args {
			if a.Value != nil {
				parts = append(parts, fmt.Sprint(a.Value))
			} else {
				parts = append(parts, a.Description)
			}
		}
		t.addConsole("console." + p.Type + ": " + strings.Join(parts, " "))
	case "Runtime.exceptionThrown":
		var p struct {
			ExceptionDetails struct {
				Text      string `json:"text"`
				URL       string `json:"url"`
				Line      int    `json:"lineNumber"`
				Exception struct {
					Description string `json:"description"`
				} `json:"exception"`
			} `json:"exceptionDetails"`
		}
		if json.Unmarshal(params, &p) == nil {
			text := p.ExceptionDetails.Exception.Description
			if text == "" {
				text = p.ExceptionDetails.Text
			}
			if i := strings.Index(text, "\n"); i > 0 {
				text = text[:i]
			}
			t.addConsole("exception: " + text)
		}
	case "Log.entryAdded":
		var p struct {
			Entry struct {
				Level string `json:"level"`
				Text  string `json:"text"`
				URL   string `json:"url"`
			} `json:"entry"`
		}
		if json.Unmarshal(params, &p) != nil || (p.Entry.Level != "error" && p.Entry.Level != "warning") {
			return
		}
		// Every headless page asks for a favicon; a miss says nothing.
		if strings.HasSuffix(p.Entry.URL, "/favicon.ico") {
			return
		}
		line := p.Entry.Level + ": " + p.Entry.Text
		if p.Entry.URL != "" {
			line += " (" + p.Entry.URL + ")"
		}
		t.addConsole(line)
	case "Inspector.targetCrashed":
		t.mu.Lock()
		t.crashed = true
		t.mu.Unlock()
	}
}

// newTab opens a tab for a member and makes it the member's current one.
func (tb *teamBrowser) newTab(ctx context.Context, member string) (*tab, error) {
	tb.mu.Lock()
	mine, all := 0, len(tb.tabs)
	for _, t := range tb.tabs {
		if t.member == member {
			mine++
		}
	}
	if mine >= maxTabsPerMember || all >= maxTabsPerTeam {
		tb.mu.Unlock()
		return nil, fmt.Errorf("%w: close a tab before opening another (limit %d per member)", ErrTooMany, maxTabsPerMember)
	}
	tb.nextTab++
	id := fmt.Sprintf("t%d", tb.nextTab)
	conn := tb.conn
	tb.mu.Unlock()
	if conn == nil {
		return nil, fmt.Errorf("the browser is not running")
	}

	var created struct {
		TargetID string `json:"targetId"`
	}
	if err := conn.call(ctx, "", "Target.createTarget", map[string]any{"url": "about:blank"}, &created); err != nil {
		return nil, err
	}
	var attached struct {
		SessionID string `json:"sessionId"`
	}
	if err := conn.call(ctx, "", "Target.attachToTarget", map[string]any{"targetId": created.TargetID, "flatten": true}, &attached); err != nil {
		return nil, err
	}
	t := &tab{id: id, member: member, target: created.TargetID, session: attached.SessionID, tb: tb, inflight: map[string]struct{}{}}
	// Register first: events begin with the first enabled domain.
	tb.mu.Lock()
	tb.tabs[id] = t
	tb.bySession[t.session] = t
	tb.current[member] = id
	tb.mu.Unlock()
	for _, domain := range []string{"Page.enable", "Runtime.enable", "Log.enable", "Network.enable"} {
		if err := t.cdp(ctx, domain, nil, nil); err != nil {
			tb.removeTab(t)
			return nil, err
		}
	}
	if tb.ua != "" {
		_ = t.cdp(ctx, "Emulation.setUserAgentOverride", map[string]any{"userAgent": tb.ua}, nil)
	}
	var tree struct {
		FrameTree struct {
			Frame struct {
				ID string `json:"id"`
			} `json:"frame"`
		} `json:"frameTree"`
	}
	if err := t.cdp(ctx, "Page.getFrameTree", nil, &tree); err == nil {
		t.mu.Lock()
		t.mainFrame = tree.FrameTree.Frame.ID
		t.mu.Unlock()
	}
	return t, nil
}

// removeTab forgets a tab, closing its page when it still exists.
func (tb *teamBrowser) removeTab(t *tab) {
	tb.mu.Lock()
	delete(tb.tabs, t.id)
	delete(tb.bySession, t.session)
	if tb.current[t.member] == t.id {
		delete(tb.current, t.member)
		// Another of the member's tabs becomes current.
		var ids []string
		for id, other := range tb.tabs {
			if other.member == t.member {
				ids = append(ids, id)
			}
		}
		sort.Strings(ids)
		if len(ids) > 0 {
			tb.current[t.member] = ids[len(ids)-1]
		}
	}
	conn := tb.conn
	tb.mu.Unlock()
	if conn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = conn.call(ctx, "", "Target.closeTarget", map[string]any{"targetId": t.target}, nil)
		cancel()
	}
}

// pickTab returns the tab a call works on.
func (tb *teamBrowser) pickTab(ctx context.Context, member, id string) (*tab, error) {
	tb.mu.Lock()
	if id == "" {
		id = tb.current[member]
	} else if t := tb.tabs[id]; t == nil {
		var mine []string
		for other, t := range tb.tabs {
			if t.member == member {
				mine = append(mine, other)
			}
		}
		sort.Strings(mine)
		tb.mu.Unlock()
		return nil, fmt.Errorf("%w: there is no tab %q (your tabs: %s)", ErrBadRequest, id, strings.Join(mine, ", "))
	} else if t.member != member {
		tb.mu.Unlock()
		return nil, fmt.Errorf("%w: tab %q", ErrTabNotYour, id)
	}
	t := tb.tabs[id]
	tb.mu.Unlock()
	if t != nil {
		return t, nil
	}
	return tb.newTab(ctx, member)
}

// ensureDownloadDir points the browser's downloads at the team's folder.
func (tb *teamBrowser) ensureDownloadDir(ctx context.Context, teamDir string) {
	dir := filepath.Join(teamDir, ".cobot", "downloads")
	tb.mu.Lock()
	same := tb.downloadDir == dir
	conn := tb.conn
	tb.mu.Unlock()
	if same || conn == nil {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	if err := conn.call(ctx, "", "Browser.setDownloadBehavior", map[string]any{"behavior": "allow", "downloadPath": dir, "eventsEnabled": true}, nil); err == nil {
		tb.mu.Lock()
		tb.downloadDir = dir
		tb.mu.Unlock()
	}
}

// run executes a call's steps on the member's tab.
func (tb *teamBrowser) run(ctx context.Context, env Env, req Request, steps []Step) (Result, error) {
	cur, err := tb.pickTab(ctx, env.MemberID, req.Tab)
	if err != nil {
		return Result{}, err
	}
	cur.callMu.Lock()
	defer func() { cur.callMu.Unlock() }()
	cur.resetCall()
	// The user shares this tab; say so when they touched it since the last call.
	cur.mu.Lock()
	userTouched := cur.userAt.After(cur.lastCallStart) && !cur.userAt.IsZero()
	cur.lastCallStart = time.Now()
	cur.mu.Unlock()
	tb.ensureDownloadDir(ctx, env.TeamDir)
	tb.mu.Lock()
	downloadsBefore := len(tb.downloads)
	tb.mu.Unlock()

	run := &callRun{tb: tb, env: env, cur: cur}
	result := Result{Steps: make([]StepResult, 0, len(steps))}
	if userTouched {
		result.Note = "用户刚在这个标签页里操作过，页面可能已经变了（比如填过表单、登录过、换了页面）；以这次返回的快照为准，别沿用旧的编号。"
	}
	for _, step := range steps {
		if result.Failed {
			result.Steps = append(result.Steps, StepResult{Action: step.Action, Skipped: true})
			continue
		}
		stepCtx, cancel := context.WithTimeout(ctx, stepTimeout(step))
		detail, stepErr := run.exec(stepCtx, step)
		cancel()
		sr := StepResult{Action: step.Action, OK: stepErr == nil, Detail: detail}
		if stepErr != nil {
			sr.Error = describeError(stepErr)
			result.Failed = true
			if isGone(stepErr) {
				tb.removeTab(run.cur)
			}
		}
		result.Steps = append(result.Steps, sr)
		// A step that opened a tab moved the call to it.
		if run.cur != cur {
			cur.callMu.Unlock()
			cur = run.cur
			cur.callMu.Lock()
		}
	}
	if run.cur != cur {
		cur = run.cur
	}
	result.Screenshots = run.screenshots
	wantSnapshot := req.Snapshot == nil || *req.Snapshot
	if wantSnapshot || req.Screenshot {
		finishCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		if req.Screenshot {
			if path, serr := run.screenshot(finishCtx, false); serr == nil {
				result.Screenshots = append(result.Screenshots, path)
			} else {
				result.Steps = append(result.Steps, StepResult{Action: "screenshot", Error: describeError(serr)})
			}
		}
		if wantSnapshot {
			if text, serr := run.snapshot(finishCtx); serr == nil {
				result.Snapshot = text
			}
		}
		cancel()
	}
	if !isGoneTab(tb, cur) {
		infoCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		result.URL, result.Title = cur.pageInfo(infoCtx)
		cancel()
	}
	result.Tab = cur.id
	cur.mu.Lock()
	result.Console = append([]string(nil), cur.console...)
	cur.mu.Unlock()
	tb.mu.Lock()
	if len(tb.downloads) > downloadsBefore {
		result.Downloads = append([]string(nil), tb.downloads[downloadsBefore:]...)
	}
	tb.mu.Unlock()
	result.Tabs = tb.listTabs(env.MemberID, cur.id)
	return result, nil
}

func isGoneTab(tb *teamBrowser, t *tab) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	return tb.tabs[t.id] == nil
}

// pageInfo reads the tab's address and title, falling back to the last known.
func (t *tab) pageInfo(ctx context.Context) (string, string) {
	var out struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	if err := t.cdp(ctx, "Runtime.evaluate", map[string]any{"expression": "location.href + '\\n' + document.title", "returnByValue": true}, &out); err == nil {
		if url, title, ok := strings.Cut(out.Result.Value, "\n"); ok {
			t.mu.Lock()
			t.lastURL, t.lastTitle = url, title
			t.mu.Unlock()
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastURL, t.lastTitle
}

// listTabs lists a member's tabs.
func (tb *teamBrowser) listTabs(member, current string) []TabInfo {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	var out []TabInfo
	for id, t := range tb.tabs {
		if t.member != member {
			continue
		}
		t.mu.Lock()
		info := TabInfo{ID: id, URL: t.lastURL, Title: t.lastTitle, Current: id == current}
		t.mu.Unlock()
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
