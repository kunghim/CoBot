package browser

import "golang.org/x/sys/windows/registry"

// defaultBrowserProgID is the program the user chose for https links.
func defaultBrowserProgID() string {
	key, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\Shell\Associations\UrlAssociations\https\UserChoice`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer key.Close()
	prog, _, err := key.GetStringValue("ProgId")
	if err != nil {
		return ""
	}
	return prog
}
