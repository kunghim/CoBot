// Package logging configures the product logger: rotating file output with a
// fixed size/count budget plus an optional console sink. Handlers must never
// pass auth headers, tokens or bootstrap secrets into log attributes.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// Options configures one logger instance.
type Options struct {
	Dir     string
	Level   slog.Level
	Console bool
}

// New builds the product logger and returns a close function for the file sink.
func New(opts Options) (*slog.Logger, func() error, error) {
	var writers []io.Writer
	if opts.Console {
		writers = append(writers, os.Stdout)
	}
	fileWriter, closer, err := newRotatingFile(filepath.Join(opts.Dir, "cobot.log"), 10<<20, 5)
	if err != nil {
		return nil, nil, err
	}
	writers = append(writers, fileWriter)
	handler := slog.NewJSONHandler(io.MultiWriter(writers...), &slog.HandlerOptions{Level: opts.Level})
	logger := slog.New(handler)
	return logger, closer, nil
}

// rotatingFile is a minimal size-based writer: one active file plus keep-1
// rotated siblings, keeping the total footprint bounded.
type rotatingFile struct {
	mu   sync.Mutex
	path string
	size int64
	max  int64
	keep int
	f    *os.File
}

func newRotatingFile(path string, maxSize int64, keep int) (*rotatingFile, func() error, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, fmt.Errorf("create log dir: %w", err)
	}
	rf := &rotatingFile{path: path, max: maxSize, keep: keep}
	if err := rf.open(); err != nil {
		return nil, nil, err
	}
	return rf, rf.Close, nil
}

func (r *rotatingFile) open() error {
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return fmt.Errorf("stat log file: %w", err)
	}
	r.f = f
	r.size = info.Size()
	return nil
}

func (r *rotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.size+int64(len(p)) > r.max {
		if err := r.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

func (r *rotatingFile) rotate() error {
	if err := r.f.Close(); err != nil {
		return err
	}
	_ = os.Remove(fmt.Sprintf("%s.%d", r.path, r.keep))
	for i := r.keep - 1; i >= 1; i-- {
		_ = os.Rename(fmt.Sprintf("%s.%d", r.path, i), fmt.Sprintf("%s.%d", r.path, i+1))
	}
	_ = os.Rename(r.path, r.path+".1")
	return r.open()
}

// Close releases the active file.
func (r *rotatingFile) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return nil
	}
	err := r.f.Close()
	r.f = nil
	return err
}
