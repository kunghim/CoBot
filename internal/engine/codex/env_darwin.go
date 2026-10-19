//go:build darwin

package codex

import (
	"context"
	"net"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Finder-launched apps do not inherit terminal proxy variables. Honor the
// user's existing system proxy for Codex only, without editing system settings
// or replacing explicitly supplied proxy variables (including empty ones).
func platformEngineEnv(base []string) []string {
	if hasProxyEnv(base) {
		return base
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/sbin/scutil", "--proxy").Output()
	if err != nil {
		return base
	}
	return withSystemProxy(base, string(out))
}

func hasProxyEnv(base []string) bool {
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(key) {
		case "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY":
			return true
		}
	}
	return false
}

func withSystemProxy(base []string, settings string) []string {
	if hasProxyEnv(base) {
		return base
	}
	values := map[string]string{}
	var exceptions []string
	inExceptions := false
	for _, line := range strings.Split(settings, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), " : ")
		if key == "ExceptionsList" {
			inExceptions = true
			continue
		}
		if inExceptions {
			if strings.TrimSpace(line) == "}" {
				inExceptions = false
			} else if ok {
				exceptions = append(exceptions, value)
			}
			continue
		}
		if ok {
			values[key] = value
		}
	}
	proxyURL := func(prefix, scheme string) string {
		port, err := strconv.Atoi(values[prefix+"Port"])
		host := strings.TrimSpace(values[prefix+"Proxy"])
		if values[prefix+"Enable"] != "1" || host == "" || err != nil || port < 1 || port > 65535 {
			return ""
		}
		return (&url.URL{Scheme: scheme, Host: net.JoinHostPort(host, strconv.Itoa(port))}).String()
	}
	out := append([]string(nil), base...)
	for _, protocol := range []string{"HTTP", "HTTPS"} {
		if proxy := proxyURL(protocol, "http"); proxy != "" {
			out = append(out, protocol+"_PROXY="+proxy)
		}
	}
	if len(out) == len(base) {
		if proxy := proxyURL("SOCKS", "socks5"); proxy != "" {
			out = append(out, "ALL_PROXY="+proxy)
		} else {
			return base
		}
	}
	// Keep local MCP and OAuth traffic on loopback. Preserve the existing
	// no-proxy list and the system's conventional hostname/IP exclusions.
	bypass := "localhost,127.0.0.1,::1"
	if len(exceptions) > 0 {
		bypass += "," + strings.Join(exceptions, ",")
	}
	filtered := make([]string, 0, len(out)+1)
	for _, entry := range out {
		key, value, _ := strings.Cut(entry, "=")
		if strings.EqualFold(key, "NO_PROXY") {
			if value != "" {
				bypass += "," + value
			}
			continue
		}
		filtered = append(filtered, entry)
	}
	return append(filtered, "NO_PROXY="+bypass)
}
