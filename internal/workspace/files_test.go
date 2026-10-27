package workspace

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func fixture(t *testing.T, root, name, content string) string {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLiteralWhitespaceFilenameWinsOverPastedPathCleanup(t *testing.T) {
	root := t.TempDir()
	fixture(t, root, "note.txt", "trimmed file")
	names := []string{" note.txt", "\u00a0note.txt"}
	if runtime.GOOS != "windows" {
		names = append(names, "note.txt ")
	}
	for _, name := range names {
		absolute := fixture(t, root, name, "literal file")
		for _, target := range []string{name, name + ":3:2", (&url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}).String()} {
			file, err := Open(root, target)
			if err != nil {
				t.Fatal(target, err)
			}
			data, err := io.ReadAll(file.Handle)
			file.Close()
			if err != nil || file.Info.Path != name || string(data) != "literal file" {
				t.Fatalf("%q: %+v %q %v", target, file.Info, data, err)
			}
		}
	}
	file, err := Open(root, " note.txt   ")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if file.Info.Path != "note.txt" {
		t.Fatal("nonexistent pasted path should still be cleaned", file.Info.Path)
	}
}

func TestOpenPositionsAndCanonicalPaths(t *testing.T) {
	root := t.TempDir()
	path := fixture(t, root, "src/hello world.go", "package hello\n")
	for _, target := range []string{"src/hello world.go", path, "src/hello%20world.go:12:3", "file://" + filepath.ToSlash(path) + "#L4-L7"} {
		f, err := Open(root, target)
		if err != nil {
			t.Fatal(target, err)
		}
		if f.Info.Path != "src/hello world.go" || f.Info.Kind != "text" {
			t.Fatalf("%s: %+v", target, f.Info)
		}
		if target == "src/hello%20world.go:12:3" && (f.Info.Line != 12 || f.Info.Column != 3) {
			t.Fatal(f.Info)
		}
		if target[len(target)-3:] == "-L7" && (f.Info.Line != 4 || f.Info.EndLine != 7) {
			t.Fatal(f.Info)
		}
		data, err := io.ReadAll(f.Handle)
		f.Close()
		if err != nil || string(data) != "package hello\n" {
			t.Fatalf("read: %q %v", data, err)
		}
	}
	if runtime.GOOS == "windows" {
		return
	}
	fixture(t, root, "report:12", "real filename")
	f, err := Open(root, "report:12")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if f.Info.Path != "report:12" || f.Info.Line != 0 {
		t.Fatal(f.Info)
	}
}

func TestOpenRejectsTraversalAndSymlinkEscape(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	os.Mkdir(root, 0700)
	outside := fixture(t, parent, "private.txt", "private")
	inside := fixture(t, root, "public.txt", "public")
	for _, target := range []string{"../private.txt", outside, "file://remote/private.txt", "//remote/path", "%00file"} {
		f, err := Open(root, target)
		if f != nil {
			f.Close()
		}
		if err == nil {
			t.Fatalf("opened %q", target)
		}
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skip("symlinks unavailable")
	}
	if f, err := Open(root, "escape"); !errors.Is(err, ErrOutside) {
		if f != nil {
			f.Close()
		}
		t.Fatalf("escape: %v", err)
	}
	if err := os.Symlink(inside, filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	f, err := Open(root, "alias")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if f.Info.Path != "public.txt" {
		t.Fatalf("alias not canonical: %+v", f.Info)
	}
	d, err := List(context.Background(), root, "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	foundAlias := false
	for _, entry := range d.Entries {
		if entry.Name == "alias" {
			foundAlias = true
			if entry.Path != "alias" {
				t.Fatal(entry)
			}
		}
		if entry.Name == "escape" && entry.Kind != "blocked" {
			t.Fatal(entry)
		}
	}
	if !foundAlias {
		t.Fatal("directory entry lost symlink name")
	}
}

func TestOpenedDescriptorSurvivesPathReplacement(t *testing.T) {
	root := t.TempDir()
	name := fixture(t, root, "read.txt", "original")
	f, err := Open(root, "read.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := os.Rename(name, name+".old"); err != nil {
		t.Skip("open file cannot be renamed")
	}
	fixture(t, root, "read.txt", "replacement")
	data, err := io.ReadAll(f.Handle)
	if err != nil || string(data) != "original" {
		t.Fatalf("%q %v", data, err)
	}
}

func TestDirectoryPaginationAndHiddenFiles(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "z-directory"), 0700)
	fixture(t, root, ".hidden", "hidden")
	for i := 0; i < PageSize+10; i++ {
		fixture(t, root, fmt.Sprintf("file-%03d.txt", i), "text")
	}
	a, err := List(context.Background(), root, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Entries) != PageSize || a.NextCursor == "" || a.Entries[0].Kind != "directory" {
		t.Fatal(a)
	}
	b, err := List(context.Background(), root, "", a.NextCursor, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Entries) != 11 || b.NextCursor != "" {
		t.Fatal(b)
	}
	seen := map[string]bool{}
	for _, page := range []Directory{a, b} {
		for _, f := range page.Entries {
			if seen[f.Path] || f.Name == ".hidden" {
				t.Fatal(f)
			}
			seen[f.Path] = true
		}
	}
	c, err := List(context.Background(), root, "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if c.Entries[1].Name != ".hidden" {
		t.Fatal(c.Entries[:2])
	}
	if _, err := List(context.Background(), root, "", "!not-base64", false); err == nil {
		t.Fatal("invalid cursor accepted")
	}
	if _, err := List(context.Background(), root, "", base64.RawURLEncoding.EncodeToString([]byte("1file-209.txt")), false); err != nil {
		t.Fatal(err)
	}
}

func TestClassificationAndSize(t *testing.T) {
	root := t.TempDir()
	for name, want := range map[string]string{"README": "text", "script.sh": "text", "file.unknown": "text", "a.md": "markdown", "a.svg": "svg", "a.docx": "document", "a.xlsm": "spreadsheet", "a.pptx": "presentation", "a.ipynb": "notebook", "a.tex": "latex", "a.pdf": "pdf", "a.tiff": "binary", "a.mp4": "video", "a.opus": "audio"} {
		fixture(t, root, name, "sample")
		f, err := Open(root, name)
		if err != nil {
			t.Fatal(err)
		}
		f.Close()
		if f.Info.Kind != want {
			t.Fatalf("%s: %s", name, f.Info.Kind)
		}
	}
	fixture(t, root, "data", "abc\x00def")
	f, err := Open(root, "data")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if f.Info.Kind != "binary" {
		t.Fatal(f.Info)
	}
	if got := kind("utf8", []byte("汉字\xe6")); got != "text" {
		t.Fatal(got)
	}
}

func TestInvalidUTF8AndCanceledListing(t *testing.T) {
	for _, sample := range [][]byte{{0xff}, {'a', 0xff}, {0xc0, 0xaf}} {
		if got := kind("unknown", sample); got != "binary" {
			t.Fatalf("invalid UTF-8 %x: %s", sample, got)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := List(ctx, t.TempDir(), "", "", false); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if f, err := Open("", ""); err == nil {
		f.Close()
		t.Fatal("empty workspace opened process directory")
	}
}

func TestLiteralPercentFilenameWinsOverDecoding(t *testing.T) {
	root := t.TempDir()
	fixture(t, root, "name%20literal.txt", "literal percent")
	fixture(t, root, "name literal.txt", "decoded name")
	f, err := Open(root, "name%20literal.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if f.Info.Path != "name%20literal.txt" {
		t.Fatal(f.Info)
	}
}
