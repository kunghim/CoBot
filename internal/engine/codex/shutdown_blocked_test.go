package codex

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/engine"
)

type startedWriter struct {
	io.WriteCloser
	started chan struct{}
}

func TestTransportFailureReapsOwnedProcessBeforeHostCallback(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestShutdownDeadlineWithProcessNotReadingInput$")
	cmd.Env = append(os.Environ(), "COBOT_BLOCKED_PIPE_HELPER=1")
	cmd.WaitDelay = time.Second
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
	host := make(chan struct{})
	server := &Server{cmd: cmd, callbacks: engine.Callbacks{OnExit: func(error) { close(host) }}}
	server.client = NewClient(in, out, ClientHooks{OnExit: server.transportExited})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := server.client.Call(ctx, "large", strings.Repeat("x", 1<<20), nil); err == nil {
		t.Fatal("blocked write succeeded")
	}
	select {
	case <-host:
	case <-time.After(3 * time.Second):
		t.Fatal("host never observed reaped transport failure")
	}
	if cmd.ProcessState == nil {
		t.Fatal("host notified before owned process was reaped")
	}
	stop, cancelStop := context.WithTimeout(context.Background(), time.Second)
	defer cancelStop()
	if err := server.Shutdown(stop); err != nil {
		t.Fatal("repeated shutdown after transport failure", err)
	}
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
