package codex

import (
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Desktop identity and bundle boundaries keep old standalone installations
// from silently taking the place of the desktop app's engine. The exact
// package identity OpenAI.Codex is store-assigned and sufficient on its own;
// its publisher subject has been seen as a bare GUID, so a publisher match
// is only required when the name alone is loose (a ChatGPT-branded package).
func isDesktopEngine(path string) bool {
	if !regularFile(path) {
		return false
	}
	for root := filepath.Dir(path); filepath.Dir(root) != root; root = filepath.Dir(root) {
		if desktopMacBundle(root) {
			for _, relative := range macEnginePaths() {
				if path == filepath.Join(root, relative) {
					return true
				}
			}
		}
		if strings.EqualFold(filepath.Base(path), "codex.exe") && desktopWindowsBundle(root) {
			return true
		}
	}
	return false
}

func macEnginePaths() []string {
	return []string{
		filepath.Join("Contents", "Resources", "codex-cli", "CodexCLI.app", "Contents", "MacOS", "codex"),
		filepath.Join("Contents", "Resources", "codex-cli", "bin", "codex"),
	}
}

func desktopMacBundle(root string) bool {
	if !strings.HasSuffix(strings.ToLower(root), ".app") {
		return false
	}
	f, err := os.Open(filepath.Join(root, "Contents", "Info.plist"))
	if err != nil {
		return false
	}
	defer f.Close()
	decoder := xml.NewDecoder(f)
	wanted := false
	for {
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		element, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if element.Name.Local == "key" {
			var key string
			if decoder.DecodeElement(&key, &element) != nil {
				return false
			}
			wanted = key == "CFBundleIdentifier"
		} else if wanted && element.Name.Local == "string" {
			var value string
			if decoder.DecodeElement(&value, &element) != nil {
				return false
			}
			return value == "com.openai.codex" || value == "com.openai.chat"
		}
	}
}

func desktopWindowsBundle(root string) bool {
	f, err := os.Open(filepath.Join(root, "AppxManifest.xml"))
	if err != nil {
		return false
	}
	defer f.Close()
	decoder := xml.NewDecoder(f)
	for {
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		if element, ok := token.(xml.StartElement); ok && element.Name.Local == "Identity" {
			name, publisher := "", ""
			for _, attr := range element.Attr {
				switch attr.Name.Local {
				case "Name":
					name = strings.ToLower(attr.Value)
				case "Publisher":
					publisher = strings.ToLower(attr.Value)
				}
			}
			return name == "openai.codex" || (strings.Contains(name, "chatgpt") && strings.Contains(publisher, "openai"))
		}
	}
}

func findDesktopEngine(roots []string) (string, error) {
	for _, root := range roots {
		if root == "" {
			continue
		}
		if desktopMacBundle(root) {
			for _, relative := range macEnginePaths() {
				candidate := filepath.Join(root, relative)
				if resolved, err := filepath.EvalSymlinks(candidate); err == nil && isDesktopEngine(resolved) {
					return resolved, nil
				}
			}
		}
		if !desktopWindowsBundle(root) {
			continue
		}
		// Store metadata supplies the real root; never guess WindowsApps paths.
		var found string
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if !entry.IsDir() && strings.EqualFold(entry.Name(), "codex.exe") && regularFile(path) {
				if found != "" {
					return errors.New("multiple desktop Codex engines; specify the bundled engine path")
				}
				found = path
			}
			return nil
		})
		if err != nil {
			return "", err
		}
		if found != "" {
			if resolved, err := filepath.EvalSymlinks(found); err == nil && isDesktopEngine(resolved) {
				return resolved, nil
			}
		}
	}
	return "", os.ErrNotExist
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
