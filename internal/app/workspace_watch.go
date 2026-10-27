package app

import (
	"context"
	"path/filepath"
	"sync"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/workspace"
)

// WatchWorkspace validates every directory before starting any native watches.
// Events contain no path information and are never used as filesystem state.
func (a *App) WatchWorkspace(ctx context.Context, principal auth.Principal, scope, id string, paths []string) (<-chan struct{}, func(), error) {
	directory, err := a.workspaceDirectory(ctx, principal, scope, id)
	if err != nil {
		return nil, nil, err
	}
	if len(paths) == 0 || len(paths) > 64 {
		return nil, nil, domain.InvalidRequest("error.workspace.invalid_path", nil)
	}
	unique := make(map[string]bool)
	var absolute []string
	for _, path := range paths {
		if len(path) > 4096 {
			return nil, nil, domain.InvalidRequest("error.workspace.invalid_path", nil)
		}
		file, err := workspace.Open(directory, path)
		if err != nil {
			return nil, nil, workspaceError(err)
		}
		name := filepath.Join(file.Root.Name(), filepath.FromSlash(file.Info.Path))
		isDirectory := file.Info.Kind == "directory"
		file.Close()
		if !isDirectory {
			return nil, nil, domain.InvalidRequest("error.workspace.invalid_path", nil)
		}
		if !unique[name] {
			unique[name] = true
			absolute = append(absolute, name)
		}
	}
	watch := a.WorkspaceWatchFactory
	if watch == nil {
		setup, cancel := context.WithTimeout(ctx, 10*time.Second)
		server, err := a.control(setup)
		cancel()
		if err != nil {
			return nil, nil, domain.CapabilityMissing("error.workspace.watch_unavailable", nil, err)
		}
		watch = server.WatchDirectory
	}
	lifetime, cancel := context.WithCancel(ctx)
	changed := make(chan struct{}, 1)
	var group sync.WaitGroup
	for _, path := range absolute {
		events, err := watch(lifetime, path)
		if err != nil {
			cancel()
			return nil, nil, domain.CapabilityMissing("error.workspace.watch_unavailable", nil, err)
		}
		group.Add(1)
		go func() {
			defer group.Done()
			for {
				select {
				case <-lifetime.Done():
					return
				case _, ok := <-events:
					if !ok {
						cancel()
						return
					}
					select {
					case changed <- struct{}{}:
					default:
					}
				}
			}
		}()
	}
	go func() { group.Wait(); close(changed) }()
	return changed, cancel, nil
}
