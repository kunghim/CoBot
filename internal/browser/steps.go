package browser

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed snapshot.js
var snapshotJS string

// maxSnapshot keeps one page's outline within what a model should read.
const maxSnapshot = 12000

var knownActions = map[string]bool{
	"goto": true, "back": true, "forward": true, "reload": true,
	"click": true, "doubleClick": true, "hover": true, "type": true, "key": true, "select": true,
	"scroll": true, "wait": true, "eval": true, "screenshot": true, "snapshot": true,
	"newTab": true, "closeTab": true, "upload": true, "viewport": true, "dialog": true,
}

// Actions lists the step actions in a stable order, for the tool's schema.
func Actions() []string {
	return []string{"goto", "back", "forward", "reload", "click", "doubleClick", "hover", "type", "key", "select",
		"scroll", "wait", "eval", "screenshot", "snapshot", "newTab", "closeTab", "upload", "viewport", "dialog"}
}

func stepTimeout(step Step) time.Duration {
	if step.TimeoutMs > 0 {
		return min(time.Duration(step.TimeoutMs)*time.Millisecond, maxStepTime)
	}
	switch step.Action {
	case "goto", "reload", "back", "forward", "newTab":
		return 30 * time.Second
	case "wait":
		return 12 * time.Second
	}
	return defaultStepTime
}

// callRun is the state of one call's steps: the tab they work on (a step
// may open another) and what they produced.
type callRun struct {
	tb          *teamBrowser
	env         Env
	cur         *tab
	screenshots []string
	shots       int
}

func (r *callRun) exec(ctx context.Context, st Step) (string, error) {
	switch st.Action {
	case "goto":
		return r.cur.navigate(ctx, st.URL)
	case "back", "forward":
		return r.cur.history(ctx, st.Action == "forward")
	case "reload":
		loads, _ := r.cur.counters()
		if err := r.cur.cdp(ctx, "Page.reload", nil, nil); err != nil {
			return "", err
		}
		r.cur.waitLoad(ctx, loads, 25*time.Second)
		return "已刷新", nil
	case "click", "doubleClick":
		return r.click(ctx, st)
	case "hover":
		return r.hover(ctx, st)
	case "type":
		return r.typeText(ctx, st)
	case "key":
		return r.pressKey(ctx, st.Key)
	case "select":
		return r.selectOption(ctx, st)
	case "scroll":
		return r.scroll(ctx, st)
	case "wait":
		return r.wait(ctx, st)
	case "eval":
		return r.eval(ctx, st)
	case "screenshot":
		path, err := r.screenshot(ctx, st.FullPage)
		if err != nil {
			return "", err
		}
		r.screenshots = append(r.screenshots, path)
		return path, nil
	case "snapshot":
		return r.snapshot(ctx)
	case "newTab":
		return r.newTab(ctx, st)
	case "closeTab":
		return r.closeTab(ctx, st)
	case "upload":
		return r.upload(ctx, st)
	case "viewport":
		return r.viewport(ctx, st)
	case "dialog":
		if st.Accept == nil {
			return "", fmt.Errorf("%w: dialog needs accept", ErrBadRequest)
		}
		r.cur.mu.Lock()
		r.cur.dialogPolicy = st.Accept
		r.cur.mu.Unlock()
		return "下一个对话框将" + map[bool]string{true: "接受", false: "取消"}[*st.Accept], nil
	}
	return "", fmt.Errorf("%w: unknown action %q", ErrBadRequest, st.Action)
}

// ---- navigation ----

// blockedSchemes are pages of the browser itself: settings, extensions and
// the like are not the member's to open, and script URLs run code in place.
var blockedSchemes = map[string]bool{
	"chrome": true, "chrome-extension": true, "chrome-untrusted": true, "chrome-search": true, "edge": true,
	"brave": true, "devtools": true, "javascript": true, "view-source": true, "vivaldi": true, "opera": true,
}

// checkURL returns the address to open: an address without a scheme gets
// http for this machine (localhost and the like) and https for anything else,
// and the browser's own pages are refused.
func checkURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("%w: the address is empty", ErrBadRequest)
	}
	if !strings.Contains(raw, "://") && !strings.HasPrefix(raw, "about:") && !strings.HasPrefix(raw, "data:") {
		host := raw
		if i := strings.IndexAny(host, "/?#"); i >= 0 {
			host = host[:i]
		}
		name := strings.ToLower(host)
		if strings.HasPrefix(name, "[") {
			if end := strings.Index(name, "]"); end > 0 {
				name = name[:end+1]
			}
		} else if i := strings.LastIndex(name, ":"); i >= 0 {
			name = name[:i]
		}
		if name == "localhost" || strings.HasSuffix(name, ".localhost") || name == "127.0.0.1" || name == "[::1]" {
			raw = "http://" + raw
		} else {
			raw = "https://" + raw
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrBadRequest, err)
	}
	scheme := strings.ToLower(u.Scheme)
	switch {
	case blockedSchemes[scheme]:
		return "", fmt.Errorf("%w: %s pages are not available", ErrBadRequest, scheme)
	case scheme == "http" || scheme == "https" || scheme == "file" || scheme == "data":
		return raw, nil
	case scheme == "about" && raw == "about:blank":
		return raw, nil
	}
	return "", fmt.Errorf("%w: the address type %q is not supported (http, https, file, data and about:blank are)", ErrBadRequest, scheme)
}

func (t *tab) navigate(ctx context.Context, raw string) (string, error) {
	target, err := checkURL(raw)
	if err != nil {
		return "", err
	}
	loads, _ := t.counters()
	var res struct {
		LoaderID  string `json:"loaderId"`
		ErrorText string `json:"errorText"`
	}
	if err := t.cdp(ctx, "Page.navigate", map[string]any{"url": target}, &res); err != nil {
		return "", err
	}
	if res.ErrorText != "" {
		if res.ErrorText == "net::ERR_ABORTED" {
			return "没有新页面：这个地址触发了下载或被中止", nil
		}
		return "", fmt.Errorf("无法打开 %s：%s", target, res.ErrorText)
	}
	if res.LoaderID == "" {
		return "已跳转（同一页面内）", nil
	}
	if !t.waitLoad(ctx, loads, 25*time.Second) {
		return "已发出请求，页面仍在加载", nil
	}
	return "已打开 " + target, nil
}

// waitLoad waits for the page's load event; false when it did not come.
func (t *tab) waitLoad(ctx context.Context, startLoads int, limit time.Duration) bool {
	deadline := time.NewTimer(limit)
	defer deadline.Stop()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		if loads, _ := t.counters(); loads > startLoads {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return false
		case <-tick.C:
		}
	}
}

func (t *tab) history(ctx context.Context, forward bool) (string, error) {
	var h struct {
		CurrentIndex int `json:"currentIndex"`
		Entries      []struct {
			ID  int    `json:"id"`
			URL string `json:"url"`
		} `json:"entries"`
	}
	if err := t.cdp(ctx, "Page.getNavigationHistory", nil, &h); err != nil {
		return "", err
	}
	index := h.CurrentIndex - 1
	if forward {
		index = h.CurrentIndex + 1
	}
	if index < 0 || index >= len(h.Entries) {
		return "", errors.New("没有可以回到的页面")
	}
	loads, _ := t.counters()
	if err := t.cdp(ctx, "Page.navigateToHistoryEntry", map[string]any{"entryId": h.Entries[index].ID}, nil); err != nil {
		return "", err
	}
	t.waitLoad(ctx, loads, 15*time.Second)
	return "已到 " + h.Entries[index].URL, nil
}

// settle gives a click or key press its effect: a navigation it started is
// waited for, and so are the requests the page sent in answer.
func (t *tab) settle(ctx context.Context, startLoads, startNav int) {
	sleep(ctx, 120*time.Millisecond)
	if _, navs := t.counters(); navs > startNav {
		t.waitLoad(ctx, startLoads, 10*time.Second)
	}
	t.quiet(ctx)
}

// quiet waits, for a moment, until the page has no requests in flight.
func (t *tab) quiet(ctx context.Context) {
	quietBy := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(quietBy) {
		t.mu.Lock()
		idle := len(t.inflight) == 0 && time.Since(t.lastNet) > 150*time.Millisecond
		t.mu.Unlock()
		if idle || ctx.Err() != nil {
			return
		}
		sleep(ctx, 40*time.Millisecond)
	}
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// ---- finding and acting on elements ----

// resolve finds the element a step names and returns its remote object id.
func (r *callRun) resolve(ctx context.Context, st Step) (string, error) {
	var expression string
	switch {
	case st.Ref != "":
		ref, _ := json.Marshal(st.Ref)
		expression = `(function(){const m=window.__cobotRefs;if(!m)return undefined;const e=m.get(` + string(ref) + `);return e&&e.isConnected?e:null})()`
	case st.Selector != "":
		sel, _ := json.Marshal(st.Selector)
		expression = `document.querySelector(` + string(sel) + `)`
	case st.Text != "":
		text, _ := json.Marshal(st.Text)
		expression = findByTextJS + `(` + string(text) + `)`
	default:
		return "", fmt.Errorf("%w: %s needs a ref, a selector or text", ErrBadRequest, st.Action)
	}
	var out struct {
		Result struct {
			Type     string `json:"type"`
			Subtype  string `json:"subtype"`
			ObjectID string `json:"objectId"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := r.cur.cdp(ctx, "Runtime.evaluate", map[string]any{"expression": expression}, &out); err != nil {
		return "", err
	}
	if out.ExceptionDetails != nil {
		return "", fmt.Errorf("选择器无效：%s", out.ExceptionDetails.Text)
	}
	if out.Result.ObjectID == "" {
		switch {
		case st.Ref != "" && out.Result.Type == "undefined":
			return "", fmt.Errorf("还没有读过页面快照，没有 %s 这个编号；先取快照（snapshot）", st.Ref)
		case st.Ref != "":
			return "", fmt.Errorf("找不到元素 %s：页面可能已变化，重新取快照后再试", st.Ref)
		}
		return "", errors.New("找不到这个元素")
	}
	return out.Result.ObjectID, nil
}

// findByTextJS returns the visible control whose name matches the text.
const findByTextJS = `(function(){return function(text){
  const want=text.trim().toLowerCase();
  const nodes=document.querySelectorAll('button,a[href],input[type=button],input[type=submit],[role=button],[role=link],[role=tab],[role=menuitem],summary,label');
  let loose=null;
  for(const el of nodes){
    const r=el.getBoundingClientRect(); if(r.width===0&&r.height===0) continue;
    const name=((el.getAttribute('aria-label')||el.value||el.innerText||el.textContent||'')+'').replace(/\s+/g,' ').trim().toLowerCase();
    if(name===want) return el;
    if(!loose&&name.includes(want)) loose=el;
  }
  return loose;
}})()`

// callOn runs a function with the element as this and returns its value.
func (r *callRun) callOn(ctx context.Context, objectID, function string, out any) error {
	var res struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text      string `json:"text"`
			Exception struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	err := r.cur.cdp(ctx, "Runtime.callFunctionOn", map[string]any{
		"objectId": objectID, "functionDeclaration": function, "returnByValue": true, "awaitPromise": true,
	}, &res)
	if err != nil {
		return err
	}
	if res.ExceptionDetails != nil {
		text := res.ExceptionDetails.Exception.Description
		if text == "" {
			text = res.ExceptionDetails.Text
		}
		return errors.New(firstLine(text))
	}
	if out != nil && len(res.Result.Value) > 0 {
		return json.Unmarshal(res.Result.Value, out)
	}
	return nil
}

type geometry struct {
	X, Y    float64
	W, H    float64
	Covered bool
	Hit     string
	Desc    string
}

const geometryJS = `function(){
  this.scrollIntoView({block:'center',inline:'center'});
  const r=this.getBoundingClientRect(), x=r.left+r.width/2, y=r.top+r.height/2;
  const hit=document.elementFromPoint(x,y);
  const covered=!!hit&&hit!==this&&!this.contains(hit)&&!hit.contains(this);
  const name=(el)=>el.tagName.toLowerCase()+(el.id?'#'+el.id:'')+((typeof el.className==='string'&&el.className.trim())?'.'+el.className.trim().split(/\s+/).slice(0,2).join('.'):'');
  const text=((this.getAttribute('aria-label')||this.value||this.innerText||'')+'').replace(/\s+/g,' ').trim().slice(0,40);
  return {X:x,Y:y,W:r.width,H:r.height,Covered:covered,Hit:covered?name(hit):'',Desc:name(this)+(text?' "'+text+'"':'')};
}`

func (r *callRun) locate(ctx context.Context, st Step) (string, geometry, error) {
	id, err := r.resolve(ctx, st)
	if err != nil {
		return "", geometry{}, err
	}
	var g geometry
	if err := r.callOn(ctx, id, geometryJS, &g); err != nil {
		return "", geometry{}, err
	}
	if g.W == 0 && g.H == 0 {
		return "", geometry{}, fmt.Errorf("元素 %s 不可见（没有大小）", g.Desc)
	}
	return id, g, nil
}

func (r *callRun) mouse(ctx context.Context, kind string, g geometry, clicks int) error {
	params := map[string]any{"type": kind, "x": g.X, "y": g.Y}
	if kind != "mouseMoved" {
		params["button"] = "left"
		params["clickCount"] = clicks
	}
	return r.cur.cdp(ctx, "Input.dispatchMouseEvent", params, nil)
}

func (r *callRun) click(ctx context.Context, st Step) (string, error) {
	id, g, err := r.locate(ctx, st)
	if err != nil {
		return "", err
	}
	loads, navs := r.cur.counters()
	if g.Covered {
		if !st.Force {
			return "", fmt.Errorf("%s 被 %s 挡住，点不到；先处理遮挡的内容（关闭弹层等），或确认后加 force 用脚本点击", g.Desc, g.Hit)
		}
		if err := r.callOn(ctx, id, `function(){this.click()}`, nil); err != nil {
			return "", err
		}
	} else {
		if err := r.mouse(ctx, "mouseMoved", g, 0); err != nil {
			return "", err
		}
		rounds := 1
		if st.Action == "doubleClick" {
			rounds = 2
		}
		for i := 1; i <= rounds; i++ {
			if err := r.mouse(ctx, "mousePressed", g, i); err != nil {
				return "", err
			}
			if err := r.mouse(ctx, "mouseReleased", g, i); err != nil {
				return "", err
			}
		}
	}
	r.cur.settle(ctx, loads, navs)
	verb := "已点击 "
	if st.Action == "doubleClick" {
		verb = "已双击 "
	}
	return verb + g.Desc, nil
}

func (r *callRun) hover(ctx context.Context, st Step) (string, error) {
	_, g, err := r.locate(ctx, st)
	if err != nil {
		return "", err
	}
	if err := r.mouse(ctx, "mouseMoved", g, 0); err != nil {
		return "", err
	}
	sleep(ctx, 150*time.Millisecond)
	return "鼠标已移到 " + g.Desc, nil
}

func (r *callRun) typeText(ctx context.Context, st Step) (string, error) {
	id, err := r.resolve(ctx, st)
	if err != nil {
		return "", err
	}
	var info struct {
		Tag      string
		Type     string
		Editable bool
	}
	if err := r.callOn(ctx, id, `function(){const t=this.tagName;return {Tag:t,Type:(this.type||'').toLowerCase(),Editable:this.isContentEditable||t==='TEXTAREA'||(t==='INPUT'&&!['checkbox','radio','button','submit','reset','file','image','range','color'].includes((this.type||'').toLowerCase()))}}`, &info); err != nil {
		return "", err
	}
	// Credentials are the user's to enter, never the member's or the model's.
	if info.Type == "password" {
		return "", errors.New("密码输入框不能由成员填写：需要登录时告知用户，由用户自己在浏览器里登录")
	}
	if !info.Editable {
		return "", fmt.Errorf("%s 不是可以输入文字的元素", strings.ToLower(info.Tag))
	}
	clear := st.Clear == nil || *st.Clear
	focus := `function(){this.focus();try{this.setSelectionRange(this.value.length,this.value.length)}catch(e){}}`
	if clear {
		focus = `function(){this.focus();if(this.select){this.select()}else{const s=getSelection(),r=document.createRange();r.selectNodeContents(this);s.removeAllRanges();s.addRange(r)}}`
	}
	if err := r.callOn(ctx, id, focus, nil); err != nil {
		return "", err
	}
	loads, navs := r.cur.counters()
	switch {
	case st.Text != "":
		if err := r.cur.cdp(ctx, "Input.insertText", map[string]any{"text": st.Text}, nil); err != nil {
			return "", err
		}
	case clear:
		if _, err := r.pressKey(ctx, "Backspace"); err != nil {
			return "", err
		}
	}
	if st.Submit {
		if _, err := r.pressKey(ctx, "Enter"); err != nil {
			return "", err
		}
		r.cur.settle(ctx, loads, navs)
	}
	var value string
	_ = r.callOn(ctx, id, `function(){return (this.value!==undefined?this.value:this.innerText)+''}`, &value)
	if len(value) > 80 {
		value = value[:80] + "…"
	}
	return fmt.Sprintf("已输入，现在的内容：%q", value), nil
}

func (r *callRun) selectOption(ctx context.Context, st Step) (string, error) {
	id, err := r.resolve(ctx, st)
	if err != nil {
		return "", err
	}
	want, _ := json.Marshal(st.Value)
	var chosen string
	err = r.callOn(ctx, id, `function(){
	  if(this.tagName!=='SELECT') throw new Error('不是下拉框');
	  const want=`+string(want)+`;
	  const opts=Array.from(this.options);
	  const hit=opts.find(o=>o.value===want)||opts.find(o=>o.textContent.trim()===want)||opts.find(o=>o.textContent.trim().includes(want));
	  if(!hit) throw new Error('没有这个选项，可选：'+opts.map(o=>JSON.stringify(o.textContent.trim())).join('、'));
	  this.value=hit.value;
	  this.dispatchEvent(new Event('input',{bubbles:true}));this.dispatchEvent(new Event('change',{bubbles:true}));
	  return hit.textContent.trim();
	}`, &chosen)
	if err != nil {
		return "", err
	}
	return "已选择 " + chosen, nil
}

func (r *callRun) scroll(ctx context.Context, st Step) (string, error) {
	if st.Ref != "" || st.Selector != "" {
		id, err := r.resolve(ctx, st)
		if err != nil {
			return "", err
		}
		var pos string
		err = r.callOn(ctx, id, fmt.Sprintf(`function(){this.scrollBy(%d,%d);return Math.round(this.scrollTop)+'/'+Math.round(this.scrollHeight-this.clientHeight)}`, st.DX, st.DY), &pos)
		return "已滚动，位置 " + pos, err
	}
	var script string
	switch st.Value {
	case "top":
		script = `window.scrollTo(0,0)`
	case "bottom":
		script = `window.scrollTo(0,document.documentElement.scrollHeight)`
	default:
		script = fmt.Sprintf(`window.scrollBy(%d,%d)`, st.DX, st.DY)
	}
	var pos string
	if err := r.evalString(ctx, `(function(){`+script+`;const s=document.scrollingElement;return Math.round(s.scrollTop)+'/'+Math.round(s.scrollHeight-s.clientHeight)})()`, &pos); err != nil {
		return "", err
	}
	return "已滚动，位置 " + pos, nil
}

func (r *callRun) evalString(ctx context.Context, expression string, out *string) error {
	var res struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text      string `json:"text"`
			Exception struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	if err := r.cur.cdp(ctx, "Runtime.evaluate", map[string]any{"expression": expression, "returnByValue": true, "awaitPromise": true}, &res); err != nil {
		return err
	}
	if res.ExceptionDetails != nil {
		text := res.ExceptionDetails.Exception.Description
		if text == "" {
			text = res.ExceptionDetails.Text
		}
		return errors.New(firstLine(text))
	}
	*out = string(res.Result.Value)
	var s string
	if json.Unmarshal(res.Result.Value, &s) == nil {
		*out = s
	}
	return nil
}

func (r *callRun) eval(ctx context.Context, st Step) (string, error) {
	if strings.TrimSpace(st.Expression) == "" {
		return "", fmt.Errorf("%w: eval needs an expression", ErrBadRequest)
	}
	var value string
	if err := r.evalString(ctx, st.Expression, &value); err != nil {
		return "", err
	}
	if len(value) > 3000 {
		value = value[:3000] + "…（已截断）"
	}
	return value, nil
}

func (r *callRun) wait(ctx context.Context, st Step) (string, error) {
	switch {
	case st.Ms > 0:
		sleep(ctx, min(time.Duration(st.Ms)*time.Millisecond, 10*time.Second))
		return fmt.Sprintf("已等待 %d 毫秒", st.Ms), nil
	case st.Text != "", st.Selector != "":
		var probe string
		if st.Text != "" {
			text, _ := json.Marshal(st.Text)
			probe = `document.body&&document.body.innerText.includes(` + string(text) + `)`
		} else {
			sel, _ := json.Marshal(st.Selector)
			probe = `!!document.querySelector(` + string(sel) + `)`
		}
		for {
			var got string
			if err := r.evalString(ctx, probe, &got); err != nil {
				return "", err
			}
			if (got == "true") != st.Gone {
				return "条件已满足", nil
			}
			select {
			case <-ctx.Done():
				return "", errors.New("等待超时，条件始终没有满足")
			case <-time.After(150 * time.Millisecond):
			}
		}
	default:
		// No condition: wait for the page to stop loading.
		r.cur.quiet(ctx)
		return "页面已安静", nil
	}
}

func (r *callRun) screenshot(ctx context.Context, fullPage bool) (string, error) {
	var shot struct {
		Data string `json:"data"`
	}
	params := map[string]any{"format": "jpeg", "quality": 80}
	if fullPage {
		params["captureBeyondViewport"] = true
	}
	if err := r.cur.cdp(ctx, "Page.captureScreenshot", params, &shot); err != nil {
		return "", err
	}
	data, err := base64.StdEncoding.DecodeString(shot.Data)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(r.env.TeamDir, ".cobot", "browser")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	r.shots++
	member := r.env.MemberID
	if len(member) > 8 {
		member = member[:8]
	}
	path := filepath.Join(dir, fmt.Sprintf("shot-%s-%s-%d.jpg", member, time.Now().Format("20060102-150405"), r.shots))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	pruneShots(dir, 100)
	return path, nil
}

// pruneShots keeps the newest screenshots of a team.
func pruneShots(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) <= keep {
		return
	}
	// Names carry a sortable timestamp, so the oldest sort first.
	for _, e := range entries[:len(entries)-keep] {
		_ = os.Remove(filepath.Join(dir, e.Name()))
	}
}

func (r *callRun) snapshot(ctx context.Context) (string, error) {
	var text string
	if err := r.evalString(ctx, snapshotJS, &text); err != nil {
		return "", err
	}
	if len(text) > maxSnapshot {
		text = text[:maxSnapshot] + "\n…（快照太长，已截断）"
	}
	if strings.TrimSpace(text) == "" {
		text = "（页面上没有可读内容）"
	}
	return text, nil
}

func (r *callRun) viewport(ctx context.Context, st Step) (string, error) {
	if st.Width <= 0 || st.Height <= 0 {
		if err := r.cur.setViewport(ctx, nil); err != nil {
			return "", err
		}
		return "已恢复默认窗口大小", nil
	}
	if err := r.cur.setViewport(ctx, &Viewport{Width: st.Width, Height: st.Height, Mobile: st.Mobile}); err != nil {
		return "", err
	}
	return fmt.Sprintf("窗口大小 %dx%d", st.Width, st.Height), nil
}

// ---- tabs and files ----

func (r *callRun) newTab(ctx context.Context, st Step) (string, error) {
	t, err := r.tb.newTab(ctx, r.env.MemberID)
	if err != nil {
		return "", err
	}
	r.cur = t
	if strings.TrimSpace(st.URL) == "" {
		return "已打开新标签页 " + t.id, nil
	}
	detail, err := t.navigate(ctx, st.URL)
	if err != nil {
		return "", err
	}
	return "新标签页 " + t.id + "：" + detail, nil
}

func (r *callRun) closeTab(ctx context.Context, st Step) (string, error) {
	target := r.cur
	if st.Value != "" {
		r.tb.mu.Lock()
		target = r.tb.tabs[st.Value]
		r.tb.mu.Unlock()
		if target == nil || target.member != r.env.MemberID {
			return "", fmt.Errorf("%w: there is no tab %q of yours", ErrBadRequest, st.Value)
		}
	}
	r.tb.removeTab(target)
	if target == r.cur {
		// Carry on in another tab of the member's, or a fresh blank one.
		r.tb.mu.Lock()
		next := r.tb.tabs[r.tb.current[r.env.MemberID]]
		r.tb.mu.Unlock()
		if next == nil {
			var err error
			if next, err = r.tb.newTab(ctx, r.env.MemberID); err != nil {
				return "", err
			}
		}
		r.cur = next
	}
	return "已关闭标签页 " + target.id, nil
}

func (r *callRun) upload(ctx context.Context, st Step) (string, error) {
	if len(st.Files) == 0 {
		return "", fmt.Errorf("%w: upload needs files", ErrBadRequest)
	}
	root, err := filepath.EvalSymlinks(r.env.TeamDir)
	if err != nil {
		return "", err
	}
	var files []string
	for _, name := range st.Files {
		abs, err := filepath.Abs(name)
		if err != nil {
			return "", err
		}
		real, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return "", fmt.Errorf("找不到文件 %s", name)
		}
		// The browser runs as the user, outside the sandbox: only what the
		// team's own folder holds may leave through it.
		if rel, err := filepath.Rel(root, real); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("只能上传团队目录里的文件：%s 在目录之外", name)
		}
		files = append(files, real)
	}
	id, err := r.resolve(ctx, st)
	if err != nil {
		return "", err
	}
	if err := r.cur.cdp(ctx, "DOM.setFileInputFiles", map[string]any{"files": files, "objectId": id}, nil); err != nil {
		return "", err
	}
	return fmt.Sprintf("已选择 %d 个文件", len(files)), nil
}

// ---- errors ----

func firstLine(text string) string {
	if i := strings.Index(text, "\n"); i > 0 {
		return text[:i]
	}
	return text
}

func describeError(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "超时"
	case errors.Is(err, context.Canceled):
		return "已取消"
	case isGone(err):
		return "这个标签页已经关闭或崩溃，请重新打开（newTab）"
	}
	var ce *cdpError
	if errors.As(err, &ce) {
		return ce.Message
	}
	return err.Error()
}

// isGone reports that the page or the browser behind a command is gone.
func isGone(err error) bool {
	var ce *cdpError
	if errors.As(err, &ce) {
		m := ce.Message
		return strings.Contains(m, "Session with given id not found") || strings.Contains(m, "Target closed") ||
			strings.Contains(m, "No target with given id") || strings.Contains(m, "Inspected target navigated or closed")
	}
	return errors.Is(err, errWSClosed)
}
