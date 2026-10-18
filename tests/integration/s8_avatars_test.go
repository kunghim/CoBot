package integration

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/platform"
)

// startCoreWithAssets starts a core whose config.json points the avatar
// catalog at base. Tests never reach the published catalog.
func startCoreWithAssets(t *testing.T, base string) *client {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "config.json"), []byte(fmt.Sprintf(`{"assetsBaseUrl":%q}`, base)), 0o600); err != nil {
		t.Fatal(err)
	}
	core := startCore(t, platform.EnvDevelopment, root)
	c := newClient(t, core.Endpoint)
	authorize(t, core, c)
	return c
}

func syncCatalog(c *client) (*http.Response, map[string]any) {
	return c.do("POST", "/api/v1/avatars/catalog/sync", map[string]any{"requestId": fmt.Sprintf("sync-%d", time.Now().UnixNano())}, map[string]string{"X-CSRF-Token": c.csrf})
}

// servePublished serves the repository's assets as GitHub does: with LF line
// endings, whatever the local checkout uses.
func servePublished() *httptest.Server {
	root := filepath.Join("..", "..")
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(path.Clean(r.URL.Path), "/"))))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")))
	}))
}

// The catalog the repository publishes downloads, verifies and is served.
func TestAvatarCatalogSyncsFromThePublishedFiles(t *testing.T) {
	published := servePublished()
	defer published.Close()
	c := startCoreWithAssets(t, published.URL+"/assets/")

	resp, body := c.do("GET", "/api/v1/avatars/catalog", nil, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("catalog: %d %v", resp.StatusCode, body)
	}
	if series := body["data"].(map[string]any)["series"].([]any); len(series) != 0 {
		t.Fatalf("a fresh profile has a catalog before any sync: %v", series)
	}

	resp, body = syncCatalog(c)
	if resp.StatusCode != 200 {
		t.Fatalf("sync: %d %v", resp.StatusCode, body)
	}
	data := body["data"].(map[string]any)
	if status := data["status"].(map[string]any); status["lastError"] != nil {
		t.Fatalf("sync failed: %v", status["lastError"])
	}
	series := data["series"].([]any)
	if len(series) != 1 {
		t.Fatalf("series = %v", series)
	}
	dango := series[0].(map[string]any)
	if dango["id"] != "dango" || dango["kind"] != "composed" {
		t.Fatalf("series = %v", dango)
	}
	defaults := dango["data"].(map[string]any)["roleDefaults"].(map[string]any)
	if leader := defaults["leader"].(map[string]any); leader["shape"] != "star" {
		t.Fatalf("leader default = %v", leader)
	}

	// The download is kept: a later read needs no network.
	published.Close()
	resp, body = c.do("GET", "/api/v1/avatars/catalog", nil, nil)
	if resp.StatusCode != 200 || len(body["data"].(map[string]any)["series"].([]any)) != 1 {
		t.Fatalf("cached catalog: %d %v", resp.StatusCode, body)
	}
}

// An unreachable catalog is a reported status, never a failed request.
func TestAvatarCatalogReportsAFailedSync(t *testing.T) {
	missing := httptest.NewServer(http.NotFoundHandler())
	defer missing.Close()
	c := startCoreWithAssets(t, missing.URL+"/assets/")

	resp, body := syncCatalog(c)
	if resp.StatusCode != 200 {
		t.Fatalf("sync: %d %v", resp.StatusCode, body)
	}
	data := body["data"].(map[string]any)
	if len(data["series"].([]any)) != 0 {
		t.Fatalf("series after a failed sync = %v", data["series"])
	}
	if status := data["status"].(map[string]any); status["lastError"] == nil || status["lastSyncAt"] == nil {
		t.Fatalf("status = %v", status)
	}
}

// A member's own avatar and a role's avatar are stored as part choices and
// come back with the member and the role library.
func TestMemberAndRoleAvatarsAreStored(t *testing.T) {
	missing := httptest.NewServer(http.NotFoundHandler())
	defer missing.Close()
	c := startCoreWithAssets(t, missing.URL+"/assets/")
	csrf := map[string]string{"X-CSRF-Token": c.csrf}
	request := func() string { return fmt.Sprintf("req-%d", time.Now().UnixNano()) }

	resp, body := c.do("POST", "/api/v1/teams", map[string]any{
		"requestId": request(), "name": "头像", "directory": t.TempDir(), "initialRoleIds": []string{"frontend-dev"},
	}, csrf)
	if resp.StatusCode != 201 {
		t.Fatalf("create team: %d %v", resp.StatusCode, body)
	}
	var member map[string]any
	for _, b := range body["data"].(map[string]any)["bots"].([]any) {
		if bot := b.(map[string]any); bot["isLeader"] != true {
			member = bot
		}
	}
	if _, has := member["avatar"]; has {
		t.Fatalf("a new member has an avatar of its own: %v", member["avatar"])
	}
	botPath := "/api/v1/bots/" + member["id"].(string)
	avatar := map[string]any{"kind": "catalog", "series": "dango", "shape": "flower", "eyes": "grin", "color": "#f98b8b"}

	resp, body = c.do("PATCH", botPath, map[string]any{"requestId": request(), "expectedRevision": member["revision"], "avatar": avatar}, csrf)
	if resp.StatusCode != 200 {
		t.Fatalf("set member avatar: %d %v", resp.StatusCode, body)
	}
	member = body["data"].(map[string]any)
	if got := member["avatar"].(map[string]any); got["shape"] != "flower" {
		t.Fatalf("member avatar = %v", got)
	}
	resp, body = c.do("PATCH", botPath, map[string]any{"requestId": request(), "expectedRevision": member["revision"], "avatar": map[string]any{"kind": "catalog", "series": "dango", "shape": "flower", "eyes": "grin", "color": "red"}}, csrf)
	if resp.StatusCode != 400 {
		t.Fatalf("invalid color accepted: %d %v", resp.StatusCode, body)
	}
	resp, body = c.do("PATCH", botPath, map[string]any{"requestId": request(), "expectedRevision": member["revision"], "avatar": nil}, csrf)
	if resp.StatusCode != 200 {
		t.Fatalf("clear member avatar: %d %v", resp.StatusCode, body)
	}
	if _, has := body["data"].(map[string]any)["avatar"]; has {
		t.Fatal("cleared member still has an avatar")
	}

	resp, body = c.do("PUT", "/api/v1/roles/frontend-dev/avatar", map[string]any{"requestId": request(), "avatar": avatar}, csrf)
	if resp.StatusCode != 200 {
		t.Fatalf("set role avatar: %d %v", resp.StatusCode, body)
	}
	if resp, _ := c.do("PUT", "/api/v1/roles/no-such-role/avatar", map[string]any{"requestId": request(), "avatar": avatar}, csrf); resp.StatusCode != 404 {
		t.Fatalf("unknown role: %d", resp.StatusCode)
	}
	roleAvatar := func() any {
		_, body := c.do("GET", "/api/v1/roles", nil, nil)
		for _, r := range body["data"].(map[string]any)["items"].([]any) {
			if role := r.(map[string]any); role["id"] == "frontend-dev" {
				return role["avatar"]
			}
		}
		t.Fatal("frontend-dev missing from the role library")
		return nil
	}
	if got, ok := roleAvatar().(map[string]any); !ok || got["eyes"] != "grin" {
		t.Fatalf("role avatar = %v", roleAvatar())
	}
	resp, body = c.do("PUT", "/api/v1/roles/frontend-dev/avatar", map[string]any{"requestId": request(), "avatar": nil}, csrf)
	if resp.StatusCode != 200 {
		t.Fatalf("clear role avatar: %d %v", resp.StatusCode, body)
	}
	if got := roleAvatar(); got != nil {
		t.Fatalf("cleared role avatar = %v", got)
	}
}
