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
