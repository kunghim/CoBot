//go:build !windows

package browser

// defaultBrowserProgID has no equivalent here: the search order alone picks.
func defaultBrowserProgID() string { return "" }
