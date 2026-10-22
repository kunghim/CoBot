package avatars

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// publisher serves a catalog the way the repository does.
type publisher struct {
	files    map[string]string
	requests atomic.Int32
}

func (p *publisher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.requests.Add(1)
	body, ok := p.files[strings.TrimPrefix(r.URL.Path, "/assets/")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	_, _ = w.Write([]byte(body))
}

func seriesFile(id string, version int) string {
	return fmt.Sprintf(`{"schemaVersion":1,"id":%q,"version":%d,"parts":{}}`, id, version)
}

// publish sets index.json and the series files from id -> version.
func (p *publisher) publish(series map[string]int) {
	p.files = map[string]string{}
	idx := index{SchemaVersion: 1}
	for id, version := range series {
		body := seriesFile(id, version)
		file := id + "/dist/" + id + ".json"
		p.files["avatars/"+file] = body
		sum := sha256.Sum256([]byte(body))
		idx.Series = append(idx.Series, IndexEntry{ID: id, Kind: "composed", Version: version, File: file, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(body))})
	}
	data, _ := json.Marshal(idx)
	p.files["avatars/index.json"] = string(data)
}

func newCatalog(t *testing.T, pub *publisher) (*Catalog, string) {
	t.Helper()
	server := httptest.NewServer(pub)
	t.Cleanup(server.Close)
	dir := filepath.Join(t.TempDir(), "assets", "avatars")
	catalog, err := New(dir, server.URL+"/assets/")
	if err != nil {
		t.Fatal(err)
	}
	return catalog, dir
}

func versions(t *testing.T, catalog *Catalog) map[string]int {
	t.Helper()
	loaded, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]int{}
	for _, series := range loaded {
		out[series.ID] = series.Version
	}
	return out
}

func TestSyncDownloadsAndKeepsCurrentVersion(t *testing.T) {
	pub := &publisher{}
	pub.publish(map[string]int{"dango": 1})
	catalog, _ := newCatalog(t, pub)
	ctx := context.Background()

	if got := versions(t, catalog); len(got) != 0 {
		t.Fatalf("empty cache loaded %v", got)
	}
	changed, err := catalog.Sync(ctx)
	if err != nil || !changed {
		t.Fatalf("first sync: changed=%v err=%v", changed, err)
	}
	if got := versions(t, catalog); got["dango"] != 1 {
		t.Fatalf("cached versions = %v", got)
	}

	// Same version published: only the index is read.
	before := pub.requests.Load()
	changed, err = catalog.Sync(ctx)
	if err != nil || changed {
		t.Fatalf("unchanged sync: changed=%v err=%v", changed, err)
	}
	if pub.requests.Load()-before != 1 {
		t.Fatalf("unchanged sync made %d requests, want 1", pub.requests.Load()-before)
	}
	if status := catalog.Status(); status.LastSyncAt.IsZero() || status.LastError != "" || status.Syncing {
		t.Fatalf("status = %+v", status)
	}
}

func TestSyncKeepsCacheWhenContentDoesNotMatch(t *testing.T) {
	pub := &publisher{}
	pub.publish(map[string]int{"dango": 1})
	catalog, _ := newCatalog(t, pub)
	ctx := context.Background()
	if _, err := catalog.Sync(ctx); err != nil {
		t.Fatal(err)
	}

	pub.publish(map[string]int{"dango": 2})
	pub.files["avatars/dango/dist/dango.json"] = strings.Replace(pub.files["avatars/dango/dist/dango.json"], `"parts"`, `"party"`, 1)
	changed, err := catalog.Sync(ctx)
	if err == nil || changed {
		t.Fatalf("tampered sync: changed=%v err=%v", changed, err)
	}
	if !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("error = %v", err)
	}
	if got := versions(t, catalog); got["dango"] != 1 {
		t.Fatalf("cache after failed sync = %v, want version 1 kept", got)
	}
	if catalog.Status().LastError == "" {
		t.Fatal("failed sync must be reported")
	}
}

func TestSyncSkipsNewerFormatAndUnknownKinds(t *testing.T) {
	pub := &publisher{}
	pub.publish(map[string]int{"dango": 1})
	catalog, _ := newCatalog(t, pub)
	ctx := context.Background()
	if _, err := catalog.Sync(ctx); err != nil {
		t.Fatal(err)
	}

	pub.files["avatars/index.json"] = `{"schemaVersion":2,"series":[]}`
	if _, err := catalog.Sync(ctx); err == nil {
		t.Fatal("a newer index format must not be read")
	}
	if got := versions(t, catalog); got["dango"] != 1 {
		t.Fatalf("cache after newer format = %v", got)
	}

	pub.publish(map[string]int{"dango": 1})
	var idx index
	_ = json.Unmarshal([]byte(pub.files["avatars/index.json"]), &idx)
	idx.Series = append(idx.Series, IndexEntry{ID: "sketches", Kind: "images", Version: 1, File: "sketches/index.json", SHA256: strings.Repeat("0", 64), Size: 10})
	data, _ := json.Marshal(idx)
	pub.files["avatars/index.json"] = string(data)
	_, err := catalog.Sync(ctx)
	if err == nil || !strings.Contains(err.Error(), `kind "images"`) {
		t.Fatalf("unknown kind: err = %v", err)
	}
	if got := versions(t, catalog); got["dango"] != 1 || len(got) != 1 {
		t.Fatalf("cache with unknown kind = %v", got)
	}
}

func TestSyncRejectsPathsOutsideTheCatalog(t *testing.T) {
	for _, file := range []string{"../secrets.json", "/etc/passwd", "a/../../b.json", `C:\x.json`} {
		entry := IndexEntry{ID: "dango", Kind: "composed", Version: 1, File: file, SHA256: strings.Repeat("a", 64), Size: 10}
		if err := checkEntry(entry); err == nil {
			t.Errorf("file %q was accepted", file)
		}
	}
	if err := checkEntry(IndexEntry{ID: "../x", Kind: "composed", Version: 1, File: "x.json", SHA256: strings.Repeat("a", 64), Size: 1}); err == nil {
		t.Error("series id with a path was accepted")
	}
}

func TestPruneKeepsTwoVersions(t *testing.T) {
	pub := &publisher{}
	catalog, dir := newCatalog(t, pub)
	ctx := context.Background()
	for version := 1; version <= 3; version++ {
		pub.publish(map[string]int{"dango": version})
		if _, err := catalog.Sync(ctx); err != nil {
			t.Fatal(err)
		}
	}
	files, _ := os.ReadDir(filepath.Join(dir, "dango"))
	var names []string
	for _, file := range files {
		names = append(names, file.Name())
	}
	if strings.Join(names, ",") != "2.json,3.json" {
		t.Fatalf("kept %v, want 2.json and 3.json", names)
	}
}

func TestNewRequiresHTTPSExceptLoopback(t *testing.T) {
	if _, err := New(t.TempDir(), "http://example.com/assets/"); err == nil {
		t.Fatal("plain http to a remote host must be refused")
	}
	for _, ok := range []string{"", "https://example.com/assets", "http://127.0.0.1:9/assets/", "http://localhost:9/"} {
		if _, err := New(t.TempDir(), ok); err != nil {
			t.Fatalf("%q refused: %v", ok, err)
		}
	}
}
