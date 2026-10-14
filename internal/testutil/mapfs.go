// Package testutil contains tiny in-memory helpers for tests. It exists
// because parts of the local Go toolchain distribution are missing standard
// test-only packages; keeping tests on the standard library surface that is
// actually present avoids depending on them.
package testutil

import (
	"io"
	"io/fs"
	"sort"
	"strings"
	"sync"
	"time"
)

// MapFS is a minimal read-only flat fs.FS (root files only) with ReadDir
// support so tools that glob the root (for example goose) work unchanged.
type MapFS struct {
	mu    sync.RWMutex
	files map[string][]byte
}

// NewMapFS builds an fs.FS from path/content pairs. Only root-level files
// are supported, which is all the migration tests need.
func NewMapFS(files map[string]string) *MapFS {
	m := &MapFS{files: map[string][]byte{}}
	for path, content := range files {
		m.files[path] = []byte(content)
	}
	return m
}

// Open implements fs.FS.
func (m *MapFS) Open(name string) (fs.File, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if name == "." || name == "/" {
		entries := m.dirEntriesLocked()
		return &mapDir{name: ".", entries: entries}, nil
	}
	data, ok := m.files[name]
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &mapFile{name: name, data: data}, nil
}

// ReadDir implements fs.ReadDirFS for the root directory.
func (m *MapFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if name != "." && name != "/" {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.dirEntriesLocked(), nil
}

// ReadFile implements fs.ReadFileFS.
func (m *MapFS) ReadFile(name string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	data, ok := m.files[name]
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	out := make([]byte, len(data))
	copy(out, data)
	return out, nil
}

func (m *MapFS) dirEntriesLocked() []fs.DirEntry {
	names := make([]string, 0, len(m.files))
	for name := range m.files {
		names = append(names, name)
	}
	sort.Strings(names)
	entries := make([]fs.DirEntry, 0, len(names))
	for _, name := range names {
		entries = append(entries, mapDirEntry{name: name, size: int64(len(m.files[name]))})
	}
	return entries
}

type mapFile struct {
	name string
	data []byte
	pos  int
}

func (f *mapFile) Stat() (fs.FileInfo, error) {
	return mapFileInfo{name: f.name, size: int64(len(f.data))}, nil
}

func (f *mapFile) Read(p []byte) (int, error) {
	if f.pos >= len(f.data) {
		return 0, io.EOF
	}
	n := copy(p, f.data[f.pos:])
	f.pos += n
	return n, nil
}

func (f *mapFile) Close() error { return nil }

type mapDir struct {
	name    string
	entries []fs.DirEntry
	pos     int
}

func (d *mapDir) Stat() (fs.FileInfo, error) {
	return mapFileInfo{name: d.name, dir: true}, nil
}

func (d *mapDir) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: d.name, Err: fs.ErrInvalid}
}

func (d *mapDir) Close() error { return nil }

func (d *mapDir) ReadDir(count int) ([]fs.DirEntry, error) {
	if count <= 0 {
		out := d.entries[d.pos:]
		d.pos = len(d.entries)
		return out, nil
	}
	if d.pos >= len(d.entries) {
		return nil, io.EOF
	}
	end := d.pos + count
	if end > len(d.entries) {
		end = len(d.entries)
	}
	out := d.entries[d.pos:end]
	d.pos = end
	return out, nil
}

type mapDirEntry struct {
	name string
	size int64
}

func (e mapDirEntry) Name() string               { return e.name }
func (e mapDirEntry) IsDir() bool                { return false }
func (e mapDirEntry) Type() fs.FileMode          { return 0 }
func (e mapDirEntry) Info() (fs.FileInfo, error) { return mapFileInfo{name: e.name, size: e.size}, nil }

type mapFileInfo struct {
	name string
	size int64
	dir  bool
}

func (i mapFileInfo) Name() string { return i.name }
func (i mapFileInfo) Size() int64  { return i.size }
func (i mapFileInfo) Mode() fs.FileMode {
	if i.dir {
		return fs.ModeDir | 0o555
	}
	return 0o644
}
func (i mapFileInfo) ModTime() time.Time { return time.Time{} }
func (i mapFileInfo) IsDir() bool        { return i.dir }
func (i mapFileInfo) Sys() any           { return nil }

// Basename is a tiny helper for path-free comparisons in tests.
func Basename(path string) string {
	if idx := strings.LastIndexAny(path, `/\`); idx >= 0 {
		return path[idx+1:]
	}
	return path
}
