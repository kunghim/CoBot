package integration

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
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

func pngOf(t *testing.T, side int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, side, side))
	for x := 0; x < side; x++ {
		img.Set(x, x, color.RGBA{R: 200, A: 255})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// A local picture is kept in the data root, served back only to the user,
// and can become a member's avatar; anything but a 256×256 PNG is refused.
func TestLocalPicturesBecomeAvatars(t *testing.T) {
	missing := httptest.NewServer(http.NotFoundHandler())
	defer missing.Close()
	c := startCoreWithAssets(t, missing.URL+"/assets/")
	csrf := map[string]string{"X-CSRF-Token": c.csrf}
	request := func() string { return fmt.Sprintf("req-%d", time.Now().UnixNano()) }
	upload := func(data []byte) (*http.Response, map[string]any) {
		return c.do("POST", "/api/v1/avatars/uploads", map[string]any{"requestId": request(), "data": base64.StdEncoding.EncodeToString(data)}, csrf)
	}

	if resp, body := upload(pngOf(t, 100)); resp.StatusCode != 400 {
		t.Fatalf("a 100px picture was accepted: %d %v", resp.StatusCode, body)
	}
	if resp, body := upload([]byte("GIF89a not a png")); resp.StatusCode != 400 {
		t.Fatalf("a non-PNG was accepted: %d %v", resp.StatusCode, body)
	}
	picture := pngOf(t, 256)
	resp, body := upload(picture)
	if resp.StatusCode != 201 {
		t.Fatalf("upload: %d %v", resp.StatusCode, body)
	}
	file := body["data"].(map[string]any)["file"].(string)

	req, _ := http.NewRequest("GET", c.base+"/api/v1/avatars/uploads/"+file, nil)
	req.Header.Set("Cookie", c.cookieHeader())
	got, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	served, _ := io.ReadAll(got.Body)
	got.Body.Close()
	if got.StatusCode != 200 || !bytes.Equal(served, picture) || got.Header.Get("Content-Type") != "image/png" || got.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("served %d %q %q, %d bytes", got.StatusCode, got.Header.Get("Content-Type"), got.Header.Get("X-Content-Type-Options"), len(served))
	}
	anonymous, _ := http.Get(c.base + "/api/v1/avatars/uploads/" + file)
	if anonymous.StatusCode != 401 {
		t.Fatalf("picture served without a session: %d", anonymous.StatusCode)
	}
	anonymous.Body.Close()

	resp, body = c.do("POST", "/api/v1/teams", map[string]any{
		"requestId": request(), "name": "图片", "directory": t.TempDir(), "initialRoleIds": []string{"frontend-dev"},
	}, csrf)
	if resp.StatusCode != 201 {
		t.Fatalf("create team: %d %v", resp.StatusCode, body)
	}
	leader := body["data"].(map[string]any)["bots"].([]any)[0].(map[string]any)
	path := "/api/v1/bots/" + leader["id"].(string)
	gone := map[string]any{"kind": "upload", "file": strings.Repeat("a", 64) + ".png"}
	if resp, body := c.do("PATCH", path, map[string]any{"requestId": request(), "expectedRevision": leader["revision"], "avatar": gone}, csrf); resp.StatusCode != 400 {
		t.Fatalf("a picture that is not stored was accepted: %d %v", resp.StatusCode, body)
	}
	resp, body = c.do("PATCH", path, map[string]any{"requestId": request(), "expectedRevision": leader["revision"], "avatar": map[string]any{"kind": "upload", "file": file}}, csrf)
	if resp.StatusCode != 200 {
		t.Fatalf("set picture avatar: %d %v", resp.StatusCode, body)
	}
	if avatar := body["data"].(map[string]any)["avatar"].(map[string]any); avatar["file"] != file {
		t.Fatalf("avatar = %v", avatar)
	}
}

// Pictures nothing uses are removed as the core starts; recent ones stay,
// since a choice is saved right after its upload.
func TestUnusedPicturesAreCleanedAtStart(t *testing.T) {
	root := t.TempDir()
	uploads := filepath.Join(root, "avatars", "uploads")
	if err := os.MkdirAll(uploads, 0o700); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	unused := filepath.Join(uploads, strings.Repeat("b", 64)+".png")
	leftover := filepath.Join(uploads, ".upload-123")
	recent := filepath.Join(uploads, strings.Repeat("c", 64)+".png")
	for _, path := range []string{unused, leftover, recent} {
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.Chtimes(unused, old, old)
	_ = os.Chtimes(leftover, old, old)

	startCore(t, platform.EnvDevelopment, root)
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, errUnused := os.Stat(unused)
		_, errLeftover := os.Stat(leftover)
		if os.IsNotExist(errUnused) && os.IsNotExist(errLeftover) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("unused pictures were not cleaned")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := os.Stat(recent); err != nil {
		t.Fatalf("a recent picture was removed: %v", err)
	}
}
