package codex

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type startedWriter struct {
	io.WriteCloser
	started chan struct{}
}

func (w startedWriter) Write(data []byte) (int, error) {
	close(w.started)
	return w.WriteCloser.Write(data)
}

func TestShutdownDeadlineWithProcessNotReadingInput(t *testing.T) {
	if os.Getenv("COBOT_BLOCKED_PIPE_HELPER") == "1" {
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestShutdownDeadlineWithProcessNotReadingInput$")
	cmd.Env = append(os.Environ(), "COBOT_BLOCKED_PIPE_HELPER=1")
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = in.Close(); _ = cmd.Process.Kill() })
	started := make(chan struct{})
	client := NewClient(startedWriter{in, started}, out, ClientHooks{})
	server := &Server{cmd: cmd, client: client}
	called := make(chan error, 1)
	go func() { called <- client.Call(context.Background(), "large", strings.Repeat("x", 1<<20), nil) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("writer not entered")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- server.Shutdown(ctx) }()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("Shutdown blocked before deadline handling")
	}
	select {
	case err := <-called:
		if err == nil {
			t.Fatal("blocked request succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("writer survived shutdown")
	}
}
