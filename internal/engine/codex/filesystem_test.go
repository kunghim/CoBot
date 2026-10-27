package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"
)

func filesystemServer(t *testing.T, reject bool) (*Server, <-chan map[string]any) {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	server := &Server{}
	server.client = NewClient(nopCloser{inW}, outR, ClientHooks{OnNotify: server.handleNotify})
	requests := make(chan map[string]any, 32)
	go func() {
		defer outW.Close()
		scanner := bufio.NewScanner(inR)
		for scanner.Scan() {
			var request map[string]any
			if json.Unmarshal(scanner.Bytes(), &request) != nil {
				return
			}
			requests <- request
			if reject && request["method"] == "fs/watch" {
				writeJSONLine(outW, map[string]any{"id": request["id"], "error": map[string]any{"code": -32601, "message": "method not found"}})
			} else {
				writeJSONLine(outW, map[string]any{"id": request["id"], "result": map[string]any{}})
			}
		}
	}()
	t.Cleanup(func() { inW.Close(); outW.Close(); server.client.Wait() })
	return server, requests
}

func watchRequest(t *testing.T, requests <-chan map[string]any, method string) map[string]any {
	t.Helper()
	select {
	case request := <-requests:
		if request["method"] != method {
			t.Fatalf("method: %v", request)
		}
		return request["params"].(map[string]any)
	case <-time.After(3 * time.Second):
		t.Fatal("missing request", method)
		return nil
	}
}

func TestFilesystemWatchesAreScopedCoalescedAndReleased(t *testing.T) {
	server, requests := filesystemServer(t, false)
	first, cancelFirst := context.WithCancel(context.Background())
	defer cancelFirst()
	second, cancelSecond := context.WithCancel(context.Background())
	defer cancelSecond()
	a, err := server.WatchDirectory(first, "/project/a")
	if err != nil {
		t.Fatal(err)
	}
	pa := watchRequest(t, requests, "fs/watch")
	b, err := server.WatchDirectory(second, "/project/b")
	if err != nil {
		t.Fatal(err)
	}
	pb := watchRequest(t, requests, "fs/watch")
	if pa["path"] != "/project/a" || pb["path"] != "/project/b" || pa["watchId"] == pb["watchId"] {
		t.Fatal(pa, pb)
	}
	notify := func(id any) {
		raw, _ := json.Marshal(map[string]any{"watchId": id, "changedPaths": []string{"/not-trusted-as-state"}})
		server.handleNotify("fs/changed", raw)
	}
	for range 100 {
		notify(pa["watchId"])
	}
	if len(a) != 1 || len(b) != 0 {
		t.Fatalf("unscoped or unbounded notifications: %d %d", len(a), len(b))
	}
	<-a
	cancelFirst()
	unwatch := watchRequest(t, requests, "fs/unwatch")
	if unwatch["watchId"] != pa["watchId"] {
		t.Fatal(unwatch)
	}
	if _, ok := <-a; ok {
		t.Fatal("canceled watcher still open")
	}
	notify(pa["watchId"])
	notify(pb["watchId"])
	select {
	case <-b:
	case <-time.After(time.Second):
		t.Fatal("other watcher was lost")
	}
	cancelSecond()
	watchRequest(t, requests, "fs/unwatch")
}

func TestFilesystemWatchRejectionAndCanceledCallsCleanUp(t *testing.T) {
	server, requests := filesystemServer(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := server.WatchDirectory(ctx, "/project"); err == nil {
		t.Fatal("unsupported method accepted")
	}
	watch := watchRequest(t, requests, "fs/watch")
	unwatch := watchRequest(t, requests, "fs/unwatch")
	if watch["watchId"] != unwatch["watchId"] {
		t.Fatal(watch, unwatch)
	}
	server.mu.Lock()
	remaining := len(server.watches)
	server.mu.Unlock()
	if remaining != 0 {
		t.Fatal("failed watcher retained")
	}
	canceled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := server.WatchDirectory(canceled, "/project"); err != context.Canceled {
		t.Fatal(err)
	}
}

func TestFilesystemWatchClosesWithTransport(t *testing.T) {
	server, requests := filesystemServer(t, false)
	changed, err := server.WatchDirectory(context.Background(), "/project")
	if err != nil {
		t.Fatal(err)
	}
	watchRequest(t, requests, "fs/watch")
	server.client.fail(io.EOF)
	select {
	case _, ok := <-changed:
		if ok {
			t.Fatal("expected close")
		}
	case <-time.After(time.Second):
		t.Fatal("watch retained after transport exit")
	}
}

func TestFilesystemWatchCancellationReleasesAnUncertainRegistration(t *testing.T) {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	server := &Server{client: NewClient(nopCloser{inW}, outR, ClientHooks{})}
	requests := make(chan map[string]any, 4)
	lateResponse := make(chan struct{})
	go func() {
		defer outW.Close()
		scanner := bufio.NewScanner(inR)
		for scanner.Scan() {
			var request map[string]any
			if json.Unmarshal(scanner.Bytes(), &request) != nil {
				return
			}
			requests <- request
			if request["method"] == "fs/watch" {
				<-lateResponse
			}
			writeJSONLine(outW, map[string]any{"id": request["id"], "result": map[string]any{}})
		}
	}()
	t.Cleanup(func() {
		select {
		case <-lateResponse:
		default:
			close(lateResponse)
		}
		inW.Close()
		outW.Close()
		server.client.Wait()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := server.WatchDirectory(ctx, "/project"); result <- err }()
	watch := watchRequest(t, requests, "fs/watch")
	server.mu.Lock()
	released := server.watches[watch["watchId"].(string)]
	server.mu.Unlock()
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("registration ignored cancellation")
	}
	close(lateResponse)
	select {
	case request := <-requests:
		if request["method"] != "fs/unwatch" || request["params"].(map[string]any)["watchId"] != watch["watchId"] {
			t.Fatal(watch, request)
		}
	case <-server.client.Closed():
		// Cancellation can win before writeContext confirms the complete
		// frame. That transport is torn down rather than reused.
	case <-time.After(3 * time.Second):
		t.Fatal("uncertain registration was neither unwatched nor disconnected")
	}
	select {
	case _, ok := <-released:
		if ok {
			t.Fatal("expected released subscription")
		}
	case <-time.After(time.Second):
		t.Fatal("subscription retained after cancellation")
	}
}
