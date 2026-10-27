package integration

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/engine/codex"
	"github.com/kunghim/CoBot/internal/platform"
)

func TestRealWorkspaceWatchHTTPControl(t *testing.T) {
	if os.Getenv("COBOT_R_FILESYSTEM") != "1" {
		t.Skip("set COBOT_R_FILESYSTEM=1 and an isolated COBOT_R_FILESYSTEM_ROOT")
	}
	root := os.Getenv("COBOT_R_FILESYSTEM_ROOT")
	if !filepath.IsAbs(root) {
		t.Fatal("COBOT_R_FILESYSTEM_ROOT must be an explicit absolute test data root")
	}
	temp := filepath.Join(root, "http-temp")
	if err := os.MkdirAll(temp, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", temp)
	t.Setenv("TMP", temp)
	t.Setenv("TEMP", temp)
	binary, err := codex.Locate(os.Getenv("COBOT_R_FILESYSTEM_BINARY"))
	if err != nil {
		t.Fatal(err)
	}
	h := newM2Harness(t)
	team, _ := h.createTeam(t, nil)
	starts := 0
	h.app.ControlFactory = func(ctx context.Context, cwd string) (*codex.Server, error) {
		starts++
		return codex.Start(ctx, codex.ServerOptions{Binary: binary, Cwd: cwd, Env: codex.EngineEnv(h.app.Paths, nil), Client: codex.ClientInfo{Name: "cobot-filesystem-http", Version: "test"}})
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := h.app.ShutdownControl(ctx); err != nil {
			t.Error(err)
		}
	})
	base := h.api.URL + "/api/v1/workspaces/team/" + team.ID
	for _, name := range []string{"first.txt", "second.txt"} {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/watch", strings.NewReader(`{"requestId":"real-workspace-watch","paths":[""]}`))
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+h.bearer)
		req.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			cancel()
			t.Fatal(response.Status)
		}
		reader := bufio.NewReader(response.Body)
		readFrame := func() string {
			var frame string
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					response.Body.Close()
					cancel()
					t.Fatal(err)
				}
				frame += line
				if line == "\n" {
					return frame
				}
			}
		}
		if frame := readFrame(); frame != "event: ready\ndata: {}\n\n" {
			t.Fatal(frame)
		}
		if err := os.WriteFile(filepath.Join(team.Directory, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
		if frame := readFrame(); frame != "event: changed\ndata: {}\n\n" {
			t.Fatal(frame)
		}
		response.Body.Close()
		cancel()
		get, err := http.NewRequest(http.MethodGet, base+"/content?path="+name, nil)
		if err != nil {
			t.Fatal(err)
		}
		get.Header.Set("Authorization", "Bearer "+h.bearer)
		content, err := h.api.Client().Do(get)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(content.Body)
		content.Body.Close()
		if err != nil || content.StatusCode != http.StatusOK || string(data) != name {
			t.Fatalf("content after native notification: status=%d data=%q error=%v", content.StatusCode, data, err)
		}
	}
	if starts != 1 {
		t.Fatalf("control server was not reused after HTTP reconnect: starts=%d", starts)
	}
}

// Explicit local RPC verification; no account or model turn is required.
func TestRealWorkspaceWatch(t *testing.T) {
	if os.Getenv("COBOT_R_FILESYSTEM") != "1" {
		t.Skip("set COBOT_R_FILESYSTEM=1 and an isolated COBOT_R_FILESYSTEM_ROOT")
	}
	root := os.Getenv("COBOT_R_FILESYSTEM_ROOT")
	if !filepath.IsAbs(root) {
		t.Fatal("COBOT_R_FILESYSTEM_ROOT must be an explicit absolute test data root")
	}
	binary, err := codex.Locate(os.Getenv("COBOT_R_FILESYSTEM_BINARY"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	version, err := codex.ProbeVersion(ctx, binary)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("binary=%s version=%s dataRoot=%s", binary, version, root)
	paths, err := platform.Resolve(platform.EnvDevelopment, filepath.Join(root, "profile"))
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(root, "workspace")
	if err := os.MkdirAll(filepath.Join(work, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	server, err := codex.Start(ctx, codex.ServerOptions{Binary: binary, Cwd: work, Env: codex.EngineEnv(paths, nil), Client: codex.ClientInfo{Name: "cobot-filesystem-watch", Version: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(stop)
	}()
	watchCtx, stop := context.WithCancel(ctx)
	defer stop()
	rootChanged, err := server.WatchDirectory(watchCtx, work)
	if err != nil {
		t.Fatal(err)
	}
	childChanged, err := server.WatchDirectory(watchCtx, filepath.Join(work, "nested"))
	if err != nil {
		t.Fatal(err)
	}
	for index, name := range []string{"created.txt", "nested/child.txt"} {
		changed := []<-chan struct{}{rootChanged, childChanged}[index]
		if err := os.WriteFile(filepath.Join(work, name), []byte("filesystem notification fixture\n"), 0600); err != nil {
			t.Fatal(err)
		}
		select {
		case _, ok := <-changed:
			if !ok {
				t.Fatal("watch closed before change")
			}
		case <-time.After(8 * time.Second):
			t.Fatalf("no change notification for %s", name)
		}
	}
	stop()
	for _, changed := range []<-chan struct{}{rootChanged, childChanged} {
		closed := false
		for !closed {
			select {
			case _, ok := <-changed:
				closed = !ok
			case <-ctx.Done():
				t.Fatal("watch not released")
			}
		}
	}
}
