package browser

import (
	"errors"
	"os/exec"
	"path/filepath"
	"testing"
)

func fakeMachine(goos string, env map[string]string, present ...string) finder {
	have := map[string]bool{}
	for _, p := range present {
		have[p] = true
	}
	return finder{
		goos:   goos,
		env:    func(k string) string { return env[k] },
		exists: func(p string) bool { return have[p] },
		lookPath: func(name string) (string, error) {
			if have[name] {
				return "/usr/bin/" + name, nil
			}
			return "", exec.ErrNotFound
		},
		defaultProg: func() string { return env["defaultProg"] },
	}
}

func TestFindPrefersTheDefaultChromiumBrowserThenChrome(t *testing.T) {
	env := map[string]string{"ProgramFiles": `C:\PF`, "ProgramFiles(x86)": `C:\PF86`, "LocalAppData": `C:\Local`}
	chrome := filepath.Join(`C:\PF`, `Google\Chrome\Application\chrome.exe`)
	edge := filepath.Join(`C:\PF86`, `Microsoft\Edge\Application\msedge.exe`)

	got, err := fakeMachine("windows", env, chrome, edge).find()
	if err != nil || got.Name != "Google Chrome" || got.Path != chrome {
		t.Fatalf("both installed, no default = %+v, %v", got, err)
	}
	// The user's own choice wins when it is a supported browser.
	env["defaultProg"] = "MSEdgeHTM"
	got, err = fakeMachine("windows", env, chrome, edge).find()
	if err != nil || got.Name != "Microsoft Edge" {
		t.Fatalf("Edge as default = %+v, %v", got, err)
	}
	// A default that is not installed or not Chromium-based falls back.
	env["defaultProg"] = "FirefoxURL-308046B0AF4A39CB"
	got, _ = fakeMachine("windows", env, edge).find()
	if got.Name != "Microsoft Edge" {
		t.Fatalf("Firefox as default, only Edge installed = %+v", got)
	}
}

func TestFindOnMacAndLinux(t *testing.T) {
	brave := "/Applications/Brave Browser.app/Contents/MacOS/Brave Browser"
	got, err := fakeMachine("darwin", map[string]string{"HOME": "/Users/x"}, brave).find()
	if err != nil || got.Name != "Brave" || got.Path != brave {
		t.Fatalf("mac = %+v, %v", got, err)
	}
	userChrome := "/Users/x/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	if got, _ = fakeMachine("darwin", map[string]string{"HOME": "/Users/x"}, brave, userChrome).find(); got.Name != "Google Chrome" {
		t.Fatalf("per-user Chrome should come before Brave: %+v", got)
	}
	got, err = fakeMachine("linux", nil, "chromium-browser", "microsoft-edge").find()
	if err != nil || got.Name != "Microsoft Edge" || got.Path != "/usr/bin/microsoft-edge" {
		t.Fatalf("linux = %+v, %v", got, err)
	}
}

func TestFindHonoursTheOverrideAndReportsNone(t *testing.T) {
	override := `D:\portable\chrome.exe`
	got, err := fakeMachine("windows", map[string]string{EnvBrowser: override}, override).find()
	if err != nil || got.Path != override {
		t.Fatalf("override = %+v, %v", got, err)
	}
	// An override pointing nowhere is an error, not a silent fallback: the
	// person who set it wants that browser.
	if _, err := fakeMachine("windows", map[string]string{EnvBrowser: override}).find(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing override = %v", err)
	}
	if _, err := fakeMachine("linux", nil).find(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty machine = %v", err)
	}
}
