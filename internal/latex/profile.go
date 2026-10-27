package latex

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

func sandboxProfile(compiler, cache, project, output string) (string, error) {
	reads := []string{"/System/Library", "/usr/lib", "/usr/share", "/System/Volumes/Preboot/Cryptexes/OS/usr/lib", "/System/Volumes/Preboot/Cryptexes/OS/System/Library", cache, project, output}
	var profile strings.Builder
	profile.WriteString("(version 1)\n(deny default)\n")
	for _, value := range append([]string{compiler}, reads...) {
		if !filepath.IsAbs(value) || strings.ContainsFunc(value, unicode.IsControl) {
			return "", ErrUnavailable
		}
	}
	fmt.Fprintf(&profile, "(allow process-exec (literal %s))\n(allow sysctl-read)\n", strconv.Quote(compiler))
	fmt.Fprintf(&profile, "(allow file-read* (literal %s))\n", strconv.Quote(compiler))
	metadata := map[string]bool{}
	for _, value := range reads {
		fmt.Fprintf(&profile, "(allow file-read* (subpath %s))\n", strconv.Quote(value))
	}
	for _, value := range append([]string{compiler}, reads...) {
		for parent := filepath.Dir(value); ; parent = filepath.Dir(parent) {
			metadata[parent] = true
			if parent == filepath.Dir(parent) {
				break
			}
		}
	}
	var parents []string
	for parent := range metadata {
		parents = append(parents, parent)
	}
	sort.Strings(parents)
	for _, parent := range parents {
		fmt.Fprintf(&profile, "(allow file-read-metadata (literal %s))\n", strconv.Quote(parent))
	}
	profile.WriteString("(allow file-read-data (literal \"/\"))\n(allow file-read* (literal \"/dev/urandom\") (literal \"/dev/random\") (literal \"/dev/null\"))\n")
	fmt.Fprintf(&profile, "(allow file-write* (subpath %s) (literal \"/dev/null\"))\n", strconv.Quote(output))
	return profile.String(), nil
}
