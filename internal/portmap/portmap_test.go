package portmap

import (
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"sync"
	"testing"
)

// TestNATPMP talks to a pretend router.
func TestNATPMP(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	var mu sync.Mutex
	mapped := map[int]uint32{}
	go func() {
		buf := make([]byte, 64)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			op := buf[1]
			out := make([]byte, 16)
			out[1] = 128 + op
			switch op {
			case 0:
				copy(out[8:12], []byte{100, 70, 3, 9}) // This is a shared (CGNAT) address.
				pc.WriteTo(out[:12], from)
			case 2:
				port := int(binary.BigEndian.Uint16(buf[4:6]))
				life := binary.BigEndian.Uint32(buf[8:12])
				mu.Lock()
				mapped[port] = life
				mu.Unlock()
				binary.BigEndian.PutUint16(out[8:], uint16(port))
				binary.BigEndian.PutUint16(out[10:], uint16(port))
				binary.BigEndian.PutUint32(out[12:], life)
				pc.WriteTo(out, from)
			}
			_ = n
		}
	}()
	r := &natpmp{gw: netip.MustParseAddrPort(pc.LocalAddr().String())}
	ip, err := r.externalIP()
	if err != nil || ip.String() != "100.70.3.9" || !sharedAddress(ip) {
		t.Fatalf("external %v %v", ip, err)
	}
	if err := r.addMapping(50000, netip.Addr{}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if mapped[50000] != 3600 {
		t.Fatalf("an hour's lease, got %d", mapped[50000])
	}
	mu.Unlock()
	if err := r.deleteMapping(50000); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if mapped[50000] != 0 {
		t.Fatal("deleting asks for a zero lease")
	}
}

// TestUPnP reads a pretend router's description and asks it things.
func TestUPnP(t *testing.T) {
	var mu sync.Mutex
	var actions []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			io.WriteString(w, `<root><device><deviceList><device><deviceList><device><serviceList>
				<service><serviceType>urn:schemas-upnp-org:service:WANIPConnection:1</serviceType><controlURL>/ctl/IPConn</controlURL></service>
				<service><serviceType>urn:schemas-upnp-org:service:WANIPv6FirewallControl:1</serviceType><controlURL>/ctl/v6</controlURL></service>
				</serviceList></device></deviceList></device></deviceList></device></root>`)
			return
		}
		body, _ := io.ReadAll(r.Body)
		action := strings.Trim(r.Header.Get("SOAPAction"), `"`)
		mu.Lock()
		actions = append(actions, action[strings.Index(action, "#")+1:]+" "+r.URL.Path)
		mu.Unlock()
		switch {
		case strings.HasSuffix(action, "#GetExternalIPAddress"):
			io.WriteString(w, `<s:Envelope><s:Body><u:GetExternalIPAddressResponse><NewExternalIPAddress>49.36.10.20</NewExternalIPAddress></u:GetExternalIPAddressResponse></s:Body></s:Envelope>`)
		case strings.HasSuffix(action, "#AddPortMapping"):
			if !strings.Contains(string(body), "<NewInternalClient>192.168.1.5</NewInternalClient>") ||
				!strings.Contains(string(body), "<NewExternalPort>50000</NewExternalPort>") {
				w.WriteHeader(500)
			}
		case strings.HasSuffix(action, "#AddPinhole"):
			io.WriteString(w, `<s:Envelope><s:Body><u:AddPinholeResponse><UniqueID>7</UniqueID></u:AddPinholeResponse></s:Body></s:Envelope>`)
		}
	}))
	defer srv.Close()
	u, err := describe(srv.URL + "/rootDesc.xml")
	if err != nil {
		t.Fatal(err)
	}
	ip, err := u.externalIP()
	if err != nil || ip.String() != "49.36.10.20" || sharedAddress(ip) {
		t.Fatalf("external %v %v", ip, err)
	}
	if err := u.addMapping(50000, netip.MustParseAddr("192.168.1.5")); err != nil {
		t.Fatal(err)
	}
	if id, err := u.addPinhole(50000, netip.MustParseAddr("2401:4900::5")); err != nil || id != "7" {
		t.Fatalf("pinhole %q %v", id, err)
	}
	u.deleteMapping(50000)
	u.deletePinhole("7")
	mu.Lock()
	defer mu.Unlock()
	want := "GetExternalIPAddress /ctl/IPConn,AddPortMapping /ctl/IPConn,AddPinhole /ctl/v6,DeletePortMapping /ctl/IPConn,DeletePinhole /ctl/v6"
	if got := strings.Join(actions, ","); got != want {
		t.Fatalf("asked:\n%s\nwant:\n%s", got, want)
	}

	// A router outside the home network is never used.
	if _, err := describe("http://203.0.113.5/rootDesc.xml"); err == nil {
		t.Fatal("a router on the internet is refused")
	}
}

func TestAddresses(t *testing.T) {
	s := Status{External: "49.36.10.20", IPv6: []string{"2401:4900::5"}, Local: []string{"192.168.1.5"}}
	got := strings.Join(s.Addresses(50000), " ")
	if got != "49.36.10.20:50000 [2401:4900::5]:50000 192.168.1.5:50000" {
		t.Fatal(got)
	}
	s.CGNAT = true
	if strings.Contains(strings.Join(s.Addresses(50000), " "), "49.36") {
		t.Fatal("a shared address isn't given out")
	}
}

// TestThisRouter only looks (it never opens anything): run with
// NEXUS_ROUTER_TEST=1 to see what this network's router answers.
func TestThisRouter(t *testing.T) {
	if os.Getenv("NEXUS_ROUTER_TEST") != "1" {
		t.Skip("set NEXUS_ROUTER_TEST=1 to look at this network's router")
	}
	gw, err := gateway()
	t.Logf("router address: %v %v", gw, err)
	if u, err := discoverUPnP(3 * 1e9); err == nil {
		ip, err := u.externalIP()
		t.Logf("UPnP: yes; internet address %v %v; IPv6 firewall service: %v", ip, err, u.v6control != "")
	} else {
		t.Logf("UPnP: %v", err)
	}
	if err == nil {
		ip, err := (&natpmp{gw: netip.AddrPortFrom(gw, natpmpPort)}).externalIP()
		t.Logf("NAT-PMP: %v %v", ip, err)
	}
	v4, v6 := localAddresses()
	t.Logf("home network: %v; IPv6: %v", v4, v6)
}
