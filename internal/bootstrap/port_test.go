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

// TestLoopbackOnlyRefusesReachableAddresses checks that the core only ever
// serves this machine: an address other machines can reach would let anyone
// on the network talk to it, because the Host header is the only thing
// standing between them and the session.
func TestLoopbackOnlyRefusesReachableAddresses(t *testing.T) {
	for _, listen := range []string{"127.0.0.1:8080", "127.0.0.53:8080", "localhost:8080", "[::1]:8080"} {
		if err := loopbackOnly(listen); err != nil {
			t.Errorf("%s: %v", listen, err)
		}
	}
	for _, listen := range []string{"0.0.0.0:8080", ":8080", "192.168.1.10:8080", "[::]:8080", "example.invalid:8080", "127.0.0.1"} {
		if err := loopbackOnly(listen); err == nil {
			t.Errorf("%s: the core would serve other machines", listen)
		}
	}
}
