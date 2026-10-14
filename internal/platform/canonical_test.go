package platform

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func mustCanonical(t *testing.T, path string) CanonicalDir {
	t.Helper()
	c, err := CanonicalDirFrom(path)
	if err != nil {
		t.Fatalf("CanonicalDirFrom(%q): %v", path, err)
	}
	return c
}

func TestCanonicalDirComponentWiseContainment(t *testing.T) {
	base := t.TempDir()
	dirAB := filepath.Join(base, "a", "b")
	dirABC := filepath.Join(dirAB, "c")
	dirABc := filepath.Join(base, "a", "bc")
	for _, d := range []string{dirAB, dirABC, dirABc} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	ab := mustCanonical(t, dirAB)
	abc := mustCanonical(t, dirABC)
	sibling := mustCanonical(t, dirABc)

	if !ab.Equal(ab) {
		t.Error("a directory must equal itself")
	}
	if ab.Equal(abc) {
		t.Error("a directory must not equal its child")
	}
	if !ab.Contains(ab) {
		t.Error("/a/b must contain itself")
	}
	if !ab.Contains(abc) {
		t.Error("/a/b must contain /a/b/c")
	}
	if ab.Contains(sibling) {
		t.Errorf("/a/b must not contain sibling /a/bc, got keys %q and %q", ab.Key, sibling.Key)
	}
	if abc.Contains(ab) {
		t.Error("/a/b/c must not contain its parent /a/b")
	}
	if !sibling.Contains(sibling) {
		t.Error("/a/bc must contain itself")
	}
}

func TestCanonicalDirMissingLeaf(t *testing.T) {
	parent := t.TempDir()
	parentC := mustCanonical(t, parent)

	missing := filepath.Join(parent, "missing-leaf")
	c := mustCanonical(t, missing)
	if !strings.HasSuffix(c.Key, "/missing-leaf") {
		t.Errorf("key %q must end with the missing leaf", c.Key)
	}
	if filepath.Base(c.Display) != "missing-leaf" {
		t.Errorf("display %q must keep the missing leaf", c.Display)
	}
	if !parentC.Contains(c) {
		t.Errorf("parent %q must contain the missing leaf %q", parentC.Key, c.Key)
	}

	deeper := filepath.Join(parent, "gone", "deeper")
	d := mustCanonical(t, deeper)
	if !strings.HasSuffix(d.Key, "/gone/deeper") {
		t.Errorf("key %q must end with the missing elements", d.Key)
	}
	if !parentC.Contains(d) {
		t.Errorf("parent %q must contain %q", parentC.Key, d.Key)
	}
}

func TestCanonicalDirEmptyPathIsError(t *testing.T) {
	if _, err := CanonicalDirFrom("  "); err == nil {
		t.Fatal("expected an error for an empty path")
	}
}

func flipASCIICase(path string) string {
	i := strings.LastIndexAny(path, `/\`)
	head, elem := "", path
	if i >= 0 {
		head, elem = path[:i+1], path[i+1:]
	}
	var b strings.Builder
	for _, r := range elem {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 'a' + 'A')
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r - 'A' + 'a')
		default:
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 || b.String() == elem {
		return ""
	}
	return head + b.String()
}

func TestCanonicalDirWindowsCaseInsensitive(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("case-insensitive keys apply on Windows only")
	}
	dir := filepath.Join(t.TempDir(), "TeamCase", "WorkDir")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	flipped := flipASCIICase(dir)
	if flipped == "" || flipped == dir {
		t.Fatalf("could not flip case in %q", dir)
	}

	orig := mustCanonical(t, dir)
	other := mustCanonical(t, flipped)
	if orig.Key != other.Key {
		t.Errorf("keys must match ignoring case: %q vs %q (from %q and %q)", orig.Key, other.Key, dir, flipped)
	}
	if !orig.Equal(other) {
		t.Error("the same directory spelled in different case must be equal")
	}
	if _, err := os.Stat(orig.Display); err != nil {
		t.Errorf("display %q must be a real directory: %v", orig.Display, err)
	}
}

func TestCanonicalDirSymlinkResolvesToTarget(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("directory symlinks are not permitted on this machine: %v", err)
	}

	cTarget := mustCanonical(t, target)
	cLink := mustCanonical(t, link)
	if cLink.Key != cTarget.Key {
		t.Errorf("symlink key %q must equal target key %q", cLink.Key, cTarget.Key)
	}
	if !cLink.Equal(cTarget) {
		t.Error("a symlink must canonicalize to its target")
	}

	// A missing leaf under the symlink resolves onto the real target too.
	cSub := mustCanonical(t, filepath.Join(link, "sub"))
	if !cTarget.Contains(cSub) {
		t.Errorf("target %q must contain %q reached through the symlink", cTarget.Key, cSub.Key)
	}
}

func TestCanonicalDirJunctionResolvesToTarget(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("junctions exist on Windows only")
	}
	base := t.TempDir()
	target := filepath.Join(base, "jtarget")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "junction")
	// Junctions, unlike symlinks, can be created without special privileges.
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("could not create a junction: %v (%s)", err, strings.TrimSpace(string(out)))
	}

	cTarget := mustCanonical(t, target)
	cLink := mustCanonical(t, link)
	if cLink.Key != cTarget.Key {
		t.Errorf("junction key %q must equal target key %q", cLink.Key, cTarget.Key)
	}
}

func TestCanonicalDirMissingParentIsError(t *testing.T) {
	t.Run("dangling symlink ancestor", func(t *testing.T) {
		base := t.TempDir()
		target := filepath.Join(base, "gone-target")
		link := filepath.Join(base, "dangling")
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("directory symlinks are not permitted on this machine: %v", err)
		}
		_, err := CanonicalDirFrom(filepath.Join(link, "child"))
		if err == nil {
			t.Fatal("expected an error for a path under a dangling symlink")
		}
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("error must wrap a not-exist condition, got: %v", err)
		}
	})

	if runtime.GOOS != "windows" {
		t.Skip("on unix every path already lives under the existing root")
	}
	t.Run("absent drive", func(t *testing.T) {
		drive := ""
		for r := 'z'; r >= 'b'; r-- {
			if _, err := os.Lstat(string(r) + `:\`); err != nil {
				drive = string(r) + `:\`
				break
			}
		}
		if drive == "" {
			t.Skip("no unused drive letter found")
		}
		if _, err := CanonicalDirFrom(filepath.Join(drive, "missing-parent", "leaf")); err == nil {
			t.Fatal("expected an error for a path on an absent drive")
		}
	})
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
}

func TestGitExcludePath(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v (%s)", err, strings.TrimSpace(string(out)))
	}

	exclude, err := GitExcludePath(dir)
	if err != nil {
		t.Fatalf("GitExcludePath: %v", err)
	}
	if !filepath.IsAbs(exclude) {
		t.Errorf("exclude path %q must be absolute", exclude)
	}
	if !strings.HasSuffix(filepath.ToSlash(exclude), "/info/exclude") {
		t.Errorf("exclude path %q must end with info/exclude", exclude)
	}
}

func TestGitExcludePathNotARepo(t *testing.T) {
	requireGit(t)
	_, err := GitExcludePath(t.TempDir())
	if err == nil {
		t.Fatal("expected an error for a directory that is not a repository")
	}
	if !errors.Is(err, ErrNotARepo) {
		t.Errorf("error must wrap ErrNotARepo, got: %v", err)
	}
}

func TestEnsureLocalCobotExclude(t *testing.T) {
	t.Run("creates missing file and parent", func(t *testing.T) {
		exclude := filepath.Join(t.TempDir(), "info", "exclude")
		if err := EnsureLocalCobotExclude(exclude); err != nil {
			t.Fatal(err)
		}
		if err := EnsureLocalCobotExclude(exclude); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(exclude)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Count(string(data), ".cobot/"); got != 1 {
			t.Errorf("file must contain exactly one .cobot/ line, got %d in %q", got, data)
		}
	})

	t.Run("preserves existing lines and is idempotent", func(t *testing.T) {
		exclude := filepath.Join(t.TempDir(), "exclude")
		original := "# keep this line\nexisting.log" // note: no trailing newline
		if err := os.WriteFile(exclude, []byte(original), 0o644); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			if err := EnsureLocalCobotExclude(exclude); err != nil {
				t.Fatal(err)
			}
		}
		data, err := os.ReadFile(exclude)
		if err != nil {
			t.Fatal(err)
		}
		content := string(data)
		if got := strings.Count(content, ".cobot/"); got != 1 {
			t.Errorf("file must contain exactly one .cobot/ line, got %d in %q", got, content)
		}
		if !strings.HasPrefix(content, original+"\n") {
			t.Errorf("existing lines must be preserved, got %q", content)
		}
	})

	t.Run("keeps a rule already present mid-file", func(t *testing.T) {
		exclude := filepath.Join(t.TempDir(), "exclude")
		original := "first.log\n.cobot/\nlast.log"
		if err := os.WriteFile(exclude, []byte(original), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := EnsureLocalCobotExclude(exclude); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(exclude)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != original {
			t.Errorf("file must stay untouched, got %q", data)
		}
	})
}

func TestIsReservedDir(t *testing.T) {
	base := t.TempDir()
	profileRoot := filepath.Join(base, "profile")
	nested := filepath.Join(profileRoot, "engines", "codex")
	sibling := filepath.Join(base, "profile-sibling")
	for _, d := range []string{nested, sibling} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	reserved := []string{filepath.Join(base, "unrelated"), profileRoot}

	rootC := mustCanonical(t, profileRoot)
	if got, ok := IsReservedDir(reserved, rootC); !ok || got != profileRoot {
		t.Errorf("the profile root itself is reserved, got (%q, %v)", got, ok)
	}

	nestedC := mustCanonical(t, nested)
	if got, ok := IsReservedDir(reserved, nestedC); !ok || got != profileRoot {
		t.Errorf("a team dir inside the profile root is reserved, got (%q, %v)", got, ok)
	}

	siblingC := mustCanonical(t, sibling)
	if got, ok := IsReservedDir(reserved, siblingC); ok {
		t.Errorf("a sibling of the profile root is allowed, got (%q, %v)", got, ok)
	}
}
