package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// This file serves the live view in CoBot's page: the user sees the team's
// browser as it works, and drives it directly. Nothing here pauses or locks a
// member; user and member share the tab, like two people at one screen, and a
// member is told when the user touched its tab since its last call.

// PanelTab is one open tab of a team's browser, for the live view.
type PanelTab struct {
	ID string `json:"id"`
	// Owner is "member" (a bot's tab) or "user" (a tab the user opened
	// themselves); Member is the bot id for a member's tab only.
	Owner    string    `json:"owner"`
	Member   string    `json:"memberId,omitempty"`
	URL      string    `json:"url"`
	Title    string    `json:"title"`
	Viewport *Viewport `json:"viewport,omitempty"`
	// Busy says a member's call is working in this tab right now.
	Busy bool `json:"busy,omitempty"`
}

// Frame is one picture of a tab. Width and Height are the page's CSS pixels,
// which is the coordinate space input events use.
type Frame struct {
	Tab    string
	Width  int
	Height int
	JPEG   []byte
}

// Input is one thing the user did in the live view.
type Input struct {
	// Kind: mouse, wheel, key, text or nav.
	Kind string `json:"kind"`
	// mouse: Action is move, down or up. key: down, up or char.
	Action     string  `json:"action,omitempty"`
	X          float64 `json:"x,omitempty"`
	Y          float64 `json:"y,omitempty"`
	Button     string  `json:"button,omitempty"` // left, middle or right
	ClickCount int     `json:"clickCount,omitempty"`
	DX         float64 `json:"dx,omitempty"`
	DY         float64 `json:"dy,omitempty"`
	Key        string  `json:"key,omitempty"`
	Code       string  `json:"code,omitempty"`
	VK         int     `json:"vk,omitempty"`
	Text       string  `json:"text,omitempty"`
	Modifiers  int     `json:"modifiers,omitempty"`
	// nav: To is back, forward, reload or an address.
	To string `json:"to,omitempty"`
}

// Panel reports a team's tabs without starting a browser nobody asked for.
func (m *Manager) Panel(ctx context.Context, teamID string) (running bool, tabs []PanelTab) {
	m.mu.Lock()
	tb := m.teams[teamID]
	m.mu.Unlock()
	if tb == nil {
		return false, nil
	}
	conn := tb.getConn()
	if conn == nil || !conn.alive() {
		return false, nil
	}
	tb.mu.Lock()
	list := make([]*tab, 0, len(tb.tabs))
	for _, t := range tb.tabs {
		list = append(list, t)
	}
	tb.mu.Unlock()
	sort.Slice(list, func(i, j int) bool { return tabOrder(list[i].id) < tabOrder(list[j].id) })
	for _, t := range list {
		tabs = append(tabs, tb.panelTab(ctx, conn, t))
	}
	return true, tabs
}

// panelTab fills one tab's entry: owner, current address and title, and the
// window size it renders at when one was set.
func (tb *teamBrowser) panelTab(ctx context.Context, conn *cdpConn, t *tab) PanelTab {
	info := PanelTab{ID: t.id, Owner: "member", Viewport: t.viewportOf()}
	if t.member == OwnerUser {
		info.Owner, info.Member = "user", ""
	} else {
		info.Member = t.member
	}
	// The call lock is held for a whole member call; a free lock means idle.
	if info.Busy = t.callMu.TryLock(); info.Busy {
		t.callMu.Unlock()
	}
	infoCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var got struct {
		TargetInfo struct {
			URL   string `json:"url"`
			Title string `json:"title"`
		} `json:"targetInfo"`
	}
	if err := conn.call(infoCtx, "", "Target.getTargetInfo", map[string]any{"targetId": t.target}, &got); err == nil {
		info.URL, info.Title = got.TargetInfo.URL, got.TargetInfo.Title
	} else {
		info.URL, info.Title = t.pageInfoCached()
	}
	return info
}

// tabOrder sorts "t2" before "t10".
func tabOrder(id string) int {
	var n int
	fmt.Sscanf(strings.TrimPrefix(id, "t"), "%d", &n)
	return n
}

func (t *tab) pageInfoCached() (string, string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastURL, t.lastTitle
}

// tabOf finds a live tab of a running team browser.
func (m *Manager) tabOf(teamID, tabID string) (*teamBrowser, *tab, error) {
	m.mu.Lock()
	tb := m.teams[teamID]
	m.mu.Unlock()
	if tb == nil {
		return nil, nil, fmt.Errorf("%w: the team's browser is not running", ErrBadRequest)
	}
	tb.mu.Lock()
	t := tb.tabs[tabID]
	tb.mu.Unlock()
	if t == nil {
		return nil, nil, fmt.Errorf("%w: there is no tab %q", ErrBadRequest, tabID)
	}
	return tb, t, nil
}

// Watch sends a tab's pictures to send until ctx ends or the tab goes away.
// send must not block; the newest picture matters, so a slow reader should
// drop old ones itself. The browser is kept from idling out while watched.
func (m *Manager) Watch(ctx context.Context, teamID, tabID string, send func(Frame)) error {
	tb, t, err := m.tabOf(teamID, tabID)
	if err != nil {
		return err
	}
	tb.begin()
	defer tb.end(m.idle)

	id, first := t.addWatcher(send)
	defer func() {
		if t.removeWatcher(id) {
			stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = t.cdp(stopCtx, "Page.stopScreencast", nil, nil)
			cancel()
		}
	}()
	if first {
		params := map[string]any{"format": "jpeg", "quality": 60, "maxWidth": 1600, "maxHeight": 1200, "everyNthFrame": 1}
		if err := t.cdp(ctx, "Page.startScreencast", params, nil); err != nil {
			return err
		}
	}
	// A still page sends no frames, so show it once right away.
	if shot, w, h, err := t.shot(ctx); err == nil {
		send(Frame{Tab: tabID, Width: w, Height: h, JPEG: shot})
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			if conn := tb.getConn(); conn == nil || !conn.alive() {
				return fmt.Errorf("the browser closed")
			}
			tb.mu.Lock()
			gone := tb.tabs[tabID] == nil
			tb.mu.Unlock()
			if gone {
				return fmt.Errorf("the tab was closed")
			}
		}
	}
}

// shot takes one picture of the page and reports its CSS size.
func (t *tab) shot(ctx context.Context) ([]byte, int, int, error) {
	var out struct {
		Data string `json:"data"`
	}
	if err := t.cdp(ctx, "Page.captureScreenshot", map[string]any{"format": "jpeg", "quality": 60}, &out); err != nil {
		return nil, 0, 0, err
	}
	raw, err := base64.StdEncoding.DecodeString(out.Data)
	if err != nil {
		return nil, 0, 0, err
	}
	var metrics struct {
		CSSVisualViewport struct {
			ClientWidth  float64 `json:"clientWidth"`
			ClientHeight float64 `json:"clientHeight"`
		} `json:"cssVisualViewport"`
	}
	w, h := 1280, 900
	if t.cdp(ctx, "Page.getLayoutMetrics", nil, &metrics) == nil && metrics.CSSVisualViewport.ClientWidth > 0 {
		w, h = int(metrics.CSSVisualViewport.ClientWidth), int(metrics.CSSVisualViewport.ClientHeight)
	}
	return raw, w, h, nil
}

// addWatcher registers a picture receiver; first reports whether it is the
// only one, so the screencast must be started.
func (t *tab) addWatcher(send func(Frame)) (id int, first bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.watchers == nil {
		t.watchers = map[int]func(Frame){}
	}
	t.nextWatcher++
	t.watchers[t.nextWatcher] = send
	return t.nextWatcher, len(t.watchers) == 1
}

// removeWatcher drops a receiver; last reports whether none is left.
func (t *tab) removeWatcher(id int) (last bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.watchers, id)
	return len(t.watchers) == 0
}

// onScreencastFrame passes a picture on to the watchers and acknowledges it,
// without which the browser sends no more.
func (t *tab) onScreencastFrame(params json.RawMessage) {
	var p struct {
		Data     string `json:"data"`
		Metadata struct {
			DeviceWidth  float64 `json:"deviceWidth"`
			DeviceHeight float64 `json:"deviceHeight"`
		} `json:"metadata"`
		SessionID int `json:"sessionId"`
	}
	if json.Unmarshal(params, &p) != nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = t.cdp(ctx, "Page.screencastFrameAck", map[string]any{"sessionId": p.SessionID}, nil)
	}()
	raw, err := base64.StdEncoding.DecodeString(p.Data)
	if err != nil {
		return
	}
	t.mu.Lock()
	receivers := make([]func(Frame), 0, len(t.watchers))
	for _, send := range t.watchers {
		receivers = append(receivers, send)
	}
	t.mu.Unlock()
	frame := Frame{Tab: t.id, Width: int(p.Metadata.DeviceWidth), Height: int(p.Metadata.DeviceHeight), JPEG: raw}
	for _, send := range receivers {
		send(frame)
	}
}

// Input applies what the user did in the live view to a tab.
func (m *Manager) Input(ctx context.Context, teamID, tabID string, in Input) error {
	_, t, err := m.tabOf(teamID, tabID)
	if err != nil {
		return err
	}
	t.mu.Lock()
	t.userAt = time.Now()
	t.mu.Unlock()
	switch in.Kind {
	case "mouse":
		button := in.Button
		if button == "" {
			button = "left"
		}
		kind := map[string]string{"move": "mouseMoved", "down": "mousePressed", "up": "mouseReleased"}[in.Action]
		if kind == "" {
			return fmt.Errorf("%w: unknown mouse action %q", ErrBadRequest, in.Action)
		}
		params := map[string]any{"type": kind, "x": in.X, "y": in.Y, "modifiers": in.Modifiers}
		if kind != "mouseMoved" {
			params["button"] = button
			params["clickCount"] = max(in.ClickCount, 1)
		} else {
			params["button"] = "none"
		}
		return t.cdp(ctx, "Input.dispatchMouseEvent", params, nil)
	case "wheel":
		return t.cdp(ctx, "Input.dispatchMouseEvent", map[string]any{
			"type": "mouseWheel", "x": in.X, "y": in.Y, "deltaX": in.DX, "deltaY": in.DY, "modifiers": in.Modifiers}, nil)
	case "key":
		return t.userKey(ctx, in)
	case "text":
		if in.Text == "" {
			return nil
		}
		return t.cdp(ctx, "Input.insertText", map[string]any{"text": in.Text}, nil)
	case "nav":
		return t.userNav(ctx, in.To)
	}
	return fmt.Errorf("%w: unknown input %q", ErrBadRequest, in.Kind)
}

func (t *tab) userKey(ctx context.Context, in Input) error {
	params := map[string]any{"modifiers": in.Modifiers, "key": in.Key, "code": in.Code, "windowsVirtualKeyCode": in.VK}
	switch in.Action {
	case "down":
		params["type"] = "rawKeyDown"
		// A printable key without a command modifier types its character.
		if in.Text != "" && in.Modifiers&(modCtrl|modAlt|modMeta) == 0 {
			params["type"] = "keyDown"
			params["text"] = in.Text
			params["unmodifiedText"] = in.Text
		}
		if in.Modifiers&(modCtrl|modMeta) != 0 {
			if command, ok := shortcutCommands[strings.ToLower(in.Key)]; ok {
				params["commands"] = []string{command}
			}
		}
	case "up":
		params["type"] = "keyUp"
	default:
		return fmt.Errorf("%w: unknown key action %q", ErrBadRequest, in.Action)
	}
	return t.cdp(ctx, "Input.dispatchKeyEvent", params, nil)
}

func (t *tab) userNav(ctx context.Context, to string) error {
	switch to {
	case "reload":
		return t.cdp(ctx, "Page.reload", nil, nil)
	case "back", "forward":
		var history struct {
			CurrentIndex int `json:"currentIndex"`
			Entries      []struct {
				ID int `json:"id"`
			} `json:"entries"`
		}
		if err := t.cdp(ctx, "Page.getNavigationHistory", nil, &history); err != nil {
			return err
		}
		i := history.CurrentIndex + 1
		if to == "back" {
			i = history.CurrentIndex - 1
		}
		if i < 0 || i >= len(history.Entries) {
			return nil
		}
		return t.cdp(ctx, "Page.navigateToHistoryEntry", map[string]any{"entryId": history.Entries[i].ID}, nil)
	}
	url, err := checkURL(to)
	if err != nil {
		return err
	}
	return t.cdp(ctx, "Page.navigate", map[string]any{"url": url}, nil)
}

// OpenUserTab opens a page in a tab the user owns. The user asked for it, so
// a browser that is down comes up; the tab counts against the user's own cap
// and against neither member limit.
func (m *Manager) OpenUserTab(ctx context.Context, teamID, rawURL string) (PanelTab, error) {
	target := strings.TrimSpace(rawURL)
	if target == "" {
		target = "about:blank"
	} else if _, err := checkURL(target); err != nil {
		return PanelTab{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tb := m.team(teamID)
	tb.begin()
	defer tb.end(m.idle)
	if err := tb.ensureStarted(ctx, m); err != nil {
		return PanelTab{}, err
	}
	t, err := tb.newTab(ctx, OwnerUser)
	if err != nil {
		return PanelTab{}, err
	}
	if target != "about:blank" {
		if _, err := t.navigate(ctx, target); err != nil {
			tb.removeTab(t)
			return PanelTab{}, err
		}
	}
	conn := tb.getConn()
	if conn == nil {
		tb.removeTab(t)
		return PanelTab{}, fmt.Errorf("the browser is not running")
	}
	return tb.panelTab(ctx, conn, t), nil
}

// CloseTab closes one tab of a running team browser: the user may close
// their own page and a member's page too.
func (m *Manager) CloseTab(teamID, tabID string) error {
	tb, t, err := m.tabOf(teamID, tabID)
	if err != nil {
		return err
	}
	tb.removeTab(t)
	return nil
}

// SetViewport changes a tab's real window size; the last setter (user or
// member) wins and the panel reports what the page now renders at.
func (m *Manager) SetViewport(ctx context.Context, teamID, tabID string, req ViewportRequest) (*Viewport, error) {
	vp, err := resolveViewport(req)
	if err != nil {
		return nil, err
	}
	_, t, err := m.tabOf(teamID, tabID)
	if err != nil {
		return nil, err
	}
	if err := t.setViewport(ctx, vp); err != nil {
		return nil, err
	}
	return t.viewportOf(), nil
}

// userTabs lists the tabs the user opened for themselves, answering from
// live state only: asking never starts a browser.
func (m *Manager) userTabs(ctx context.Context, teamID string) (Result, error) {
	m.mu.Lock()
	tb := m.teams[teamID]
	m.mu.Unlock()
	out := []TabInfo{}
	if tb == nil {
		return Result{UserTabs: out}, nil
	}
	conn := tb.getConn()
	if conn == nil || !conn.alive() {
		return Result{UserTabs: out,
			Note: "团队浏览器没有在运行，用户当前没有打开的标签页。"}, nil
	}
	tb.mu.Lock()
	list := make([]*tab, 0, len(tb.tabs))
	for _, t := range tb.tabs {
		if t.member == OwnerUser {
			list = append(list, t)
		}
	}
	tb.mu.Unlock()
	sort.Slice(list, func(i, j int) bool { return tabOrder(list[i].id) < tabOrder(list[j].id) })
	for _, t := range list {
		info := TabInfo{ID: t.id}
		infoCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		var got struct {
			TargetInfo struct {
				URL   string `json:"url"`
				Title string `json:"title"`
			} `json:"targetInfo"`
		}
		if err := conn.call(infoCtx, "", "Target.getTargetInfo", map[string]any{"targetId": t.target}, &got); err == nil {
			info.URL, info.Title = got.TargetInfo.URL, got.TargetInfo.Title
		} else {
			info.URL, info.Title = t.pageInfoCached()
		}
		cancel()
		out = append(out, info)
	}
	return Result{UserTabs: out}, nil
}
