package platform

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ErrNotARepo reports that git could not describe a repository: either the
// git executable is unavailable or the directory is not inside a work tree.
var ErrNotARepo = errors.New("not a git repository")

// CanonicalDir is a directory resolved to its real location and normalized
// for binding comparisons.
//
// Key is the comparison key: forward slashes, no trailing slash except for a
// drive root such as "c:/", and lowercased on Windows where directories are
// case-insensitive. Display is the resolved real path with native separators,
// kept for user-facing messages.
type CanonicalDir struct {
	Key     string
	Display string
}

// CanonicalDirFrom resolves path into a CanonicalDir.
//
// The path is made absolute against the current directory and lexically
// cleaned, then symlinks and junctions are resolved on the longest existing
// ancestor, so a leaf that does not exist yet still canonicalizes. A path
// that cannot be placed under any existing directory — an absent volume root
// or an existing element that resolves nowhere, like a dangling symlink — is
// an error: bindings must describe real directories.
func CanonicalDirFrom(path string) (CanonicalDir, error) {
	if strings.TrimSpace(path) == "" {
		return CanonicalDir{}, errors.New("canonicalize directory: empty path")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return CanonicalDir{}, fmt.Errorf("canonicalize %q: make absolute: %w", path, err)
	}
	resolved, missing, err := resolveExisting(filepath.Clean(abs))
	if err != nil {
		return CanonicalDir{}, err
	}
	display := resolved
	if len(missing) > 0 {
		display = filepath.Join(append([]string{resolved}, missing...)...)
	}
	return CanonicalDir{Key: canonicalKey(display), Display: display}, nil
}

// Contains reports whether other is c itself or a directory nested under c.
// Elements are compared one by one, never as a raw string prefix, so "/a/b"
// does not contain "/a/bc".
func (c CanonicalDir) Contains(other CanonicalDir) bool {
	if c.Key == "" || other.Key == "" {
		return c.Key == other.Key
	}
	this, that := splitKey(c.Key), splitKey(other.Key)
	if len(that) < len(this) {
		return false
	}
	for i := range this {
		if this[i] != that[i] {
			return false
		}
	}
	return true
}

// Equal reports whether both values describe the same directory.
func (c CanonicalDir) Equal(other CanonicalDir) bool {
	return c.Key == other.Key
}

// resolveExisting walks path upward from its leaf and returns the longest
// existing ancestor resolved through symlinks, plus the not-yet-existing
// elements below it in path order.
func resolveExisting(path string) (string, []string, error) {
	var missing []string
	cur := path
	for {
		_, lerr := os.Lstat(cur)
		if lerr == nil {
			real, rerr := evalReal(cur)
			if rerr != nil {
				if os.IsNotExist(rerr) {
					return "", nil, fmt.Errorf("canonicalize %q: %w: %q does not resolve to an existing directory", path, rerr, cur)
				}
				return "", nil, fmt.Errorf("canonicalize %q: resolve symlinks in %q: %w", path, cur, rerr)
			}
			for i, j := 0, len(missing)-1; i < j; i, j = i+1, j-1 {
				missing[i], missing[j] = missing[j], missing[i]
			}
			return real, missing, nil
		}
		if !os.IsNotExist(lerr) {
			return "", nil, fmt.Errorf("canonicalize %q: inspect %q: %w", path, cur, lerr)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", nil, fmt.Errorf("canonicalize %q: %w: no existing ancestor under %q", path, lerr, cur)
		}
		missing = append(missing, filepath.Base(cur))
		cur = parent
	}
}

// evalReal resolves path through symlinks and junctions. On Windows,
// filepath.EvalSymlinks alone leaves junctions untouched since Go 1.23
// reports them as ordinary directories, but a binding must recognize the
// same real directory no matter how it is spelled.
func evalReal(path string) (string, error) {
	if runtime.GOOS != "windows" {
		return filepath.EvalSymlinks(path)
	}
	for i := 0; i < 8; i++ {
		dejunctioned, err := resolveJunctions(path)
		if err != nil {
			return "", err
		}
		resolved, err := filepath.EvalSymlinks(dejunctioned)
		if err != nil {
			return "", err
		}
		if resolved == path {
			return resolved, nil
		}
		path = resolved
	}
	return path, nil
}

// resolveJunctions returns path with every junction element replaced by that
// junction's target.
func resolveJunctions(path string) (string, error) {
	const maxLinks = 255
	for links := 0; ; links++ {
		if links > maxLinks {
			return "", errors.New("resolve junctions: too many links")
		}
		next, found, err := firstJunctionTarget(path)
		if err != nil {
			return "", err
		}
		if !found {
			return path, nil
		}
		path = next
	}
}

// firstJunctionTarget returns path with its first junction element replaced
// by that junction's target; found reports whether any junction was present.
func firstJunctionTarget(path string) (string, bool, error) {
	vol := filepath.VolumeName(path)
	rest := strings.TrimLeft(path[len(vol):], `/\`)
	dest := vol + string(filepath.Separator)
	for rest != "" {
		elem := rest
		if i := strings.IndexAny(rest, `/\`); i >= 0 {
			elem, rest = rest[:i], rest[i+1:]
		} else {
			rest = ""
		}
		if elem == "" {
			continue
		}
		if elem == ".." {
			// Pop one element without ever leaving the volume.
			if parent := filepath.Dir(dest); strings.HasPrefix(parent, vol) {
				dest = parent
			}
			continue
		}
		dest = filepath.Join(dest, elem)
		target, isLink, err := junctionTarget(dest)
		if err != nil {
			return "", false, err
		}
		if !isLink {
			continue
		}
		if filepath.IsAbs(target) {
			return filepath.Join(target, rest), true, nil
		}
		return filepath.Join(filepath.Dir(dest), target, rest), true, nil
	}
	return path, false, nil
}

// junctionTarget reports whether p is a junction and, if so, its target.
// Genuine symlinks are left for filepath.EvalSymlinks to resolve.
func junctionTarget(p string) (string, bool, error) {
	fi, err := os.Lstat(p)
	if err != nil {
		return "", false, err
	}
	if fi.Mode()&fs.ModeSymlink != 0 {
		return "", false, nil
	}
	// os.Readlink reads any reparse point; it fails on ordinary files and
	// directories, which covers the vast majority of elements.
	target, err := os.Readlink(p)
	if err != nil {
		return "", false, nil
	}
	return target, true, nil
}

// canonicalKey builds the comparison key of a resolved display path.
func canonicalKey(display string) string {
	key := strings.ReplaceAll(display, string(filepath.Separator), "/")
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	// Only roots keep a trailing slash: "/" and a drive root like "c:/".
	for len(key) > 1 && strings.HasSuffix(key, "/") && !(len(key) == 3 && key[1] == ':') {
		key = key[:len(key)-1]
	}
	return key
}

// splitKey splits a Key into elements; the trailing slash of a root key such
// as "/" or "c:/" contributes a single root element.
func splitKey(key string) []string {
	return strings.Split(strings.TrimSuffix(key, "/"), "/")
}

// GitExcludePath returns the absolute path of the repository's local exclude
// file (.git/info/exclude). That file is where CoBot records its private
// ignore rules; repoDir must already be a canonical, existing directory.
func GitExcludePath(repoDir string) (string, error) {
	cmd := exec.Command("git", "-C", repoDir, "rev-parse", "--git-path", "info/exclude")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			return "", fmt.Errorf("resolve git exclude path in %s: %s: %w: %w", repoDir, detail, ErrNotARepo, err)
		}
		return "", fmt.Errorf("resolve git exclude path in %s: %w: %w", repoDir, ErrNotARepo, err)
	}
	exclude := strings.TrimSpace(string(out))
	if exclude == "" {
		return "", fmt.Errorf("resolve git exclude path in %s: %w: git returned no path", repoDir, ErrNotARepo)
	}
	if !filepath.IsAbs(exclude) {
		exclude = filepath.Join(repoDir, exclude)
	}
	if !filepath.IsAbs(exclude) {
		abs, err := filepath.Abs(exclude)
		if err != nil {
			return "", fmt.Errorf("resolve git exclude path in %s: make %q absolute: %w", repoDir, exclude, err)
		}
		exclude = abs
	}
	return exclude, nil
}

// localExcludeRule is the line CoBot keeps in the repository-local exclude
// file so its own bookkeeping stays out of the user's commits.
const localExcludeRule = ".cobot/"

// EnsureLocalCobotExclude appends the local ignore rule ".cobot/" to
// excludeFile when that line is missing, creating the file (mode 0644) and
// its parent directory when needed. Existing lines are left untouched and
// shared ignore files such as .gitignore are never modified. Repeated calls
// are idempotent.
func EnsureLocalCobotExclude(excludeFile string) error {
	if strings.TrimSpace(excludeFile) == "" {
		return errors.New("ensure local cobot exclude: empty path")
	}
	content, err := os.ReadFile(excludeFile)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("read git exclude file %s: %w", excludeFile, err)
		}
		content = nil
	}
	if hasExcludeLine(string(content), localExcludeRule) {
		return nil
	}
	if dir := filepath.Dir(excludeFile); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create git exclude directory %s: %w", dir, err)
		}
	}
	f, err := os.OpenFile(excludeFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open git exclude file %s: %w", excludeFile, err)
	}
	add := localExcludeRule + "\n"
	if len(content) > 0 && !strings.HasSuffix(string(content), "\n") {
		add = "\n" + add
	}
	if _, err := f.WriteString(add); err != nil {
		f.Close()
		return fmt.Errorf("update git exclude file %s: %w", excludeFile, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close git exclude file %s: %w", excludeFile, err)
	}
	return nil
}

// hasExcludeLine reports whether content already contains line as a whole
// line, tolerating CRLF endings and a missing trailing newline.
func hasExcludeLine(content, line string) bool {
	for _, l := range strings.Split(content, "\n") {
		if strings.TrimSuffix(l, "\r") == line {
			return true
		}
	}
	return false
}

// IsReservedDir returns the first reserved directory that equals c or
// contains it, so callers can reject binding a profile root or an
// engine/runtime directory as a team working directory. Reserved entries
// that cannot be canonicalized are skipped; the original entry is returned
// so the caller can name it.
func IsReservedDir(reserved []string, c CanonicalDir) (string, bool) {
	for _, r := range reserved {
		if strings.TrimSpace(r) == "" {
			continue
		}
		rc, err := CanonicalDirFrom(r)
		if err != nil {
			continue
		}
		if rc.Contains(c) {
			return r, true
		}
	}
	return "", false
}
