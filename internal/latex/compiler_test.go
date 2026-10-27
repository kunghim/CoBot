package latex

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCompilerCancellationDoesNotStartAProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Compile(ctx, Config{}, "", ""); err != context.Canceled {
		t.Fatal(err)
	}
}
func TestCompilerLogIsBoundedWithoutBlockingWriters(t *testing.T) {
	log := &boundedLog{}
	n, err := log.Write([]byte(strings.Repeat("x", maxLog+100)))
	data, truncated := log.snapshot()
	if err != nil || n != maxLog+100 || len(data) != maxLog || !truncated {
		t.Fatalf("%d %d %v %v", n, len(data), truncated, err)
	}
}
func TestCompilerEnvironmentDoesNotCarryProviderSecrets(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "synthetic-secret")
	t.Setenv("TECTONIC_CACHE_DIR", "other-cache")
	text := strings.Join(compilerEnv("selected-cache", "output-temp"), "\n")
	if strings.Contains(text, "synthetic-secret") || strings.Contains(text, "other-cache") || !strings.Contains(text, "TECTONIC_UNTRUSTED_MODE=1") {
		t.Fatal("unexpected compiler environment")
	}
}

func TestCompilerOutputBudgetChecksSizeWithoutReadingFileData(t *testing.T) {
	root := t.TempDir()
	file, err := os.Create(filepath.Join(root, "large.xdv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(65 << 20); err != nil {
		file.Close()
		t.Fatal(err)
	}
	file.Close()
	if err := checkOutput(root); err != ErrOutputLimit {
		t.Fatal(err)
	}
}

func TestCompilerRuntimeReadsDoNotIncludeTheMacOSDataVolumeOrCompilerParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SBPL paths use macOS syntax")
	}
	profile, err := sandboxProfile("/Users/owner/tectonic", "/cache", "/project", "/output")
	if err != nil {
		t.Fatal(err)
	}
	for _, broad := range []string{`(subpath "/System")`, `(subpath "/Users/owner")`, `(allow file-read-metadata)`} {
		if strings.Contains(profile, broad) {
			t.Fatal("overbroad runtime boundary", broad)
		}
	}
	if !strings.Contains(profile, `(literal "/Users/owner/tectonic")`) {
		t.Fatal("compiler executable not readable")
	}
}
