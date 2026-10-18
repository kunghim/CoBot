//go:build windows

package codex

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNpmLauncherUsesInstalledNativeDependency(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "node_modules", "@openai", "codex")
	native := filepath.Join(root, "node_modules", "@openai", "codex-win32-x64", "vendor", "x86_64-pc-windows-msvc", "bin", "codex.exe")
	writeLauncherFixture(t, filepath.Join(pkg, "package.json"), `{"name":"@openai/codex"}`)
	writeLauncherFixture(t, native, "test executable")
	for _, launcher := range []string{
		filepath.Join(root, "codex.cmd"),
		filepath.Join(root, "node_modules", ".bin", "codex"),
		filepath.Join(pkg, "bin", "codex.js"),
	} {
		if got := npmNativeBinary(launcher, "amd64"); got != native {
			t.Fatalf("launcher %q resolved to %q, want %q", launcher, got, native)
		}
	}
	if got := npmNativeBinary(filepath.Join(root, "codex.cmd"), "arm64"); got != "" {
		t.Fatalf("used the wrong architecture: %q", got)
	}
	writeLauncherFixture(t, filepath.Join(pkg, "package.json"), `{"name":"unrelated-package"}`)
	if got := npmNativeBinary(filepath.Join(root, "codex.cmd"), "amd64"); got != "" {
		t.Fatalf("unrelated package selected: %q", got)
	}
}

func TestMissingNativeDependencyLeavesLauncherUnchanged(t *testing.T) {
	launcher := filepath.Join(t.TempDir(), "codex.cmd")
	if got := resolveLauncher(launcher); got != launcher {
		t.Fatalf("unknown launcher changed to %q", got)
	}
	writeLauncherFixture(t, launcher, "explicit launcher")
	if got, err := Locate(launcher); err != nil || got != launcher {
		t.Fatalf("explicit configuration was changed: %q, %v", got, err)
	}
}

func TestNpmJunctionUsesPhysicalDependencyDirectory(t *testing.T) {
	root := t.TempDir()
	alias := filepath.Join(root, "node_modules", "@openai", "codex")
	pkg := filepath.Join(root, "store", "entry", "node_modules", "@openai", "codex")
	native := filepath.Join(filepath.Dir(pkg), "codex-win32-x64", "vendor", "x86_64-pc-windows-msvc", "bin", "codex.exe")
	writeLauncherFixture(t, filepath.Join(pkg, "package.json"), `{"name":"@openai/codex"}`)
	writeLauncherFixture(t, native, "test executable")
	if err := os.MkdirAll(filepath.Dir(alias), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("cmd.exe", "/c", "mklink", "/J", alias, pkg)
	hideChildWindow(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create package junction: %v: %s", err, out)
	}
	launcher := filepath.Join(root, "node_modules", ".bin", "codex")
	if got := npmNativeBinary(launcher, "amd64"); got != native {
		t.Fatalf("junction resolved to %q, want physical dependency %q", got, native)
	}
}

func writeLauncherFixture(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
}
