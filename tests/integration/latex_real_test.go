package integration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/latex"
)

func TestRealIsolatedLatexCompiler(t *testing.T) {
	if os.Getenv("COBOT_R_LATEX") != "1" {
		t.Skip("requires explicit isolated compiler resources")
	}
	if runtime.GOOS != "darwin" {
		t.Skip("macOS compiler boundary verification")
	}
	root := os.Getenv("COBOT_R_LATEX_ROOT")
	if !filepath.IsAbs(root) {
		t.Fatal("explicit absolute test root required")
	}
	project := filepath.Join(root, "project")
	output := filepath.Join(root, "temporary")
	for _, dir := range []string{project, output} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	config := latex.Config{Compiler: os.Getenv("COBOT_R_LATEX_BINARY"), Cache: os.Getenv("COBOT_R_LATEX_CACHE"), Temporary: output}
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(project, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("part.tex", "PROJECT RELATIVE FIXTURE")
	write("main.tex", "\\documentclass{article}\n\\begin{document}\n\\input{part}\n\\end{document}")
	result, err := latex.Compile(context.Background(), config, project, "main.tex")
	if err != nil || len(result.PDF) < 100 {
		t.Fatalf("compile: %v\n%s", err, result.Log)
	}
	if err := os.WriteFile(filepath.Join(root, "relative.pdf"), result.PDF, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(project, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	write("nested/main.tex", "\\documentclass{article}\n\\begin{document}\\input{nested-part}\\end{document}")
	write("nested/nested-part.tex", "NESTED MAIN RELATIVE FIXTURE")
	if nested, err := latex.Compile(context.Background(), config, project, "nested/main.tex"); err != nil || len(nested.PDF) < 100 {
		t.Fatalf("nested main relative input: %v\n%s", err, nested.Log)
	}
	canary := filepath.Join(root, "outside-canary.txt")
	if err := os.WriteFile(canary, []byte("SYNTHETIC OUTSIDE CANARY"), 0600); err != nil {
		t.Fatal(err)
	}
	write("main.tex", "\\documentclass{article}\n\\begin{document}\n\\input{"+canary+"}\n\\end{document}")
	result, err = latex.Compile(context.Background(), config, project, "main.tex")
	if err == nil || len(result.PDF) != 0 {
		t.Fatal("outside document read accepted")
	}
	// The writable data volume also has a /System alias on some macOS releases.
	// Runtime library access must not accidentally permit that alias.
	alias := filepath.Join("/System/Volumes/Data", canary)
	if original, err := os.Stat(canary); err == nil {
		if alternate, err := os.Stat(alias); err == nil && os.SameFile(original, alternate) {
			write("main.tex", "\\documentclass{article}\n\\begin{document}\n\\input{"+alias+"}\n\\end{document}")
			result, err = latex.Compile(context.Background(), config, project, "main.tex")
			if err == nil || len(result.PDF) != 0 {
				t.Fatal("outside document read through data-volume alias accepted")
			}
		}
	}
	write("main.tex", "\\documentclass{article}\n\\begin{document}\n\\newwrite\\target\\immediate\\openout\\target=part.tex\\immediate\\write\\target{CORRUPTED}\n\\end{document}")
	_, _ = latex.Compile(context.Background(), config, project, "main.tex")
	original, err := os.ReadFile(filepath.Join(project, "part.tex"))
	if err != nil || string(original) != "PROJECT RELATIVE FIXTURE" {
		t.Fatal("compilation modified project source", err)
	}
	write("main.tex", "\\documentclass{article}\n\\begin{document}\n\\loop\\iftrue\\repeat\n\\end{document}")
	config.Timeout = 200 * time.Millisecond
	if _, err := latex.Compile(context.Background(), config, project, "main.tex"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout: %v", err)
	}
	entries, err := os.ReadDir(output)
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary output retained: %v %v", entries, err)
	}
}
