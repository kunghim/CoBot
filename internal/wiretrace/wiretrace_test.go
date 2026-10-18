package wiretrace

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func readEntries(t *testing.T, dir string) []Entry {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	var entries []Entry
	for _, file := range files {
		f, err := os.Open(file)
		if err != nil {
			t.Fatal(err)
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
		for scanner.Scan() {
			var entry Entry
			if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
				t.Fatal(err)
			}
			entries = append(entries, entry)
		}
		f.Close()
	}
	return entries
}

func TestRecordsOnlyWhileOnAndKeepsBytesExact(t *testing.T) {
	root := t.TempDir()
	rec := New(root, "core")
	rec.Record("codex.stdio", "out", "", []byte("before"))
	if _, err := os.Stat(TracesDir(root)); !os.IsNotExist(err) {
		t.Fatal("nothing may be written while recording is off")
	}

	session, err := Start(root, time.Date(2026, 10, 17, 22, 0, 0, 0, time.Local))
	if err != nil {
		t.Fatal(err)
	}
	rec.checkedAt = time.Time{} // skip the one-second recheck
	rec.Record("codex.stdio", "cobot→codex", "codex#1", []byte(`{"id":1,"method":"turn/start","params":{"text":"你好\n"}}`))
	rec.Record("codex.stderr", "codex→cobot", "codex#1", []byte{0xff, 0xfe, 'x'})
	if _, err := Stop(root); err != nil {
		t.Fatal(err)
	}
	rec.checkedAt = time.Time{}
	rec.Record("codex.stdio", "out", "", []byte("after"))
	rec.Close()

	entries := readEntries(t, session.Dir(root))
	if len(entries) != 2 {
		t.Fatalf("entries = %+v", entries)
	}
	if entries[0].Raw != `{"id":1,"method":"turn/start","params":{"text":"你好\n"}}` || entries[0].Dir != "cobot→codex" || entries[0].Peer != "codex#1" || entries[0].Seq != 1 {
		t.Fatalf("first entry = %+v", entries[0])
	}
	if raw, _ := base64.StdEncoding.DecodeString(entries[1].RawBase64); string(raw) != "\xff\xfex" || entries[1].Raw != "" {
		t.Fatalf("binary entry = %+v", entries[1])
	}
}

func TestLinesSplitStreamsWithoutChangingThem(t *testing.T) {
	root := t.TempDir()
	session, _ := Start(root, time.Now())
	rec := New(root, "mcp")
	lines := NewLines(rec, "mcp.stdio", "codex→bridge", "")
	source := "{\"a\":1}\r\n{\"b\":2}\npartial"
	passed, err := io.ReadAll(TeeReader(strings.NewReader(source), lines))
	if err != nil || string(passed) != source {
		t.Fatalf("stream changed: %q %v", passed, err)
	}
	lines.Flush()
	rec.Close()
	var raws []string
	for _, entry := range readEntries(t, session.Dir(root)) {
		raws = append(raws, entry.Raw)
	}
	if strings.Join(raws, "|") != `{"a":1}|{"b":2}|partial` {
		t.Fatalf("lines = %q", raws)
	}
}

func TestMiddlewareRecordsRequestsResponsesAndStreams(t *testing.T) {
	root := t.TempDir()
	session, _ := Start(root, time.Now())
	rec := New(root, "core")
	handler := Middleware(rec, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path == "/stream" {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("data: one\n\n"))
			w.(http.Flusher).Flush()
			_, _ = w.Write([]byte("data: two\n\n"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(append([]byte(`{"echo":`), append(body, '}')...))
	}))
	server := httptest.NewServer(handler)
	defer server.Close()

	resp, err := http.Post(server.URL+"/api", "application/json", strings.NewReader(`"hi"`))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(got) != `{"echo":"hi"}` {
		t.Fatalf("handler saw a changed body: %s", got)
	}
	resp, err = http.Get(server.URL + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	rec.Close()

	var dirs []string
	var all string
	for _, entry := range readEntries(t, session.Dir(root)) {
		dirs = append(dirs, entry.Dir)
		all += entry.Raw + "\n"
	}
	if strings.Join(dirs, ",") != "request,response,request,response-head,response-chunk,response-chunk,response-end" {
		t.Fatalf("dirs = %v", dirs)
	}
	for _, want := range []string{`POST /api HTTP/1.1`, `"hi"`, `{"echo":"hi"}`, "data: one", "data: two"} {
		if !strings.Contains(all, want) {
			t.Fatalf("recording lacks %q:\n%s", want, all)
		}
	}
}
