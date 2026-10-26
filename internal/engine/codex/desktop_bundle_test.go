package codex

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeDesktopFixture(t *testing.T, path, contents string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0755); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func macDesktopFixture(t *testing.T, id string) (string, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "ChatGPT.app")
	writeDesktopFixture(t, filepath.Join(root, "Contents", "Info.plist"), `<plist><dict><key>CFBundleIdentifier</key><string>`+id+`</string></dict></plist>`)
	binary := writeDesktopFixture(t, filepath.Join(root, macEnginePaths()[0]), "desktop engine")
	return root, binary
}

func TestDesktopDiscoveryUsesBundleIdentityAndPrefersNativeEngine(t *testing.T) {
	root, binary := macDesktopFixture(t, "com.openai.codex")
	writeDesktopFixture(t, filepath.Join(root, macEnginePaths()[1]), "desktop launcher")
	got, err := findDesktopEngine([]string{filepath.Join(t.TempDir(), "missing"), root})
	if err != nil || got != binary {
		t.Fatalf("got %q, %v; want %q", got, err, binary)
	}
	if configured, err := Locate(binary); err != nil || configured != binary {
		t.Fatalf("explicit desktop engine: %q, %v", configured, err)
	}
	unrelated, other := macDesktopFixture(t, "example.unrelated")
	if isDesktopEngine(other) {
		t.Fatal("unrelated app was accepted as the desktop engine")
	}
	if _, err := findDesktopEngine([]string{unrelated}); err == nil {
		t.Fatal("unrelated app was auto-detected")
	}
}

func TestStandaloneEngineIsRejectedEvenWhenExplicitOrOnPATH(t *testing.T) {
	binary := writeDesktopFixture(t, filepath.Join(t.TempDir(), "codex"), "standalone engine")
	if _, err := Locate(binary); !errors.Is(err, ErrNotFound) {
		t.Fatalf("explicit standalone engine accepted: %v", err)
	}
	t.Setenv("PATH", filepath.Dir(binary))
	got, err := Locate("")
	if err == nil && (!isDesktopEngine(got) || got == binary) {
		t.Fatalf("PATH standalone selected: %q", got)
	}
}

func TestDesktopEngineCannotEscapeBundleThroughASymlink(t *testing.T) {
	root, binary := macDesktopFixture(t, "com.openai.codex")
	standalone := writeDesktopFixture(t, filepath.Join(t.TempDir(), "codex"), "standalone")
	if err := os.Remove(binary); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(standalone, binary); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	if _, err := Locate(binary); !errors.Is(err, ErrNotFound) {
		t.Fatalf("explicit symlink escaped the bundle: %v", err)
	}
	if _, err := findDesktopEngine([]string{root}); err == nil {
		t.Fatal("auto-discovered symlink escaped the bundle")
	}
}

func TestWindowsDesktopPackageDiscoveryUsesManifestAndRejectsAmbiguity(t *testing.T) {
	root := t.TempDir()
	manifest := filepath.Join(root, "AppxManifest.xml")
	writeDesktopFixture(t, manifest, `<Package><Identity Name="OpenAI.Codex" Publisher="CN=OpenAI" /></Package>`)
	binary := writeDesktopFixture(t, filepath.Join(root, "app", "resources", "codex.exe"), "desktop engine")
	got, err := findDesktopEngine([]string{root})
	if err != nil || got != binary || !isDesktopEngine(binary) {
		t.Fatalf("Windows desktop package: %q, %v", got, err)
	}
	second := writeDesktopFixture(t, filepath.Join(root, "other", "codex.exe"), "other architecture")
	if _, err := findDesktopEngine([]string{root}); err == nil {
		t.Fatal("ambiguous Windows package silently picked an engine")
	}
	if err := os.Remove(second); err != nil {
		t.Fatal(err)
	}
	writeDesktopFixture(t, manifest, `<Package><Identity Name="OpenAI.Codex" Publisher="CN=Other" /></Package>`)
	if isDesktopEngine(binary) {
		t.Fatal("unrelated publisher accepted")
	}
	if _, err := findDesktopEngine([]string{root}); err == nil {
		t.Fatal("unrelated package auto-detected")
	}
}
