package integration

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
)

func TestWorkspaceWatchValidatesAllPathsBeforeStarting(t *testing.T) {
	h := newM2Harness(t)
	team, _ := h.createTeam(t, nil)
	if err := os.WriteFile(filepath.Join(team.Directory, "file.txt"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	h.app.WorkspaceWatchFactory = func(context.Context, string) (<-chan struct{}, error) { calls++; return make(chan struct{}), nil }
	for _, paths := range [][]string{nil, {"", "../outside"}, {"file.txt"}, make([]string, 65)} {
		_, _, err := h.app.WatchWorkspace(context.Background(), h.user, "team", team.ID, paths)
		if err == nil {
			t.Fatalf("invalid paths accepted: %v", paths)
		}
	}
	_, _, err := h.app.WatchWorkspace(context.Background(), auth.NewPrincipalBot("bot", team.ID), "team", team.ID, []string{""})
	if domain.AsError(err).Code != domain.CodeForbidden {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("native subscriptions started before authorization completed")
	}
	changed, stop, err := h.app.WatchWorkspace(context.Background(), h.user, "team", team.ID, []string{"", ".", team.Directory})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("canonical directory aliases not deduplicated: %d", calls)
	}
	stop()
	select {
	case <-changed:
	case <-time.After(time.Second):
		t.Fatal("subscription not closed")
	}
}

func TestWorkspaceWatchPartialFailureReleasesEarlierSubscriptions(t *testing.T) {
	h := newM2Harness(t)
	team, _ := h.createTeam(t, nil)
	if err := os.Mkdir(filepath.Join(team.Directory, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	calls := 0
	h.app.WorkspaceWatchFactory = func(ctx context.Context, _ string) (<-chan struct{}, error) {
		calls++
		if calls == 2 {
			return nil, errors.New("unsupported")
		}
		go func() { <-ctx.Done(); close(released) }()
		return make(chan struct{}), nil
	}
	_, _, err := h.app.WatchWorkspace(context.Background(), h.user, "team", team.ID, []string{"", "nested"})
	if domain.AsError(err).Code != domain.CodeCapabilityMissing {
		t.Fatal(err)
	}
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("partial subscriptions leaked")
	}
}

func TestWorkspaceWatchHTTPStreamsHintsAndCancelsOnDisconnect(t *testing.T) {
	h := newM2Harness(t)
	team, _ := h.createTeam(t, nil)
	expected, err := filepath.EvalSymlinks(team.Directory)
	if err != nil {
		t.Fatal(err)
	}
	events := make(chan struct{}, 256)
	released := make(chan struct{})
	h.app.WorkspaceWatchFactory = func(ctx context.Context, path string) (<-chan struct{}, error) {
		if path != expected {
			t.Errorf("unvalidated path: %q", path)
		}
		go func() { <-ctx.Done(); close(released) }()
		return events, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	body, _ := json.Marshal(map[string]any{"requestId": "workspace-watch-http", "paths": []string{""}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.api.URL+"/api/v1/workspaces/team/"+team.ID+"/watch", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+h.bearer)
	req.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 || response.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatal(response.Status)
	}
	reader := bufio.NewReader(response.Body)
	read := func() string {
		var frame string
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			frame += line
			if line == "\n" {
				return frame
			}
		}
	}
	if frame := read(); frame != "event: ready\ndata: {}\n\n" {
		t.Fatal(frame)
	}
	for range 100 {
		events <- struct{}{}
	}
	if frame := read(); frame != "event: changed\ndata: {}\n\n" {
		t.Fatal(frame)
	}
	cancel()
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("HTTP disconnect retained native subscriptions")
	}
}
