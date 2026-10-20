// Package browser gives team members a real browser they can drive. Codex's
// own command sandbox cannot run one (Chromium needs the very handles a
// restricted token denies), and Codex's built-in browser belongs to its
// desktop app, so CoBot runs a browser itself, outside the sandbox, and lets
// members drive it through one tool: navigate, read the page as a compact
// snapshot, click, type, screenshot. One browser serves a team, in a profile
// of its own under the CoBot data root; each member has its own tabs.
package browser

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Request is one call of the browser tool.
type Request struct {
	// Tab is one of the member's tabs; empty means its current tab, opened
	// on first use.
	Tab string `json:"tab,omitempty"`
	// URL is shorthand for a first goto step.
	URL   string `json:"url,omitempty"`
	Steps []Step `json:"steps,omitempty"`
	// Snapshot reports the page at the end; on unless set to false.
	Snapshot *bool `json:"snapshot,omitempty"`
	// Screenshot saves a screenshot at the end.
	Screenshot bool `json:"screenshot,omitempty"`
	// Help asks for the usage and safety guide instead of running anything.
	Help bool `json:"help,omitempty"`
	// UserTabs lists the tabs the user opened for themselves and runs
	// nothing else; it never starts a browser.
	UserTabs bool `json:"userTabs,omitempty"`
}

//go:embed guide.md
var guideText string

// Guide is the browser's usage, safety and confirmation guide. A member reads
// it on demand instead of carrying it in every instruction.
func Guide() string { return guideText }

// Step is one action. Which fields matter depends on the action.
type Step struct {
	Action     string   `json:"action"`
	URL        string   `json:"url,omitempty"`
	Ref        string   `json:"ref,omitempty"`
	Selector   string   `json:"selector,omitempty"`
	Text       string   `json:"text,omitempty"`
	Key        string   `json:"key,omitempty"`
	Value      string   `json:"value,omitempty"`
	Expression string   `json:"expression,omitempty"`
	Ms         int      `json:"ms,omitempty"`
	DX         int      `json:"dx,omitempty"`
	DY         int      `json:"dy,omitempty"`
	Clear      *bool    `json:"clear,omitempty"`
	Submit     bool     `json:"submit,omitempty"`
	Accept     *bool    `json:"accept,omitempty"`
	Files      []string `json:"files,omitempty"`
	FullPage   bool     `json:"fullPage,omitempty"`
	Gone       bool     `json:"gone,omitempty"`
	Force      bool     `json:"force,omitempty"`
	Width      int      `json:"width,omitempty"`
	Height     int      `json:"height,omitempty"`
	Mobile     bool     `json:"mobile,omitempty"`
	TimeoutMs  int      `json:"timeoutMs,omitempty"`
}

// Env is where the call comes from.
type Env struct {
	TeamID   string
	MemberID string
	// TeamDir is the team's working directory: screenshots and downloads go
	// under its .cobot folder, and uploads may only come from inside it.
	TeamDir string
}

// Result is what a call reports back.
type Result struct {
	Tab         string       `json:"tab"`
	URL         string       `json:"url"`
	Title       string       `json:"title"`
	Steps       []StepResult `json:"steps"`
	Failed      bool         `json:"failed,omitempty"`
	Snapshot    string       `json:"snapshot,omitempty"`
	Screenshots []string     `json:"screenshots,omitempty"`
	Console     []string     `json:"console,omitempty"`
	Downloads   []string     `json:"downloads,omitempty"`
	Tabs        []TabInfo    `json:"tabs,omitempty"`
	// UserTabs answers a userTabs request: the tabs the user opened for
	// themselves, whether or not a member may use them.
	UserTabs []TabInfo `json:"userTabs,omitempty"`
	// Guide and Note answer a help request, or a first call made before the
	// guide was read: nothing was run.
	Guide string `json:"guide,omitempty"`
	Note  string `json:"note,omitempty"`
}

// StepResult is how one step went. The first failing step ends the call;
// the steps after it are reported as skipped.
type StepResult struct {
	Action  string `json:"action"`
	OK      bool   `json:"ok"`
	Detail  string `json:"detail,omitempty"`
	Error   string `json:"error,omitempty"`
	Skipped bool   `json:"skipped,omitempty"`
}

// TabInfo lists one of the member's tabs.
type TabInfo struct {
	ID      string `json:"id"`
	URL     string `json:"url,omitempty"`
	Title   string `json:"title,omitempty"`
	Current bool   `json:"current,omitempty"`
}

// Limits keep one call and one team's browser bounded.
const (
	maxSteps         = 30
	maxTabsPerMember = 5
	maxTabsPerTeam   = 15
	// The user's own tabs have their own cap and count against neither of
	// the member limits above.
	maxTabsPerUser  = 5
	defaultStepTime = 15 * time.Second
	maxStepTime     = 40 * time.Second
	// A tool call must answer within the bridge's 60 seconds.
	callBudget = 50 * time.Second
	// A browser nobody used for this long is closed; its profile stays.
	defaultIdle = 10 * time.Minute
)

// OwnerUser marks a tab the user opened themselves. Member ids are UUIDs,
// so no member can collide with this reserved owner.
const OwnerUser = "__user__"

// Error kinds the caller can tell apart.
var (
	ErrBadRequest = errors.New("bad browser request")
	ErrTabNotYour = errors.New("tab belongs to another member")
	ErrTooMany    = errors.New("too many tabs")
)

// Manager runs the teams' browsers.
type Manager struct {
	dir    string // profiles live under <dir>/<team>/profile
	logger *slog.Logger
	idle   time.Duration
	find   func() (Installed, error)

	mu    sync.Mutex
	teams map[string]*teamBrowser
}

// NewManager makes a manager that keeps browser profiles under dir.
func NewManager(dir string, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{dir: dir, logger: logger, idle: defaultIdle, find: Find, teams: map[string]*teamBrowser{}}
}

// Run executes one call for a member of a team.
func (m *Manager) Run(ctx context.Context, env Env, req Request) (Result, error) {
	if env.TeamID == "" || env.MemberID == "" || env.TeamDir == "" {
		return Result{}, fmt.Errorf("%w: missing team or member", ErrBadRequest)
	}
	// Listing the user's own tabs answers from live state and starts
	// nothing: a browser nobody opened has no tabs to show.
	if req.UserTabs {
		ctx, cancel := context.WithTimeout(ctx, callBudget)
		defer cancel()
		return m.userTabs(ctx, env.TeamID)
	}
	steps, err := normalize(req)
	if err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, callBudget)
	defer cancel()
	tb := m.team(env.TeamID)
	tb.begin()
	defer tb.end(m.idle)
	if err := tb.ensureStarted(ctx, m); err != nil {
		return Result{}, err
	}
	return tb.run(ctx, env, req, steps)
}

// CloseTeam closes a team's browser, keeping its profile (and so its logins).
func (m *Manager) CloseTeam(teamID string) {
	m.mu.Lock()
	tb := m.teams[teamID]
	delete(m.teams, teamID)
	m.mu.Unlock()
	if tb != nil {
		tb.stop()
	}
}

// PurgeTeam closes a team's browser and deletes its profile.
func (m *Manager) PurgeTeam(teamID string) error {
	m.CloseTeam(teamID)
	return os.RemoveAll(filepath.Join(m.dir, teamID))
}

// Shutdown closes every browser, for the core's exit.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	teams := m.teams
	m.teams = map[string]*teamBrowser{}
	m.mu.Unlock()
	for _, tb := range teams {
		tb.stop()
	}
}

func (m *Manager) team(id string) *teamBrowser {
	m.mu.Lock()
	defer m.mu.Unlock()
	tb := m.teams[id]
	if tb == nil {
		tb = &teamBrowser{
			id: id, profile: filepath.Join(m.dir, id, "profile"),
			tabs: map[string]*tab{}, bySession: map[string]*tab{}, current: map[string]string{}, downloadNames: map[string]string{},
		}
		m.teams[id] = tb
	}
	return tb
}

// teamBrowser is one team's browser process and its tabs.
type teamBrowser struct {
	id      string
	profile string

	startMu sync.Mutex // one start or stop at a time
	cmd     *exec.Cmd
	exited  chan struct{} // closed when the launched process ends
	conn    *cdpConn
	ua      string

	mu            sync.Mutex // everything below
	tabs          map[string]*tab
	bySession     map[string]*tab
	current       map[string]string // member -> current tab id
	nextTab       int
	downloadNames map[string]string
	downloads     []string
	busy          int
	idleTimer     *time.Timer
	downloadDir   string
}

func (tb *teamBrowser) begin() {
	tb.mu.Lock()
	tb.busy++
	if tb.idleTimer != nil {
		tb.idleTimer.Stop()
	}
	tb.mu.Unlock()
}

// end counts a call as finished and arms the idle timeout.
func (tb *teamBrowser) end(idle time.Duration) {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.busy--
	if tb.busy > 0 {
		return
	}
	if tb.idleTimer != nil {
		tb.idleTimer.Stop()
	}
	tb.idleTimer = time.AfterFunc(idle, func() {
		tb.mu.Lock()
		busy := tb.busy
		tb.mu.Unlock()
		if busy == 0 {
			tb.stop()
		}
	})
}

// ensureStarted makes sure a browser is up, starting or reconnecting to one.
func (tb *teamBrowser) ensureStarted(ctx context.Context, m *Manager) error {
	tb.startMu.Lock()
	defer tb.startMu.Unlock()
	if c := tb.getConn(); c != nil && c.alive() {
		return nil
	}
	tb.dropTabs()
	if err := os.MkdirAll(tb.profile, 0o700); err != nil {
		return err
	}
	wsURL, err := tb.reconnectable()
	if err != nil || wsURL == "" {
		installed, ferr := m.find()
		if ferr != nil {
			return ferr
		}
		if wsURL, err = tb.launch(ctx, installed); err != nil {
			return err
		}
	}
	conn, err := dialCDP(ctx, wsURL, tb.handleEvent)
	if err != nil {
		tb.killProcess()
		return fmt.Errorf("connect to browser: %w", err)
	}
	var version struct {
		UserAgent string `json:"userAgent"`
	}
	if err := conn.call(ctx, "", "Browser.getVersion", nil, &version); err != nil {
		conn.close()
		tb.killProcess()
		return err
	}
	// Headless Chrome says so in its user agent, and some sites refuse it for
	// that alone; say what the browser is, without the "Headless".
	tb.ua = strings.Replace(version.UserAgent, "HeadlessChrome", "Chrome", 1)
	tb.mu.Lock()
	tb.conn = conn
	tb.mu.Unlock()
	m.logger.Info("team browser started", "team", tb.id)
	return nil
}

// reconnectable finds a browser a previous core left holding this profile.
func (tb *teamBrowser) reconnectable() (string, error) {
	data, err := os.ReadFile(filepath.Join(tb.profile, "DevToolsActivePort"))
	if err != nil {
		return "", nil
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		return "", nil
	}
	url := "ws://127.0.0.1:" + strings.TrimSpace(lines[0]) + strings.TrimSpace(lines[1])
	client := http.Client{Timeout: time.Second}
	resp, err := client.Get("http://127.0.0.1:" + strings.TrimSpace(lines[0]) + "/json/version")
	if err != nil {
		// Nobody answers: the file is stale.
		_ = os.Remove(filepath.Join(tb.profile, "DevToolsActivePort"))
		return "", nil
	}
	defer resp.Body.Close()
	var info struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&info) == nil && info.WebSocketDebuggerURL != "" {
		return info.WebSocketDebuggerURL, nil
	}
	return url, nil
}

// liveTarget is the launch-time check: it reads where the browser says it
// is listening and asks that endpoint to answer, but never removes the
// file. A browser that just started can publish the port a moment before it
// accepts, and deleting the file at that point would hide it for good.
// The note describes the last observation for the caller's error.
func (tb *teamBrowser) liveTarget(timeout time.Duration) (string, string) {
	portFile := filepath.Join(tb.profile, "DevToolsActivePort")
	data, err := os.ReadFile(portFile)
	if err != nil {
		return "", "debugging port file never appeared"
	}
	text := strings.TrimSpace(string(data))
	lines := strings.Split(text, "\n")
	if len(lines) < 2 {
		return "", fmt.Sprintf("debugging port file reads %q", text)
	}
	port := strings.TrimSpace(lines[0])
	client := http.Client{Timeout: timeout}
	resp, err := client.Get("http://127.0.0.1:" + port + "/json/version")
	if err != nil {
		return "", fmt.Sprintf("debugging port %s published but /json/version: %v", port, err)
	}
	defer resp.Body.Close()
	var info struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&info) != nil || info.WebSocketDebuggerURL == "" {
		return "", fmt.Sprintf("debugging port %s published but /json/version answered empty", port)
	}
	return info.WebSocketDebuggerURL, ""
}

// launch starts the browser and waits for its debugging port.
func (tb *teamBrowser) launch(ctx context.Context, installed Installed) (string, error) {
	portFile := filepath.Join(tb.profile, "DevToolsActivePort")
	_ = os.Remove(portFile)
	args := []string{
		"--headless=new", "--remote-debugging-port=0", "--user-data-dir=" + tb.profile,
		"--no-first-run", "--no-default-browser-check", "--disable-gpu", "--mute-audio",
		"--disable-background-timer-throttling", "--disable-renderer-backgrounding",
		"--window-size=1280,900", "about:blank",
	}
	if runtime.GOOS == "linux" {
		args = append(args, "--disable-dev-shm-usage")
		if os.Geteuid() == 0 || os.Getenv("COBOT_BROWSER_NO_SANDBOX") == "1" {
			args = append(args, "--no-sandbox")
		}
	}
	cmd := exec.Command(installed.Path, args...)
	// The browser's own complaint is the only clue when it will not start.
	stderr := &tailBuffer{max: 3000}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start %s: %w", installed.Name, err)
	}
	tb.cmd = cmd
	exited := make(chan struct{})
	var exitErr error
	tb.exited = exited
	go func() { exitErr = cmd.Wait(); close(exited) }()
	var exitedAt time.Time
	// A browser starting on a loaded machine (a CI runner running other
	// work) can take well over 20 seconds.
	deadline := time.Now().Add(45 * time.Second)
	note := ""
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			tb.killProcess()
			return "", ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
		if url, observed := tb.liveTarget(3 * time.Second); url != "" {
			return url, nil
		} else {
			note = observed
		}
		// Edge hands the real browser to a separate process and its launcher
		// exits cleanly, so an exit only counts once the port stays absent.
		select {
		case <-exited:
			if exitedAt.IsZero() {
				exitedAt = time.Now()
			}
			if exitErr != nil || time.Since(exitedAt) > 8*time.Second {
				return "", fmt.Errorf("%s exited right after starting%s", installed.Name, stderr.suffix())
			}
		default:
		}
	}
	tb.killProcess()
	if note == "" {
		note = "debugging port file never appeared"
	}
	return "", fmt.Errorf("%s did not open its debugging port in time (%s)%s", installed.Name, note, stderr.suffix())
}

// tailBuffer keeps the last bytes written to it.
type tailBuffer struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if len(b.buf) > b.max {
		b.buf = b.buf[len(b.buf)-b.max:]
	}
	return len(p), nil
}

// suffix is the tail as text to append to an error, or empty.
func (b *tailBuffer) suffix() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	text := strings.TrimSpace(string(b.buf))
	if text == "" {
		return ""
	}
	return " (" + text + ")"
}

func (tb *teamBrowser) killProcess() {
	if tb.cmd != nil && tb.cmd.Process != nil {
		_ = tb.cmd.Process.Kill()
	}
	tb.cmd = nil
}

// stop closes the browser, leaving the profile.
func (tb *teamBrowser) stop() {
	tb.startMu.Lock()
	defer tb.startMu.Unlock()
	tb.mu.Lock()
	conn := tb.conn
	tb.conn = nil
	tb.downloadDir = ""
	tb.mu.Unlock()
	if conn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = conn.call(ctx, "", "Browser.close", nil, nil)
		cancel()
		conn.close()
		// The launcher process may be long gone (Edge); wait for the browser
		// itself to stop answering so a restart does not reattach to it.
		for i := 0; i < 30; i++ {
			if url, _ := tb.reconnectable(); url == "" {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	if tb.cmd != nil && tb.cmd.Process != nil {
		select {
		case <-tb.exited:
		case <-time.After(2 * time.Second):
			_ = tb.cmd.Process.Kill()
		}
	}
	tb.cmd = nil
	tb.dropTabs()
	tb.mu.Lock()
	if tb.idleTimer != nil {
		tb.idleTimer.Stop()
	}
	tb.mu.Unlock()
}

// dropTabs forgets every tab, after the browser went away.
func (tb *teamBrowser) dropTabs() {
	tb.mu.Lock()
	tb.tabs = map[string]*tab{}
	tb.bySession = map[string]*tab{}
	tb.current = map[string]string{}
	tb.mu.Unlock()
}

// normalize validates a request and returns the steps to run, with the URL
// shorthand turned into a first goto.
func normalize(req Request) ([]Step, error) {
	steps := append([]Step(nil), req.Steps...)
	if strings.TrimSpace(req.URL) != "" {
		steps = append([]Step{{Action: "goto", URL: req.URL}}, steps...)
	}
	if len(steps) > maxSteps {
		return nil, fmt.Errorf("%w: at most %d steps per call", ErrBadRequest, maxSteps)
	}
	for i, step := range steps {
		if !knownActions[step.Action] {
			return nil, fmt.Errorf("%w: step %d has unknown action %q", ErrBadRequest, i+1, step.Action)
		}
	}
	return steps, nil
}
