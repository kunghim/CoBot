package app

import (
	"context"
	"errors"
	"io/fs"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/latex"
	"github.com/kunghim/CoBot/internal/workspace"
)

type WorkspaceLatexProject struct {
	workspace.LatexProject
	Available bool `json:"available"`
}

type WorkspaceLatexResult struct {
	Success      bool   `json:"success"`
	Main         string `json:"main"`
	PDF          []byte `json:"pdf,omitempty"`
	Log          string `json:"log"`
	LogTruncated bool   `json:"logTruncated"`
	ErrorKey     string `json:"errorKey,omitempty"`
}

func (a *App) DiscoverWorkspaceLatex(ctx context.Context, principal auth.Principal, scope, id, path string) (WorkspaceLatexProject, error) {
	var result WorkspaceLatexProject
	directory, err := a.workspaceDirectory(ctx, principal, scope, id)
	if err != nil {
		return result, err
	}
	if len(path) > 4096 {
		return result, workspaceError(fs.ErrInvalid)
	}
	result.LatexProject, err = workspace.DiscoverLatex(ctx, directory, path)
	result.Available = a.WorkspaceLatex != nil && latex.Available(*a.WorkspaceLatex)
	return result, workspaceError(err)
}

func (a *App) CompileWorkspaceLatex(ctx context.Context, principal auth.Principal, scope, id, main string) (WorkspaceLatexResult, error) {
	var result WorkspaceLatexResult
	directory, err := a.workspaceDirectory(ctx, principal, scope, id)
	if err != nil {
		return result, err
	}
	if len(main) == 0 || len(main) > 4096 {
		return result, workspaceError(fs.ErrInvalid)
	}
	file, err := workspace.Open(directory, main)
	if err != nil {
		return result, workspaceError(err)
	}
	result.Main = file.Info.Path
	valid := file.Info.Kind == "latex"
	oversized := file.Info.Size > 10<<20
	file.Close()
	if !valid {
		return result, workspaceError(fs.ErrInvalid)
	}
	if oversized {
		return result, domain.InvalidRequest("error.workspace.latex_too_large", nil)
	}
	if a.WorkspaceLatex == nil || !latex.Available(*a.WorkspaceLatex) {
		return result, domain.CapabilityMissing("error.workspace.latex_unavailable", nil, nil)
	}
	// Keep ephemeral compilations bounded across tabs and scopes without creating
	// durable jobs or allowing a cancelled request to sit in a work queue.
	if !a.latexMu.TryLock() {
		return result, domain.CapabilityMissing("error.workspace.latex_busy", nil, nil)
	}
	defer a.latexMu.Unlock()
	compiled, err := latex.Compile(ctx, *a.WorkspaceLatex, directory, result.Main)
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if errors.Is(err, latex.ErrUnavailable) {
		return result, domain.CapabilityMissing("error.workspace.latex_unavailable", nil, err)
	}
	result.Log, result.LogTruncated = compiled.Log, compiled.LogTruncated
	if err != nil {
		result.ErrorKey = "error.workspace.latex_failed"
		if errors.Is(err, context.DeadlineExceeded) {
			result.ErrorKey = "error.workspace.latex_timeout"
		}
		if errors.Is(err, latex.ErrOutputLimit) {
			result.ErrorKey = "error.workspace.latex_output_limit"
		}
		return result, nil
	}
	result.Success, result.PDF = true, compiled.PDF
	return result, nil
}
