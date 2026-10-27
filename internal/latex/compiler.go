// Package latex runs an explicitly supplied compiler behind a platform boundary.
// It never installs tools or downloads compilation resources.
package latex

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kunghim/CoBot/internal/workspace"
)

var ErrUnavailable = errors.New("isolated LaTeX compiler unavailable")

const maxLog = 128 << 10
const maxPDF = 32 << 20

type Config struct {
	Compiler, Cache, Temporary string
	Timeout                    time.Duration
}
type Result struct {
	PDF          []byte
	Log          string
	LogTruncated bool
}

func Compile(ctx context.Context, config Config, project, main string) (Result, error) {
	var result Result
	if err := ctx.Err(); err != nil {
		return result, err
	}
	for _, value := range []string{config.Compiler, config.Cache, config.Temporary, project} {
		if !filepath.IsAbs(value) {
			return result, ErrUnavailable
		}
	}
	compiler, err := filepath.EvalSymlinks(config.Compiler)
	if err != nil {
		return result, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	cache, err := filepath.EvalSymlinks(config.Cache)
	if err != nil {
		return result, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	source, err := workspace.Open(project, main)
	if err != nil {
		return result, err
	}
	if source.Info.Kind != "latex" || source.Info.Size > 10<<20 {
		source.Close()
		return result, errors.New("main file is not TeX")
	}
	project = source.Root.Name()
	input := filepath.Join(project, filepath.FromSlash(source.Info.Path))
	name := strings.TrimSuffix(source.Info.Name, filepath.Ext(source.Info.Name)) + ".pdf"
	source.Close()
	output, err := os.MkdirTemp(config.Temporary, "latex-")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(output)
	output, err = filepath.EvalSymlinks(output)
	if err != nil {
		return result, err
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if timeout > time.Minute {
		timeout = time.Minute
	}
	run, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd, err := isolatedCommand(run, compiler, cache, project, output, input)
	if err != nil {
		return result, err
	}
	cmd.Dir = project
	cmd.Env = compilerEnv(cache, output)
	cmd.WaitDelay = 2 * time.Second
	log := &boundedLog{}
	cmd.Stdout, cmd.Stderr = log, log
	var outputExceeded atomic.Bool
	monitored := make(chan struct{})
	go func() {
		defer close(monitored)
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-run.Done():
				return
			case <-ticker.C:
				if checkOutput(output) != nil {
					outputExceeded.Store(true)
					cancel()
					return
				}
			}
		}
	}()
	err = cmd.Run()
	reason := run.Err()
	cancel()
	<-monitored
	result.Log, result.LogTruncated = log.snapshot()
	if outputExceeded.Load() {
		return result, ErrOutputLimit
	}
	if reason != nil {
		return result, reason
	}
	if err != nil {
		return result, fmt.Errorf("LaTeX compilation failed: %w", err)
	}
	if err := checkOutput(output); err != nil {
		return result, err
	}
	pdf, err := workspace.Open(output, name)
	if err != nil {
		return result, err
	}
	defer pdf.Close()
	if pdf.Info.Kind != "pdf" || pdf.Info.Size > maxPDF {
		return result, errors.New("invalid or oversized LaTeX PDF")
	}
	result.PDF, err = io.ReadAll(io.LimitReader(pdf.Handle, maxPDF+1))
	if err != nil {
		result.PDF = nil
		return result, err
	}
	if len(result.PDF) > maxPDF || !strings.HasPrefix(string(result.PDF[:min(5, len(result.PDF))]), "%PDF-") {
		result.PDF = nil
		return result, errors.New("invalid LaTeX PDF")
	}
	return result, nil
}

func compilerEnv(cache, temporary string) []string {
	allowed := map[string]bool{"PATH": true, "HOME": true, "USER": true, "LOGNAME": true, "LANG": true, "LC_ALL": true, "SYSTEMROOT": true, "WINDIR": true}
	var result []string
	for _, value := range os.Environ() {
		key, _, ok := strings.Cut(value, "=")
		if ok && allowed[strings.ToUpper(key)] {
			result = append(result, value)
		}
	}
	return append(result, "TECTONIC_CACHE_DIR="+cache, "TECTONIC_UNTRUSTED_MODE=1", "TMPDIR="+temporary)
}

type boundedLog struct {
	mu        sync.Mutex
	data      []byte
	truncated bool
}

func (w *boundedLog) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	count := len(data)
	remaining := maxLog - len(w.data)
	if len(data) > remaining {
		w.truncated = true
		data = data[:remaining]
	}
	w.data = append(w.data, data...)
	return count, nil
}
func (w *boundedLog) snapshot() (string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.data), w.truncated
}
