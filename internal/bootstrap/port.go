package bootstrap

import (
	"fmt"
	"hash/fnv"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kunghim/CoBot/internal/platform"
)

// Stable ports live in a quiet range; the exact one is derived from the data
// root so two profiles on one machine do not compete for it.
const (
	stablePortBase  = 47000
	stablePortRange = 2000
)

func portFile(paths platform.Paths) string {
	return filepath.Join(paths.RuntimeDir(), "listen-port")
}

// preferredPort is the port this profile used last, or its derived default.
func preferredPort(paths platform.Paths) int {
	if data, err := os.ReadFile(portFile(paths)); err == nil {
		if port, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && port > 0 && port < 65536 {
			return port
		}
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(filepath.Clean(paths.Root))))
	return stablePortBase + int(h.Sum32()%stablePortRange)
}

// rememberPort records the port actually bound for the next start.
func rememberPort(paths platform.Paths, addr net.Addr) {
	tcp, ok := addr.(*net.TCPAddr)
	if !ok {
		return
	}
	_ = os.WriteFile(portFile(paths), []byte(strconv.Itoa(tcp.Port)+"\n"), 0o600)
}

// loopbackOnly refuses an address other machines can reach.
//
// The core's authority is the Host header and the session cookie, and it
// only accepts a loopback Host — a protection that a listener on a public
// address would let anyone on the network walk around. CoBot serves this
// machine and nothing else, so an address that is not loopback is refused
// instead of quietly opening the door.
func loopbackOnly(listen string) error {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("listen address %q is not host:port: %w", listen, err)
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("listen address %q is not a loopback address; CoBot only serves this machine", listen)
	}
	return nil
}
