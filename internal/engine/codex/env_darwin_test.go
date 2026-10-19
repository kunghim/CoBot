//go:build darwin

package codex

import (
	"slices"
	"testing"
)

func TestSystemProxyInheritance(t *testing.T) {
	settings := `<dictionary> {
    ExceptionsList : <array> {
      0 : *.example.test
    }
    HTTPSEnable : 1
    HTTPSPort : 7892
    HTTPSProxy : 127.0.0.1
}`
	base := []string{"PATH=/usr/bin", "no_proxy=private.test"}
	got := withSystemProxy(base, settings)
	if !slices.Contains(got, "HTTPS_PROXY=http://127.0.0.1:7892") ||
		!slices.Contains(got, "NO_PROXY=localhost,127.0.0.1,::1,*.example.test,private.test") {
		t.Fatal(got)
	}
	if !slices.Equal(base, []string{"PATH=/usr/bin", "no_proxy=private.test"}) {
		t.Fatal("mutated the parent environment")
	}
	for _, entry := range []string{"HTTPS_PROXY=http://explicit.test:8888", "http_proxy="} {
		explicit := append(slices.Clone(base), entry)
		if got := withSystemProxy(explicit, settings); !slices.Equal(got, explicit) {
			t.Fatal("replaced an explicit proxy setting", got)
		}
	}
}

func TestSystemProxyDisabledAndInvalidPorts(t *testing.T) {
	base := []string{"PATH=/usr/bin"}
	for _, settings := range []string{
		"HTTPSEnable : 0\nHTTPSPort : 7892\nHTTPSProxy : localhost",
		"HTTPSEnable : 1\nHTTPSPort : 0\nHTTPSProxy : localhost",
		"HTTPSEnable : 1\nHTTPSPort : 65536\nHTTPSProxy : localhost",
		"HTTPSEnable : 1\nHTTPSPort : invalid\nHTTPSProxy : localhost",
	} {
		if got := withSystemProxy(base, settings); !slices.Equal(got, base) {
			t.Fatal("used an unavailable proxy", got)
		}
	}
}
