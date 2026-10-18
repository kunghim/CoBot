package bootstrap

import (
	"net"
	"testing"

	"github.com/kunghim/CoBot/internal/platform"
)

func TestPreferredPortIsStableAndRemembered(t *testing.T) {
	a := platform.Paths{Root: t.TempDir()}
	if preferredPort(a) != preferredPort(a) {
		t.Fatal("the derived port must not change between starts")
	}
	if p := preferredPort(a); p < stablePortBase || p >= stablePortBase+stablePortRange {
		t.Fatalf("port %d outside the stable range", p)
	}
	if err := a.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	rememberPort(a, &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 51234})
	if got := preferredPort(a); got != 51234 {
		t.Fatalf("remembered port = %d", got)
	}
}
