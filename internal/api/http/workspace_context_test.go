package http

import (
	"context"
	"testing"
	"time"
)

func TestWorkspaceTasksStopWithCoreOrRequest(t *testing.T) {
	for _, stopCore := range []bool{true, false} {
		stop := make(chan struct{})
		server := &Server{StreamStop: stop}
		request, disconnect := context.WithCancel(context.Background())
		task, release := server.workspaceTaskContext(request)
		if stopCore {
			close(stop)
		} else {
			disconnect()
		}
		select {
		case <-task.Done():
		case <-time.After(time.Second):
			t.Fatal("ephemeral task survived shutdown")
		}
		release()
		disconnect()
	}
	stop := make(chan struct{})
	close(stop)
	task, release := (&Server{StreamStop: stop}).workspaceTaskContext(context.Background())
	defer release()
	if task.Err() != context.Canceled {
		t.Fatal("task started after core shutdown")
	}
}
