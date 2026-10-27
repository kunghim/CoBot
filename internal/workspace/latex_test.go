package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestLatexDiscoveryRequiresAnExplicitChoiceForAmbiguousProjects(t *testing.T) {
	root := t.TempDir()
	fixture(t, root, "chapter.tex", "Chapter text")
	fixture(t, root, "one.tex", "\\documentclass{article}\n")
	fixture(t, root, "two.tex", "\\documentclass[12pt]{book}\n")
	fixture(t, root, "comment.tex", "% \\documentclass{article}\n")
	result, err := DiscoverLatex(context.Background(), root, "chapter.tex")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 2 || result.Suggested != "" || result.Truncated {
		t.Fatalf("%+v", result)
	}
	selected, err := DiscoverLatex(context.Background(), root, "two.tex")
	if err != nil || selected.Suggested != "two.tex" {
		t.Fatalf("%+v %v", selected, err)
	}
}
func TestLatexRootDirectivesResolveInsideTheWorkspaceOnly(t *testing.T) {
	root := t.TempDir()
	main := fixture(t, root, "main file.tex", "\\documentclass{article}\n")
	fixture(t, root, "chapters/part.tex", "% !TeX root = \"../main file.tex\"\nChapter")
	result, err := DiscoverLatex(context.Background(), root, "chapters/part.tex")
	if err != nil || result.Suggested != "main file.tex" {
		t.Fatalf("%+v %v", result, err)
	}
	fixture(t, root, "chapters/part.tex", "% !TEX root = "+main+"\n")
	result, err = DiscoverLatex(context.Background(), root, "chapters/part.tex")
	if err != nil || result.Suggested != "main file.tex" {
		t.Fatalf("%+v %v", result, err)
	}
	outside := fixture(t, t.TempDir(), "private.tex", "outside")
	fixture(t, root, "chapters/part.tex", "% !TEX root = "+outside+"\n")
	if _, err := DiscoverLatex(context.Background(), root, "chapters/part.tex"); !errors.Is(err, ErrOutside) {
		t.Fatal(err)
	}
}
func TestLatexDiscoveryBoundsScanningAndCancellation(t *testing.T) {
	root := t.TempDir()
	fixture(t, root, "selected.tex", "Chapter")
	for index := 0; index < 2001; index++ {
		fixture(t, root, fmt.Sprintf("file-%04d.txt", index), "")
	}
	result, err := DiscoverLatex(context.Background(), root, "selected.tex")
	if err != nil || !result.Truncated {
		t.Fatalf("%+v %v", result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := DiscoverLatex(ctx, root, "selected.tex"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestLatexDiscoveryDoesNotRecurseThroughAncestorAliases(t *testing.T) {
	root := t.TempDir()
	fixture(t, root, "part.tex", "Chapter")
	fixture(t, root, "nested/main.tex", "\\documentclass{article}")
	if err := os.Symlink(root, filepath.Join(root, "nested", "up")); err != nil {
		t.Skip("symlink unavailable")
	}
	result, err := DiscoverLatex(context.Background(), root, "part.tex")
	if err != nil || len(result.Candidates) != 1 || result.Suggested != "nested/main.tex" || result.Truncated {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestLatexDiscoveryDoesNotRejectATruncatedUTF8Character(t *testing.T) {
	data := append([]byte("\\documentclass{article}\n"), []byte("界")[:2]...)
	prefix, err := latexUTF8Prefix(data, true)
	if err != nil || !latexDocument(string(prefix)) {
		t.Fatalf("%q %v", prefix, err)
	}
	if _, err := latexUTF8Prefix(data, false); err == nil {
		t.Fatal("invalid UTF-8 accepted as a complete source")
	}
}

func TestLatexDiscoveryDoesNotTreatAnIncompleteScanAsAnUnambiguousMain(t *testing.T) {
	root := t.TempDir()
	fixture(t, root, "part.tex", "Chapter")
	fixture(t, root, "main.tex", `\documentclass{article}`)
	fixture(t, root, "a/b/c/d/e/f/g/h/i/hidden-main.tex", `\documentclass{article}`)
	result, err := DiscoverLatex(context.Background(), root, "part.tex")
	if err != nil || !result.Truncated || len(result.Candidates) != 1 || result.Suggested != "" {
		t.Fatalf("incomplete discovery: %+v %v", result, err)
	}
}
