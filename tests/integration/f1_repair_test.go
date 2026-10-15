package integration

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/platform"
)

func TestBootstrapRecoversFromStaleIdentity(t *testing.T) {
	core := startCore(t, platform.EnvDevelopment, filepath.Join(t.TempDir(), "profile"))
	c := newClient(t, core.Endpoint)
	_, status := c.do("GET", "/api/v1/status", nil, nil)
	sessionName := status["data"].(map[string]any)["sessionCookieName"].(string)
	c.cookies[sessionName] = "expired-from-previous-service"
	response, body := c.do("POST", "/api/v1/session/bootstrap", map[string]any{"requestId": "boot-recovery", "secret": core.StartupBootstrap}, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("fresh secret rejected by stale cookie: %d %v", response.StatusCode, body)
	}
	response, _ = c.do("POST", "/api/v1/session/bootstrap", map[string]any{"requestId": "boot-reused", "secret": core.StartupBootstrap}, nil)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatal("secret reuse accepted", response.StatusCode)
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

func TestBrowserReauthorizesAfterNormalCoreRestart(t *testing.T) {
	root := filepath.Join(t.TempDir(), "profile")
	core := startCore(t, platform.EnvDevelopment, root)
	c := newClient(t, core.Endpoint)
	authorize(t, core, c)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := core.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	restarted := startCore(t, platform.EnvDevelopment, root)
	c.base = restarted.Endpoint
	resp, _ := c.do("GET", "/api/v1/settings", nil, nil)
	if resp.StatusCode != 401 {
		t.Fatal("old session survived restart")
	}
	resp, body := c.do("POST", "/api/v1/session/bootstrap", map[string]any{"requestId": "restart-bootstrap", "secret": restarted.StartupBootstrap}, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("could not reauthorize after restart: %d %v", resp.StatusCode, body)
	}
	resp, _ = c.do("GET", "/api/v1/settings", nil, nil)
	if resp.StatusCode != 200 {
		t.Fatal("new session not usable")
	}
}
