package host

import (
	"net"
	"testing"
)

// TestBothAddressKinds checks that a port opened the way GuildForge opens it (no
// address given) answers on IPv4 and on IPv6, so a guild can be reached by
// either kind of address.
func TestBothAddressKinds(t *testing.T) {
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	_, port, _ := net.SplitHostPort(l.Addr().String())
	for _, host := range []string{"127.0.0.1", "::1"} {
		c, err := net.Dial("tcp", net.JoinHostPort(host, port))
		if err != nil {
			if host == "::1" {
				t.Skipf("this computer has IPv6 turned off: %v", err)
			}
			t.Fatalf("%s: %v", host, err)
		}
		c.Close()
	}
}
