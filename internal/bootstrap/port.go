package bootstrap

import (
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
