//go:build windows

package codex

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

// Package-manager launchers may allocate their own consoles despite the
// parent's CREATE_NO_WINDOW flag. Resolve only known automatic PATH entries;
// an explicit configured engine path is always left alone by Locate.
func resolveLauncher(path string) string {
	launcher := path
	if strings.EqualFold(filepath.Base(filepath.Dir(path)), "shims") {
		mise := filepath.Join(filepath.Dir(filepath.Dir(path)), "bin", "mise.exe")
		if info, err := os.Stat(mise); err != nil || info.IsDir() {
			return path
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, mise, "which", "codex")
		hideChildWindow(cmd)
		out, err := cmd.Output()
		if err != nil {
			return path
		}
		launcher = strings.TrimSpace(string(out))
		if !filepath.IsAbs(launcher) {
			return path
		}
	}
	if native := npmNativeBinary(launcher, runtime.GOARCH); native != "" {
		return native
	}
	return path
}

// Follow the official npm package's optional native dependency, including
// npm/pnpm junction layouts, without executing Node or scanning installations.
func npmNativeBinary(launcher, arch string) string {
	platform, target := "", ""
	switch arch {
	case "amd64":
		platform, target = "win32-x64", "x86_64-pc-windows-msvc"
	case "arm64":
		platform, target = "win32-arm64", "aarch64-pc-windows-msvc"
	default:
		return ""
	}
	dir := filepath.Dir(launcher)
	var packages []string
	if strings.EqualFold(filepath.Base(dir), ".bin") {
		packages = append(packages, filepath.Join(filepath.Dir(dir), "@openai", "codex"))
	}
	if strings.EqualFold(filepath.Base(launcher), "codex.js") {
		packages = append(packages, filepath.Dir(dir))
	}
	ext := strings.ToLower(filepath.Ext(launcher))
	if ext == ".cmd" || ext == ".ps1" || ext == "" {
		packages = append(packages, filepath.Join(dir, "node_modules", "@openai", "codex"))
	}
	for _, pkg := range packages {
		root, err := packageRealPath(pkg)
		if err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, "package.json"))
		if err != nil {
			continue
		}
		var manifest struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(data, &manifest) != nil || manifest.Name != "@openai/codex" {
			continue
		}
		// Node resolves dependencies from the package and its ancestors.
		for parent := root; ; parent = filepath.Dir(parent) {
			candidate := filepath.Join(parent, "node_modules", "@openai", "codex-"+platform, "vendor", target, "bin", "codex.exe")
			if regularFile(candidate) {
				return candidate
			}
			if filepath.Dir(parent) == parent {
				break
			}
		}
		candidate := filepath.Join(root, "vendor", target, "bin", "codex.exe")
		if regularFile(candidate) {
			return candidate
		}
	}
	return ""
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// npm package links on Windows can be junctions rather than symlinks. Resolve
// the opened directory handle so dependency lookup uses its physical parent.
func packageRealPath(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buffer := make([]uint16, 32768)
	n, err := windows.GetFinalPathNameByHandle(windows.Handle(f.Fd()), &buffer[0], uint32(len(buffer)), 0)
	if err != nil {
		return "", err
	}
	if n >= uint32(len(buffer)) {
		return "", os.ErrInvalid
	}
	resolved := windows.UTF16ToString(buffer[:n])
	if strings.HasPrefix(resolved, `\\?\UNC\`) {
		return `\\` + strings.TrimPrefix(resolved, `\\?\UNC\`), nil
	}
	return strings.TrimPrefix(resolved, `\\?\`), nil
}
