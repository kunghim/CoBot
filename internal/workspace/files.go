// Package workspace provides the bounded, read-only transport used by the
// file viewer. It does not expose tools to the model or execute file contents.
package workspace

import (
	"container/heap"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const PageSize = 200
const MaxPreviewBytes int64 = 256 << 20

var ErrOutside = errors.New("path is outside workspace")
var position = regexp.MustCompile(`(?:#L([0-9]+)(?:-L?([0-9]+))?|:([0-9]+)(?::([0-9]+))?)$`)

type File struct {
	Path       string    `json:"path"`
	Name       string    `json:"name"`
	Kind       string    `json:"kind"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modifiedAt"`
	Version    string    `json:"version"`
	Line       int       `json:"line,omitempty"`
	Column     int       `json:"column,omitempty"`
	EndLine    int       `json:"endLine,omitempty"`
}

type Directory struct {
	Path       string `json:"path"`
	Entries    []File `json:"entries"`
	NextCursor string `json:"nextCursor,omitempty"`
}

// Opened retains the root and final descriptor together. os.Root rejects
// symlink escapes during traversal, including replacements after validation.
type Opened struct {
	Root   *os.Root
	Handle *os.File
	Info   File
}

func (f *Opened) Close() error {
	a := f.Handle.Close()
	b := f.Root.Close()
	return errors.Join(a, b)
}

func literalExists(boundary *os.Root, rootPath, target string) bool {
	if filepath.IsAbs(target) {
		rel, err := filepath.Rel(rootPath, target)
		if err != nil || !filepath.IsLocal(rel) {
			// The owner may use an absolute alias of the workspace root.
			resolved, err := filepath.EvalSymlinks(target)
			if err != nil {
				return false
			}
			rel, err = filepath.Rel(rootPath, resolved)
			if err != nil {
				return false
			}
		}
		target = rel
	}
	if !filepath.IsLocal(target) {
		return false
	}
	_, err := boundary.Lstat(target)
	return err == nil
}

func relative(boundary *os.Root, root, raw string) (string, error) {
	target := raw
	if trimmed := strings.TrimSpace(raw); trimmed != raw {
		// Pasted navigation may have surrounding whitespace, but a literal
		// filename (including a position suffix) must win over that cleanup.
		exists := literalExists(boundary, root, raw)
		if !exists {
			if suffix := position.FindString(raw); suffix != "" {
				exists = literalExists(boundary, root, strings.TrimSuffix(raw, suffix))
			}
		}
		if !exists {
			target = trimmed
		}
	}
	if strings.HasPrefix(strings.ToLower(target), "file:") {
		u, err := url.Parse(target)
		if err != nil || u.Host != "" && !strings.EqualFold(u.Host, "localhost") {
			return "", ErrOutside
		}
		target = u.Path
		if u.Fragment != "" {
			target += "#" + u.Fragment
		}
	} else {
		if !literalExists(boundary, root, target) {
			decoded, err := url.PathUnescape(target)
			if err != nil {
				return "", ErrOutside
			}
			target = decoded
		}
	}
	if strings.ContainsRune(target, 0) || strings.HasPrefix(target, "//") {
		return "", ErrOutside
	}
	if len(target) >= 3 && target[0] == '/' && target[2] == ':' {
		target = target[1:]
	}
	if filepath.IsAbs(target) {
		resolved, resolveErr := filepath.EvalSymlinks(target)
		if resolveErr != nil {
			if suffix := position.FindString(target); suffix != "" {
				if plain, err := filepath.EvalSymlinks(strings.TrimSuffix(target, suffix)); err == nil {
					resolved, resolveErr = plain+suffix, nil
				}
			}
		}
		if resolveErr != nil {
			return "", resolveErr
		}
		target = resolved
		var err error
		target, err = filepath.Rel(root, target)
		if err != nil {
			return "", ErrOutside
		}
	}
	target = filepath.Clean(target)
	if target == ".." || strings.HasPrefix(target, ".."+string(filepath.Separator)) || filepath.IsAbs(target) {
		return "", ErrOutside
	}
	return target, nil
}

func Open(directory, raw string) (*Opened, error) {
	if directory == "" {
		return nil, ErrOutside
	}
	rootPath, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, err
	}
	rel, err := relative(root, rootPath, raw)
	if err != nil {
		root.Close()
		return nil, err
	}
	handle, resolved, err := openResolved(root, rootPath, rel)
	var line, column, endLine int
	// A real filename containing a position-like suffix wins over navigation.
	if err != nil && (errors.Is(err, fs.ErrNotExist) || runtime.GOOS == "windows" && !errors.Is(err, ErrOutside)) {
		if match := position.FindStringSubmatch(rel); match != nil {
			plain := position.ReplaceAllString(rel, "")
			handle, resolved, err = openResolved(root, rootPath, plain)
			if err == nil {
				rel = plain
				line, _ = strconv.Atoi(match[1])
				endLine, _ = strconv.Atoi(match[2])
				if match[3] != "" {
					line, _ = strconv.Atoi(match[3])
					column, _ = strconv.Atoi(match[4])
				}
			}
		}
	}
	if err != nil {
		root.Close()
		return nil, err
	}
	stat, err := handle.Stat()
	if err != nil {
		handle.Close()
		root.Close()
		return nil, err
	}
	if !stat.IsDir() && !stat.Mode().IsRegular() {
		handle.Close()
		root.Close()
		return nil, fs.ErrPermission
	}
	info := describe(resolved, stat)
	if !stat.IsDir() {
		var sample [4096]byte
		n, err := handle.ReadAt(sample[:], 0)
		if err != nil && !errors.Is(err, io.EOF) {
			handle.Close()
			root.Close()
			return nil, err
		}
		info.Kind = kind(info.Name, sample[:n])
	}
	info.Line, info.Column, info.EndLine = line, column, endLine
	return &Opened{Root: root, Handle: handle, Info: info}, nil
}

func openResolved(root *os.Root, rootPath, path string) (*os.File, string, error) {
	real, err := filepath.EvalSymlinks(filepath.Join(rootPath, path))
	if err != nil {
		return nil, "", err
	}
	rel, err := filepath.Rel(rootPath, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return nil, "", ErrOutside
	}
	stat, err := root.Stat(rel)
	if err != nil {
		return nil, "", err
	}
	if !stat.IsDir() && !stat.Mode().IsRegular() {
		return nil, "", fs.ErrPermission
	}
	f, err := openAt(root, rel)
	return f, rel, err
}

func describe(path string, stat fs.FileInfo) File {
	k := "file"
	if stat.IsDir() {
		k = "directory"
	}
	if path == "." {
		path = ""
	}
	return File{Path: filepath.ToSlash(path), Name: stat.Name(), Kind: k, Size: stat.Size(), ModifiedAt: stat.ModTime().UTC(), Version: fmt.Sprintf(`"%x-%x"`, stat.ModTime().UnixNano(), stat.Size())}
}

func kind(name string, sample []byte) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".md", ".markdown", ".mdown", ".mkd", ".mdx":
		return "markdown"
	case ".html", ".htm":
		return "html"
	case ".svg":
		return "svg"
	case ".avif", ".bmp", ".gif", ".ico", ".jpeg", ".jpg", ".png", ".webp":
		return "image"
	case ".pdf":
		return "pdf"
	case ".aac", ".flac", ".m4a", ".mp3", ".mp4a", ".oga", ".ogg", ".opus", ".wav":
		return "audio"
	case ".mp4", ".ogv", ".webm":
		return "video"
	case ".docx":
		return "document"
	case ".xlsx", ".xlsm", ".csv", ".tsv":
		return "spreadsheet"
	case ".pptx":
		return "presentation"
	case ".ipynb":
		return "notebook"
	case ".tex":
		return "latex"
	case ".doc", ".xls", ".ppt", ".rtf", ".pages", ".numbers", ".key", ".odt", ".ods", ".odp", ".tif", ".tiff":
		return "binary"
	}
	if strings.IndexByte(string(sample), 0) >= 0 {
		return "binary"
	}
	if utf8.Valid(sample) {
		return "text"
	}
	// Only an incomplete trailing rune may be truncated, not invalid bytes.
	for trim := 1; trim <= 3 && trim <= len(sample); trim++ {
		tail := sample[len(sample)-trim:]
		if utf8.RuneStart(tail[0]) && !utf8.FullRune(tail) && utf8.Valid(sample[:len(sample)-trim]) {
			return "text"
		}
	}
	return "binary"
}

type entryHeap []string

func (h entryHeap) Len() int           { return len(h) }
func (h entryHeap) Less(i, j int) bool { return h[i] > h[j] }
func (h entryHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *entryHeap) Push(v any)        { *h = append(*h, v.(string)) }
func (h *entryHeap) Pop() any          { old := *h; v := old[len(old)-1]; *h = old[:len(old)-1]; return v }

// List scans one directory in bounded batches. A small heap keeps the next
// sorted page, so a directory with millions of names is not loaded in RAM.
func List(ctx context.Context, directory, raw, cursor string, includeHidden bool) (Directory, error) {
	f, err := Open(directory, raw)
	if err != nil {
		return Directory{}, err
	}
	defer f.Close()
	if f.Info.Kind != "directory" {
		return Directory{}, &fs.PathError{Op: "readdir", Path: raw, Err: fs.ErrInvalid}
	}
	after := ""
	if cursor != "" {
		if len(cursor) > 2048 {
			return Directory{}, fs.ErrInvalid
		}
		b, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil {
			return Directory{}, fs.ErrInvalid
		}
		after = string(b)
	}
	names := &entryHeap{}
	for {
		if err := ctx.Err(); err != nil {
			return Directory{}, err
		}
		batch, err := f.Handle.ReadDir(256)
		if err != nil && !errors.Is(err, io.EOF) {
			return Directory{}, err
		}
		for _, entry := range batch {
			name := entry.Name()
			if !includeHidden && strings.HasPrefix(name, ".") {
				continue
			}
			prefix := "1"
			if entry.IsDir() {
				prefix = "0"
			}
			key := prefix + name
			if key <= after {
				continue
			}
			if names.Len() < PageSize+1 {
				heap.Push(names, key)
			} else if key < (*names)[0] {
				(*names)[0] = key
				heap.Fix(names, 0)
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
	}
	sort.Strings(*names)
	result := Directory{Path: f.Info.Path, Entries: []File{}}
	if names.Len() > PageSize {
		result.NextCursor = base64.RawURLEncoding.EncodeToString([]byte((*names)[PageSize-1]))
		*names = (*names)[:PageSize]
	}
	for _, key := range *names {
		path := filepath.Join(f.Info.Path, key[1:])
		// Use the original root descriptor, rather than reopening a mutable
		// absolute pathname or trusting DirEntry.Info through a symlink.
		child, _, err := openResolved(f.Root, f.Root.Name(), path)
		if err != nil {
			result.Entries = append(result.Entries, File{Path: filepath.ToSlash(path), Name: key[1:], Kind: "blocked"})
			continue
		}
		stat, err := child.Stat()
		child.Close()
		if err != nil {
			continue
		}
		info := describe(path, stat)
		info.Name = key[1:]
		if !stat.IsDir() {
			info.Kind = kind(info.Name, nil)
		}
		result.Entries = append(result.Entries, info)
	}
	return result, nil
}
