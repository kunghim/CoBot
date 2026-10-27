package integration

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/bootstrap"
	"github.com/kunghim/CoBot/internal/platform"
)

func TestBootstrapRecoversFromStaleIdentity(t *testing.T) {
	core := startCore(t, platform.EnvDevelopment, filepath.Join(t.TempDir(), "profile"))
	c := newClient(t, core.Endpoint)
	_, status := c.do("GET", "/api/v1/status", nil, nil)
	sessionName := status["data"].(map[string]any)["sessionCookieName"].(string)
	c.cookies[sessionName] = "expired-from-previous-service"
	for _, path := range []string{"/", "/settings"} {
		response, _ := c.do("GET", path, nil, nil)
		if response.StatusCode != 200 || response.Header.Get("Content-Type") != "text/html; charset=utf-8" {
			t.Fatalf("stale cookie blocked public shell %s: %d", path, response.StatusCode)
		}
	}
	response, statusProjection := c.do("GET", "/api/v1/status", nil, nil)
	if response.StatusCode != 200 || statusProjection["data"].(map[string]any)["authorized"] != false {
		t.Fatal("stale identity must reach reauthorization entry")
	}
	response, _ = c.do("GET", "/api/v1/settings", nil, nil)
	if response.StatusCode != 401 {
		t.Fatal("public shell widened API access")
	}
	response, body := c.do("POST", "/api/v1/session/bootstrap", map[string]any{"requestId": "boot-recovery", "secret": core.StartupBootstrap}, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("fresh secret rejected by stale cookie: %d %v", response.StatusCode, body)
	}
	response, _ = c.do("POST", "/api/v1/session/bootstrap", map[string]any{"requestId": "boot-reused", "secret": core.StartupBootstrap}, nil)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatal("secret reuse accepted", response.StatusCode)
	}
}

// The page issues itself a session as it loads. This is what lets a
// bookmarked address keep working: the user opens the address and is in,
// with nothing to fetch from a terminal and nothing that expires.
func TestPageLoadIssuesItsOwnSession(t *testing.T) {
	core := startCore(t, platform.EnvDevelopment, filepath.Join(t.TempDir(), "profile"))
	c := newClient(t, core.Endpoint)

	response, _ := c.do("GET", "/", nil, map[string]string{"Accept": "text/html,application/xhtml+xml"})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("page load = %d", response.StatusCode)
	}
	if c.sessionName == "" || c.cookies[c.sessionName] == "" {
		t.Fatal("the page was not issued a session")
	}
	// The session it was given is a working one.
	response, _ = c.do("GET", "/api/v1/settings", nil, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("the session from the page load does not work: %d", response.StatusCode)
	}
}

// A reload keeps the session it already has. Minting another one on every
// load would fill the session file with sessions nobody uses.
func TestPageReloadKeepsItsSession(t *testing.T) {
	core := startCore(t, platform.EnvDevelopment, filepath.Join(t.TempDir(), "profile"))
	c := newClient(t, core.Endpoint)

	c.do("GET", "/", nil, map[string]string{"Accept": "text/html"})
	first := c.cookies[c.sessionName]
	if first == "" {
		t.Fatal("the page was not issued a session")
	}
	c.do("GET", "/", nil, map[string]string{"Accept": "text/html"})
	if c.cookies[c.sessionName] != first {
		t.Fatal("the page was issued a second session on reload")
	}
}

// Only a page opens a session. A program on the machine that asks for the
// API directly is still refused, the way it always was.
func TestApiWithoutACredentialStaysRefused(t *testing.T) {
	core := startCore(t, platform.EnvDevelopment, filepath.Join(t.TempDir(), "profile"))
	c := newClient(t, core.Endpoint)

	response, _ := c.do("GET", "/api/v1/settings", nil, nil)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("settings without a credential = %d, want 401", response.StatusCode)
	}
	if c.sessionName != "" {
		t.Fatal("an API request was issued a session")
	}
}

// Another site in the user's own browser is still turned away, and it never
// reaches the point where a session could be issued to it.
func TestAnotherSiteCannotOpenASession(t *testing.T) {
	core := startCore(t, platform.EnvDevelopment, filepath.Join(t.TempDir(), "profile"))
	c := newClient(t, core.Endpoint)

	response, _ := c.do("GET", "/", nil, map[string]string{
		"Accept": "text/html",
		"Origin": "https://evil.example",
	})
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site page load = %d, want 403", response.StatusCode)
	}
	if c.sessionName != "" {
		t.Fatal("a refused request was issued a session")
	}
}

// The desktop shell is served from another origin, where a SameSite=Strict
// cookie is never stored. The core must not report it as authorized on sight,
// or the shell skips redeeming its secret and every write fails CSRF. It
// takes the bearer path instead, which needs no CSRF token.
func TestDesktopShellAuthorizesWithBearerNotCookie(t *testing.T) {
	const shell = "http://wails.localhost"
	core, err := bootstrap.Start(context.Background(), bootstrap.Options{
		Environment:  platform.EnvDevelopment,
		DataRoot:     filepath.Join(t.TempDir(), "profile"),
		Listen:       "127.0.0.1:0",
		Version:      "test",
		ExtraOrigins: []string{shell},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = core.Shutdown(ctx)
	})
	c := newClient(t, core.Endpoint)
	origin := map[string]string{"Origin": shell}

	_, status := c.do("GET", "/api/v1/status", nil, origin)
	if status["data"].(map[string]any)["authorized"] != false {
		t.Fatal("a page on another origin was authorized without a credential")
	}
	if c.sessionName != "" {
		t.Fatal("a page on another origin was issued a cookie session")
	}
	resp, body := c.do("POST", "/api/v1/session/bootstrap", map[string]any{"requestId": "shell-boot", "secret": core.StartupBootstrap}, origin)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bootstrap: %d %v", resp.StatusCode, body)
	}
	token, _ := body["data"].(map[string]any)["bearerToken"].(string)
	if token == "" {
		t.Fatal("no bearer token issued to the shell")
	}
	// The shell holds no cookie: only the bearer token travels.
	c.cookies = map[string]string{}
	bearer := map[string]string{"Origin": shell, "Authorization": "Bearer " + token}
	resp, body = c.do("PATCH", "/api/v1/settings", map[string]any{"requestId": "shell-patch", "expectedRevision": 1, "patch": map[string]any{"memberLimit": 7}}, bearer)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bearer write: %d %v", resp.StatusCode, body)
	}
}

// A role avatar saved twice under one request id (a double click, a retry
// after a lost answer) answers the second time as it did the first, instead of
// failing on the record the first one left.
func TestRoleAvatarReplayAnswersLikeTheFirstRequest(t *testing.T) {
	core := startCore(t, platform.EnvDevelopment, filepath.Join(t.TempDir(), "profile"))
	c := newClient(t, core.Endpoint)
	authorize(t, core, c)
	body := map[string]any{
		"requestId": "avatar-replay",
		"avatar":    map[string]any{"kind": "catalog", "series": "dango", "shape": "square", "eyes": "shine", "color": "#7d8c84"},
	}
	for attempt := 1; attempt <= 2; attempt++ {
		resp, out := c.do("PUT", "/api/v1/roles/leader/avatar", body, map[string]string{"X-CSRF-Token": c.csrf})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("attempt %d: %d %v", attempt, resp.StatusCode, out)
		}
	}
}

// The desktop shell lives on another origin, so every write it makes is
// preceded by a preflight, and a method the preflight does not list is blocked
// by the browser before it is sent. PUT is used for role avatars, a provider's
// switch and a member's model.
func TestPreflightListsEveryMethodTheRoutesUse(t *testing.T) {
	const shell = "http://wails.localhost"
	core, err := bootstrap.Start(context.Background(), bootstrap.Options{
		Environment:  platform.EnvDevelopment,
		DataRoot:     filepath.Join(t.TempDir(), "profile"),
		Listen:       "127.0.0.1:0",
		Version:      "test",
		ExtraOrigins: []string{shell},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = core.Shutdown(ctx)
	})
	c := newClient(t, core.Endpoint)
	resp, _ := c.do("OPTIONS", "/api/v1/providers/x/enabled", nil, map[string]string{
		"Origin":                         shell,
		"Access-Control-Request-Method":  "PUT",
		"Access-Control-Request-Headers": "authorization,content-type",
	})
	allowed := resp.Header.Get("Access-Control-Allow-Methods")
	for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
		if !strings.Contains(allowed, method) {
			t.Errorf("preflight does not allow %s: %q", method, allowed)
		}
	}
}

func TestLocalStopDoesNotAcceptPageAuthority(t *testing.T) {
	core := startCore(t, platform.EnvDevelopment, filepath.Join(t.TempDir(), "profile"))
	c := newClient(t, core.Endpoint)
	authorize(t, core, c)
	resp, _ := c.do("POST", "/internal/v1/service/stop", map[string]any{"requestId": "stop-page"}, map[string]string{"X-CSRF-Token": c.csrf})
	if resp.StatusCode != 401 {
		t.Fatal("page may stop core", resp.StatusCode)
	}
	select {
	case <-core.StopRequested:
		t.Fatal("unauthorized stop took effect")
	default:
	}
	resp, _ = c.do("POST", "/internal/v1/service/stop", map[string]any{"requestId": "stop-own"}, map[string]string{"X-Instance-Secret": core.AdminSecret})
	if resp.StatusCode != 202 {
		t.Fatal("local stop rejected", resp.StatusCode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	select {
	case <-core.StopRequested:
	case <-ctx.Done():
		t.Fatal("local stop not delivered")
	}
}

func TestProfilesUseIndependentCookies(t *testing.T) {
	a := startCore(t, platform.EnvDevelopment, filepath.Join(t.TempDir(), "a"))
	b := startCore(t, platform.EnvDevelopment, filepath.Join(t.TempDir(), "b"))
	c := newClient(t, a.Endpoint)
	authorize(t, a, c)
	c.base = b.Endpoint
	authorize(t, b, c)
	for _, core := range []string{a.Endpoint, b.Endpoint} {
		c.base = core
		resp, body := c.do("GET", "/api/v1/status", nil, nil)
		if resp.StatusCode != 200 || body["data"].(map[string]any)["authorized"] != true {
			t.Fatalf("profile cookie overwritten: %d %v", resp.StatusCode, body)
		}
	}
}

// A browser stays signed in across a normal core restart; signing out still
// ends the session for good, restart or not.
func TestBrowserStaysSignedInAcrossCoreRestart(t *testing.T) {
	root := filepath.Join(t.TempDir(), "profile")
	core := startCore(t, platform.EnvDevelopment, root)
	c := newClient(t, core.Endpoint)
	authorize(t, core, c)
	restart := func() {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := core.Shutdown(ctx); err != nil {
			t.Fatal(err)
		}
		core = startCore(t, platform.EnvDevelopment, root)
		c.base = core.Endpoint
	}
	restart()
	resp, _ := c.do("GET", "/api/v1/settings", nil, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("session lost on restart: %d", resp.StatusCode)
	}
	resp, body := c.do("POST", "/api/v1/session/logout", map[string]any{"requestId": "restart-logout"}, map[string]string{"X-CSRF-Token": c.csrf})
	if resp.StatusCode != 200 {
		t.Fatalf("logout: %d %v", resp.StatusCode, body)
	}
	restart()
	resp, _ = c.do("GET", "/api/v1/settings", nil, nil)
	if resp.StatusCode != 401 {
		t.Fatal("signed-out session came back after restart")
	}
	resp, body = c.do("POST", "/api/v1/session/bootstrap", map[string]any{"requestId": "restart-bootstrap", "secret": core.StartupBootstrap}, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("could not reauthorize after restart: %d %v", resp.StatusCode, body)
	}
	resp, _ = c.do("GET", "/api/v1/settings", nil, nil)
	if resp.StatusCode != 200 {
		t.Fatal("new session not usable")
	}
}
