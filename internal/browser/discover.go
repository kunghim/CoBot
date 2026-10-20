package browser

import (
	"errors"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// Installed is a Chromium-based browser found on this machine. CoBot never
// downloads a browser: it runs one the user already has, in its own profile.
type Installed struct {
	Name string
	Path string
}

// ErrNotFound means no supported browser is installed.
var ErrNotFound = errors.New("no Chromium-based browser (Chrome, Edge, Brave, Chromium, Vivaldi) found")

// EnvBrowser names a browser executable to use instead of searching, for
// people with a portable install and for tests.
const EnvBrowser = "COBOT_BROWSER"

// candidate is one place a browser may live; ids tie it to the default-
// browser registration so the user's own choice wins when it is supported.
type candidate struct {
	name  string
	progs []string // Windows URL-association ProgIds
	paths []string // Windows and macOS install locations
	cmds  []string // Linux command names
}

// finder holds everything the search reads, so tests can supply a fake machine.
type finder struct {
	goos        string
	env         func(string) string
	exists      func(string) bool
	lookPath    func(string) (string, error)
	defaultProg func() string // the Windows https handler's ProgId, "" if unknown
}

func systemFinder() finder {
	return finder{
		goos: runtime.GOOS,
		env:  os.Getenv,
		exists: func(path string) bool {
			info, err := os.Stat(path)
			return err == nil && !info.IsDir()
		},
		lookPath:    exec.LookPath,
		defaultProg: defaultBrowserProgID,
	}
}

// Find locates a browser: the override from COBOT_BROWSER, else the user's
// default browser when it is Chromium-based, else Chrome, Edge, Brave,
// Chromium and Vivaldi in that order.
func Find() (Installed, error) { return systemFinder().find() }

func (f finder) find() (Installed, error) {
	if override := strings.TrimSpace(f.env(EnvBrowser)); override != "" {
		if !f.exists(override) {
			return Installed{}, ErrNotFound
		}
		return Installed{Name: filepath.Base(override), Path: override}, nil
	}
	candidates := f.candidates()
	if prog := f.defaultProg(); prog != "" {
		for _, c := range candidates {
			for _, p := range c.progs {
				if strings.EqualFold(p, prog) {
					if found, ok := f.locate(c); ok {
						return found, nil
					}
				}
			}
		}
	}
	for _, c := range candidates {
		if found, ok := f.locate(c); ok {
			return found, nil
		}
	}
	return Installed{}, ErrNotFound
}

func (f finder) locate(c candidate) (Installed, bool) {
	if f.goos == "linux" {
		for _, command := range c.cmds {
			if path, err := f.lookPath(command); err == nil {
				return Installed{Name: c.name, Path: path}, true
			}
		}
		return Installed{}, false
	}
	for _, path := range c.paths {
		if f.exists(path) {
			return Installed{Name: c.name, Path: path}, true
		}
	}
	return Installed{}, false
}

func (f finder) candidates() []candidate {
	switch f.goos {
	case "windows":
		pf, pf86, local := f.env("ProgramFiles"), f.env("ProgramFiles(x86)"), f.env("LocalAppData")
		in := func(rest string, roots ...string) []string {
			var out []string
			for _, root := range roots {
				if root != "" {
					out = append(out, filepath.Join(root, rest))
				}
			}
			return out
		}
		return []candidate{
			{name: "Google Chrome", progs: []string{"ChromeHTML"}, paths: in(`Google\Chrome\Application\chrome.exe`, pf, pf86, local)},
			{name: "Microsoft Edge", progs: []string{"MSEdgeHTM"}, paths: in(`Microsoft\Edge\Application\msedge.exe`, pf86, pf)},
			{name: "Brave", progs: []string{"BraveHTML"}, paths: in(`BraveSoftware\Brave-Browser\Application\brave.exe`, pf, pf86, local)},
			{name: "Chromium", progs: []string{"ChromiumHTM"}, paths: in(`Chromium\Application\chrome.exe`, local)},
			{name: "Vivaldi", progs: []string{"VivaldiHTM"}, paths: in(`Vivaldi\Application\vivaldi.exe`, local, pf)},
		}
	case "darwin":
		home := f.env("HOME")
		app := func(bundle, binary string) []string {
			paths := []string{"/Applications/" + bundle + "/Contents/MacOS/" + binary}
			if home != "" {
				paths = append(paths, path.Join(home, "Applications", bundle, "Contents", "MacOS", binary))
			}
			return paths
		}
		return []candidate{
			{name: "Google Chrome", paths: app("Google Chrome.app", "Google Chrome")},
			{name: "Microsoft Edge", paths: app("Microsoft Edge.app", "Microsoft Edge")},
			{name: "Brave", paths: app("Brave Browser.app", "Brave Browser")},
			{name: "Chromium", paths: app("Chromium.app", "Chromium")},
			{name: "Vivaldi", paths: app("Vivaldi.app", "Vivaldi")},
		}
	default:
		return []candidate{
			{name: "Google Chrome", cmds: []string{"google-chrome", "google-chrome-stable"}},
			{name: "Microsoft Edge", cmds: []string{"microsoft-edge", "microsoft-edge-stable"}},
			{name: "Brave", cmds: []string{"brave-browser", "brave"}},
			{name: "Chromium", cmds: []string{"chromium", "chromium-browser"}},
			{name: "Vivaldi", cmds: []string{"vivaldi", "vivaldi-stable"}},
		}
	}
}
