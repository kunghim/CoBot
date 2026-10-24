package modelcatalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

const dataset = `{
 "zhipuai": {"models": {
   "glm-5": {"id":"glm-5","name":"GLM-5","reasoning":true,"reasoning_options":[{"type":"toggle"}],"modalities":{"input":["text"]},"limit":{"context":204800,"output":131072}},
   "glm-5.3": {"id":"glm-5.3","name":"GLM-5.3","reasoning":true,"reasoning_options":[{"type":"effort","values":["low","high","max"]}],"modalities":{"input":["text"]},"limit":{"context":1000000,"output":131072}}
 }},
 "deepseek": {"models": {
   "deepseek-v4-pro": {"id":"deepseek-v4-pro","name":"DeepSeek V4 Pro","reasoning":true,"reasoning_options":[{"type":"toggle"},{"type":"effort","values":["none","low","high","max"]}],"modalities":{"input":["text","image"]},"limit":{"context":1048576,"output":393216}},
   "plain": {"id":"plain","name":"Plain","reasoning":false,"modalities":{"input":["text"]},"limit":{"context":8000,"output":2000}},
   "silent": {"id":"silent","name":"Silent","modalities":{"input":["text"]},"limit":{"context":8000}}
 }},
 "zai": {"models": {
   "glm-5.3": {"id":"glm-5.3","name":"Same id, other vendor","limit":{"context":1}}
 }},
 "unrelated": {"models": {"x": {"id":"x"}}}
}`

func convert(t *testing.T) *Catalog {
	t.Helper()
	catalog, err := Convert([]byte(dataset), time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func TestConvertKeepsWhatCoBotUses(t *testing.T) {
	catalog := convert(t)
	glm, ok := catalog.Match("glm-5.3")
	if !ok || glm.Name != "GLM-5.3" || glm.Context != 1000000 || glm.Output != 131072 || glm.Vendor != "zhipuai" {
		t.Fatalf("glm-5.3 = %+v (%v)", glm, ok)
	}
	if !reflect.DeepEqual(glm.Thinking, &Thinking{Efforts: []string{"low", "high", "max"}}) {
		t.Fatalf("glm thinking = %+v", glm.Thinking)
	}
	if _, ok := catalog.Match("x"); ok {
		t.Fatal("a vendor that is not read leaked into the catalog")
	}
}

func TestAToggleIsAToggleAndNoneIsOff(t *testing.T) {
	catalog := convert(t)
	if e, _ := catalog.Match("glm-5"); !reflect.DeepEqual(e.Thinking, &Thinking{Toggle: true}) {
		t.Fatalf("toggle only = %+v", e.Thinking)
	}
	// "none" among the levels is the switch, not a level.
	if e, _ := catalog.Match("deepseek-v4-pro"); !reflect.DeepEqual(e.Thinking, &Thinking{Toggle: true, Efforts: []string{"low", "high", "max"}}) {
		t.Fatalf("toggle and levels = %+v", e.Thinking)
	}
	// A model that does not think has a thinking entry with nothing to control;
	// one the dataset is silent about has none at all.
	if e, _ := catalog.Match("plain"); e.Thinking == nil || e.Thinking.Toggle || len(e.Thinking.Efforts) != 0 {
		t.Fatalf("no thinking = %+v", e.Thinking)
	}
	if e, _ := catalog.Match("silent"); e.Thinking != nil {
		t.Fatalf("silent = %+v", e.Thinking)
	}
}

func TestTheMostAuthoritativeVendorWinsAnId(t *testing.T) {
	if e, _ := convert(t).Match("glm-5.3"); e.Vendor != "zhipuai" {
		t.Fatalf("vendor = %s, want zhipuai, which is listed before zai", e.Vendor)
	}
}

func TestAGatewaysOwnWordsInFrontOfAnIdStillMatch(t *testing.T) {
	catalog := convert(t)
	for id, want := range map[string]string{
		"GLM-5.3":          "glm-5.3",
		"q-glm-5.3":        "glm-5.3",
		"z-ai/glm-5.3":     "glm-5.3",
		"glm-5.3:free":     "glm-5.3",
		"vendor/q-glm-5":   "glm-5",
		"deepseek-v4-pro ": "deepseek-v4-pro",
	} {
		got, ok := catalog.Match(id)
		if !ok || got.ID != want {
			t.Errorf("%q matched %q (%v), want %q", id, got.ID, ok, want)
		}
	}
	// The longest id wins, and a word merely contained in an id is not a match.
	if got, _ := catalog.Match("q-glm-5.3"); got.ID != "glm-5.3" {
		t.Errorf("q-glm-5.3 taken for %q", got.ID)
	}
	for _, id := range []string{"glm-6", "xglm-5.3", "glm-5.3-thinking", ""} {
		if got, ok := catalog.Match(id); ok {
			t.Errorf("%q matched %q", id, got.ID)
		}
	}
}

func TestTheShippedCatalogIsReadable(t *testing.T) {
	store, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	status := store.Status()
	if status.Source != "bundled" || status.Models < 100 {
		t.Fatalf("status = %+v", status)
	}
	for _, id := range []string{"glm-5.3", "mimo-v2.6-flash", "deepseek-v4-pro", "qwen3.8-flash"} {
		if _, ok := store.Match(id); !ok {
			t.Errorf("the shipped catalog does not know %s", id)
		}
	}
}

func serve(t *testing.T, status int, body string) *Store {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	store, err := Open(filepath.Join(t.TempDir(), "catalog", "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	store.SetSourceURL(server.URL)
	return store
}

func TestUpdateReplacesTheCatalogAndKeepsItAcrossRestarts(t *testing.T) {
	store := serve(t, 200, dataset)
	before := store.Status()
	status, err := store.Update(context.Background(), time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if status.Source != "downloaded" || status.Models != 5 || status.Models == before.Models {
		t.Fatalf("status = %+v (was %+v)", status, before)
	}
	if _, ok := store.Match("glm-5.3"); !ok {
		t.Fatal("the downloaded catalog is not in use")
	}
	reopened, err := Open(store.file)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Status().Source != "downloaded" || reopened.Status().Models != 5 {
		t.Fatalf("after a restart: %+v", reopened.Status())
	}
}

func TestAFailedUpdateLeavesTheCatalogAsItWas(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		body   string
		kind   Failure
	}{
		"an error status": {500, "oops", Rejected},
		"not json":        {200, "<html>", Rejected},
		"no known model":  {200, `{"somebody":{"models":{}}}`, Rejected},
	} {
		store := serve(t, tc.status, tc.body)
		before := store.Status()
		_, err := store.Update(context.Background(), time.Now())
		if kind, ok := IsUpdateError(err); !ok || kind != tc.kind {
			t.Errorf("%s: error = %v, want %s", name, err, tc.kind)
		}
		if store.Status() != before {
			t.Errorf("%s: the catalog changed", name)
		}
		if _, statErr := os.Stat(store.file); statErr == nil {
			t.Errorf("%s: a file was written", name)
		}
	}
	// A server that is not there is "unreachable".
	store := serve(t, 200, dataset)
	store.SetSourceURL("http://127.0.0.1:1/api.json")
	if _, err := store.Update(context.Background(), time.Now()); err == nil {
		t.Fatal("update from nowhere succeeded")
	} else if kind, _ := IsUpdateError(err); kind != Unreachable {
		t.Fatalf("kind = %s", kind)
	}
}
