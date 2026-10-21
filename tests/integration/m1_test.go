// Package integration drives the real core (SQLite + HTTP) with temporary
// data roots. Nothing here touches the production profile or any account.
package integration

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/bootstrap"
	"github.com/kunghim/CoBot/internal/platform"
)

// client is a tiny cookie-aware HTTP client. It manages cookies manually so
// the tests do not depend on net/http/cookiejar, which is missing from this
// machine's Go distribution.
type client struct {
	t           *testing.T
	base        string
	http        *http.Client
	cookies     map[string]string
	csrf        string
	sessionName string
}

func newClient(t *testing.T, base string) *client {
	return &client{t: t, base: base, cookies: map[string]string{}, http: &http.Client{Timeout: 10 * time.Second}}
}

func (c *client) applyCookies(resp *http.Response) {
	for _, ck := range resp.Cookies() {
		if ck.Value == "" || ck.MaxAge < 0 {
			delete(c.cookies, ck.Name)
			continue
		}
		c.cookies[ck.Name] = ck.Value
		if strings.HasPrefix(ck.Name, "cobot_session_") {
			c.sessionName = ck.Name
		}
		if strings.HasPrefix(ck.Name, "cobot_csrf_") {
			c.csrf = ck.Value
		}
	}
}

func (c *client) cookieHeader() string {
	parts := make([]string, 0, len(c.cookies))
	for name, value := range c.cookies {
		parts = append(parts, name+"="+value)
	}
	return strings.Join(parts, "; ")
}

func (c *client) do(method, path string, body any, headers map[string]string) (*http.Response, map[string]any) {
	c.t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, c.base+path, reader)
	if err != nil {
		c.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if header := c.cookieHeader(); header != "" {
		req.Header.Set("Cookie", header)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	c.applyCookies(resp)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var parsed map[string]any
	_ = json.Unmarshal(raw, &parsed)
	return resp, parsed
}

func (c *client) patchSettings(patch map[string]any, expectedRevision int64) (*http.Response, map[string]any) {
	c.t.Helper()
	return c.do("PATCH", "/api/v1/settings", map[string]any{
		"requestId":        fmt.Sprintf("req-%d", time.Now().UnixNano()),
		"expectedRevision": expectedRevision,
		"patch":            patch,
	}, map[string]string{"X-CSRF-Token": c.csrf})
}

func startCore(t *testing.T, env platform.Environment, root string) *bootstrap.Core {
	t.Helper()
	core, err := bootstrap.Start(context.Background(), bootstrap.Options{
		Environment: env,
		DataRoot:    root,
		Listen:      "127.0.0.1:0",
		Version:     "test",
	})
	if err != nil {
		t.Fatalf("start core: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = core.Shutdown(ctx)
	})
	return core
}

func authorize(t *testing.T, core *bootstrap.Core, c *client) {
	t.Helper()
	resp, body := c.do("POST", "/api/v1/session/bootstrap", map[string]any{
		"requestId": "boot-1",
		"secret":    core.StartupBootstrap,
	}, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("bootstrap exchange failed: %d %v", resp.StatusCode, body)
	}
	data := body["data"].(map[string]any)
	c.sessionName = data["sessionCookieName"].(string)
	c.csrf = c.cookies[data["csrfCookieName"].(string)]
	if c.csrf == "" {
		t.Fatal("csrf cookie missing after bootstrap")
	}
}

// Committed settings survive restart; a new profile starts clean.
func TestSettingsPersistAcrossRestartAndProfilesAreIsolated(t *testing.T) {
	root := filepath.Join(t.TempDir(), "dev-profile")
	core := startCore(t, platform.EnvDevelopment, root)
	c := newClient(t, core.Endpoint)
	authorize(t, core, c)

	if resp, body := c.patchSettings(map[string]any{"language": "en", "memberLimit": 8}, 1); resp.StatusCode != 200 {
		t.Fatalf("patch: %d %v", resp.StatusCode, body)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := core.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	// Clean shutdown removes its own runtime artifacts only.
	if _, err := os.Stat(filepath.Join(root, "runtime", "instance.lock")); !os.IsNotExist(err) {
		t.Fatal("clean shutdown must remove the instance lock")
	}

	core2 := startCore(t, platform.EnvDevelopment, root)
	c2 := newClient(t, core2.Endpoint)
	authorize(t, core2, c2)
	resp, body := c2.do("GET", "/api/v1/settings", nil, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("get settings: %d", resp.StatusCode)
	}
	data := body["data"].(map[string]any)
	if data["language"] != "en" || data["memberLimit"] != float64(8) || data["revision"] != float64(2) {
		t.Fatalf("settings not persisted: %v", data)
	}

	// A production profile on its own root must not see development data.
	prodRoot := filepath.Join(t.TempDir(), "prod-profile")
	prod := startCore(t, platform.EnvProduction, prodRoot)
	pc := newClient(t, prod.Endpoint)
	authorize(t, prod, pc)
	resp, body = pc.do("GET", "/api/v1/settings", nil, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("prod settings: %d", resp.StatusCode)
	}
	pdata := body["data"].(map[string]any)
	if pdata["language"] != "zh-CN" || pdata["revision"] != float64(1) {
		t.Fatalf("production profile leaked development settings: %v", pdata)
	}
}

// A second core on the same profile refuses to start and reports the
// running instance instead of double-owning the database.
func TestSecondStartRefusesSameProfile(t *testing.T) {
	root := filepath.Join(t.TempDir(), "profile")
	core := startCore(t, platform.EnvDevelopment, root)

	_, err := bootstrap.Start(context.Background(), bootstrap.Options{
		Environment: platform.EnvDevelopment,
		DataRoot:    root,
		Listen:      "127.0.0.1:0",
	})
	var already *platform.ErrAlreadyRunning
	if !errors.As(err, &already) {
		t.Fatalf("want ErrAlreadyRunning, got %v", err)
	}
	if already.Endpoint != core.Endpoint {
		t.Fatalf("reported endpoint %q, want %q", already.Endpoint, core.Endpoint)
	}
}

// Bootstrap secrets are single-use and short-lived; forged origins,
// missing CSRF and unauthenticated access are rejected.
func TestAuthGuards(t *testing.T) {
	root := filepath.Join(t.TempDir(), "profile")
	core := startCore(t, platform.EnvDevelopment, root)
	c := newClient(t, core.Endpoint)

	// Unauthenticated settings access is 401.
	if resp, _ := c.do("GET", "/api/v1/settings", nil, nil); resp.StatusCode != 401 {
		t.Fatalf("unauth settings = %d, want 401", resp.StatusCode)
	}

	// Forged Origin is refused even with a valid secret.
	if resp, _ := c.do("POST", "/api/v1/session/bootstrap", map[string]any{
		"requestId": "forged", "secret": core.StartupBootstrap,
	}, map[string]string{"Origin": "http://evil.example"}); resp.StatusCode != 403 {
		t.Fatalf("forged origin = %d, want 403", resp.StatusCode)
	}
	// The secret was NOT consumed by the forged attempt: legit exchange works.
	authorize(t, core, c)

	// Reuse of the redeemed secret fails (with CSRF present so the guard
	// under test is the issuer, not the CSRF middleware).
	if resp, _ := c.do("POST", "/api/v1/session/bootstrap", map[string]any{
		"requestId": "reuse", "secret": core.StartupBootstrap,
	}, map[string]string{"X-CSRF-Token": c.csrf}); resp.StatusCode != 401 {
		t.Fatalf("secret reuse = %d, want 401", resp.StatusCode)
	}

	// PATCH without CSRF header is refused despite a valid session cookie.
	if resp, _ := c.patchSettingsWithoutCSRF(map[string]any{"language": "en"}); resp.StatusCode != 403 {
		t.Fatalf("missing csrf = %d, want 403", resp.StatusCode)
	}

	// Expired session cookie is a clean 401 (re-auth entry), not anonymous.
	c2 := newClient(t, core.Endpoint)
	secret, err := core.Issuer.Issue()
	if err != nil {
		t.Fatal(err)
	}
	resp, _ := c2.do("POST", "/api/v1/session/bootstrap", map[string]any{"requestId": "b2", "secret": secret}, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("second bootstrap: %d", resp.StatusCode)
	}
	core.Sessions.Delete(c2.cookies[c2.sessionName])
	if resp, _ := c2.do("GET", "/api/v1/settings", nil, nil); resp.StatusCode != 401 {
		t.Fatalf("revoked session = %d, want 401", resp.StatusCode)
	}
}

func (c *client) patchSettingsWithoutCSRF(patch map[string]any) (*http.Response, map[string]any) {
	return c.do("PATCH", "/api/v1/settings", map[string]any{
		"requestId": fmt.Sprintf("req-%d", time.Now().UnixNano()),
		"patch":     patch,
	}, nil)
}

// Stream mechanics: snapshot cursor plus SSE delivery of durable
// events after the cursor, without losing commits made while streaming.
func TestSnapshotAndEventStream(t *testing.T) {
	root := filepath.Join(t.TempDir(), "profile")
	core := startCore(t, platform.EnvDevelopment, root)
	c := newClient(t, core.Endpoint)
	authorize(t, core, c)

	resp, body := c.do("GET", "/api/v1/snapshot", nil, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("snapshot: %d", resp.StatusCode)
	}
	snapshot := body["data"].(map[string]any)
	cursor := int64(snapshot["seq"].(float64))

	// Open the stream in the background.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/api/v1/events?after=%d", core.Endpoint, cursor), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Cookie", c.sessionName+"="+c.cookies[c.sessionName])
	streamResp, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer streamResp.Body.Close()
	if streamResp.StatusCode != 200 {
		t.Fatalf("events stream status %d", streamResp.StatusCode)
	}

	got := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(streamResp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, "settings.updated") {
				got <- line
				return
			}
		}
	}()

	// Commit an event while the stream is open.
	if resp, body := c.patchSettings(map[string]any{"chainLimit": 9}, 1); resp.StatusCode != 200 {
		t.Fatalf("patch during stream: %d %v", resp.StatusCode, body)
	}

	select {
	case line := <-got:
		if !strings.Contains(line, `"seq":`) {
			t.Fatalf("unexpected event line: %s", line)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("stream did not deliver the durable settings event")
	}
}

// The internal link route only answers the same-user runtime credential.
func TestInternalLinkRequiresRuntimeSecret(t *testing.T) {
	root := filepath.Join(t.TempDir(), "profile")
	core := startCore(t, platform.EnvDevelopment, root)

	// Without the secret: 401.
	resp, _ := http.Post(core.Endpoint+"/internal/v1/session/link", "application/json", strings.NewReader(`{"requestId":"x"}`))
	if resp != nil {
		resp.Body.Close()
		if resp.StatusCode != 401 {
			t.Fatalf("anonymous internal link = %d, want 401", resp.StatusCode)
		}
	}

	req, _ := http.NewRequest("POST", core.Endpoint+"/internal/v1/session/link", strings.NewReader(`{"requestId":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Instance-Secret", core.AdminSecret)
	out, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Body.Close()
	if out.StatusCode != 200 {
		t.Fatalf("internal link with secret = %d", out.StatusCode)
	}
	var envelope struct {
		Data struct {
			BootstrapSecret string `json:"bootstrapSecret"`
		} `json:"data"`
	}
	if err := json.NewDecoder(out.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.BootstrapSecret == "" {
		t.Fatal("expected a minted bootstrap secret")
	}

	// The minted secret exchanges for a page session.
	c := newClient(t, core.Endpoint)
	if resp, body := c.do("POST", "/api/v1/session/bootstrap", map[string]any{
		"requestId": "minted", "secret": envelope.Data.BootstrapSecret,
	}, nil); resp.StatusCode != 200 {
		t.Fatalf("minted secret exchange = %d %v", resp.StatusCode, body)
	}
}
