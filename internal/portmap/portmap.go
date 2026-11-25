// Package portmap asks the home router to open a guild's port, so people on the
// internet can reach it, and finds the addresses to give them. Two standard
// ways are tried, both built from Go's own pieces:
//
//   - UPnP: the router is found with a broadcast on the home network, and
//     asked over plain HTTP ("open port 50000 to this computer"). Its IPv6
//     firewall is asked too, where it has one that listens.
//   - NAT-PMP: a short message to the router on UDP port 5351.
//
// Requests last an hour and are renewed every 20 minutes, so a computer that
// loses power doesn't leave the port open for long. Close withdraws them.
package portmap

import (
	"errors"
	"log"
	"net"
	"net/netip"
	"sort"
	"sync"
	"time"
)

const (
	lease       = time.Hour
	renewEvery  = 20 * time.Minute
	retryEvery  = time.Minute
	description = "Nexus guild"
)

// Status is what the window shows about a guild's port.
type Status struct {
	// It is "UPnP" or "NAT-PMP" ("" when the router didn't answer).
	Method   string   `json:"method,omitempty"`
	External string   `json:"external,omitempty"` // It is the router's internet (IPv4) address.
	Open     bool     `json:"open,omitempty"`     // It is true when the IPv4 port is forwarded.
	CGNAT    bool     `json:"cgnat,omitempty"`    // It is true when the router's own address is shared.
	IPv6     []string `json:"ipv6,omitempty"`     // These are this computer's internet IPv6 addresses.
	// It is true when the router's IPv6 firewall lets the port in.
	IPv6Open bool     `json:"ipv6_open,omitempty"`
	Local    []string `json:"local,omitempty"` // These are the addresses on the home network.
	Problem  string   `json:"problem,omitempty"`
	Off      bool     `json:"off,omitempty"` // It is true when Port Forwarding is off.
}

// Keeper keeps one port open while a guild runs.
type Keeper struct {
	port int
	log  *log.Logger

	mu     sync.Mutex
	status Status
	router router // It is the router that answered.
	pin    string // It is the IPv6 pinhole's ID (UPnP).

	stop chan struct{}
	done chan struct{}
}

// router is a way of asking: UPnP or NAT-PMP.
type router interface {
	name() string
	externalIP() (netip.Addr, error)
	addMapping(port int, local netip.Addr) error
	deleteMapping(port int) error
}

// v6router is a router whose IPv6 firewall can be asked too.
type v6router interface {
	addPinhole(port int, local netip.Addr) (string, error)
	updatePinhole(id string) error
	deletePinhole(id string) error
}

// Open starts keeping port open (auto false: only the addresses are found).
func Open(port int, auto bool, logger *log.Logger) *Keeper {
	if logger == nil {
		logger = log.New(discard{}, "", 0)
	}
	k := &Keeper{port: port, log: logger, stop: make(chan struct{}), done: make(chan struct{})}
	k.status.Off = !auto
	go k.loop(auto)
	return k
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

// Status says how things stand now.
func (k *Keeper) Status() Status {
	k.mu.Lock()
	defer k.mu.Unlock()
	s := k.status
	s.IPv6 = append([]string(nil), s.IPv6...)
	s.Local = append([]string(nil), s.Local...)
	return s
}

// Close withdraws the requests (the guild stopped or was deleted).
func (k *Keeper) Close() {
	select {
	case <-k.stop:
		return
	default:
		close(k.stop)
	}
	<-k.done
}

func (k *Keeper) loop(auto bool) {
	defer close(k.done)
	defer k.withdraw()
	wait := time.Duration(0)
	for {
		select {
		case <-k.stop:
			return
		case <-time.After(wait):
		}
		ok := k.refresh(auto)
		wait = renewEvery
		if !ok && auto {
			wait = retryEvery // The network or router may come back.
		}
	}
}

// Mode changes what Open does: "demo" makes up a router and addresses (for
// pictures, which must never show real ones), "off" never asks the router
// (tests).
var Mode string

// demoStatus uses the addresses kept for examples in documentation.
func demoStatus(auto bool) Status {
	if !auto {
		return Status{Off: true, IPv6: []string{"2001:db8::7"}, Local: []string{"192.168.1.5"}}
	}
	return Status{Method: "UPnP", External: "203.0.113.7", Open: true, IPv6: []string{"2001:db8::7"}, IPv6Open: true, Local: []string{"192.168.1.5"}}
}

// refresh finds the addresses and (when auto) asks the router again.
func (k *Keeper) refresh(auto bool) bool {
	switch Mode {
	case "demo":
		k.set(demoStatus(auto), nil)
		return true
	case "off":
		auto = false
	}
	local4, v6 := localAddresses()
	st := Status{Off: !auto, IPv6: v6}
	for _, a := range local4 {
		st.Local = append(st.Local, a.String())
	}
	k.mu.Lock()
	r := k.router
	k.mu.Unlock()
	if r == nil {
		r = findRouter(k.port)
	}
	if r == nil {
		st.Problem = "The router didn't answer. Forward the port by hand if people on the internet should reach this guild."
		k.set(st, nil)
		return false
	}
	st.Method = r.name()
	if ip, err := r.externalIP(); err == nil && ip.IsValid() {
		st.External = ip.String()
		st.CGNAT = sharedAddress(ip)
	}
	ok := true
	if auto && len(local4) > 0 {
		if err := r.addMapping(k.port, local4[0]); err != nil {
			k.log.Printf("the router didn't open port %d: %v", k.port, err)
			st.Problem = "The router refused to open the port. Forward it by hand, or turn on UPnP in the router's settings."
			ok = false
			r = nil // Look again next time.
		} else {
			st.Open = true
		}
	}
	if auto && r != nil && len(v6) > 0 {
		st.IPv6Open = k.keepPinhole(r, v6[0])
	}
	k.set(st, r)
	return ok
}

// keepPinhole asks the router's IPv6 firewall to let the port in.
func (k *Keeper) keepPinhole(r router, addr string) bool {
	v6, ok := r.(v6router)
	if !ok {
		return false
	}
	k.mu.Lock()
	id := k.pin
	k.mu.Unlock()
	if id != "" && v6.updatePinhole(id) == nil {
		return true
	}
	a, err := netip.ParseAddr(addr)
	if err != nil {
		return false
	}
	id, err = v6.addPinhole(k.port, a)
	if err != nil {
		return false
	}
	k.mu.Lock()
	k.pin = id
	k.mu.Unlock()
	return true
}

func (k *Keeper) set(st Status, r router) {
	k.mu.Lock()
	k.status, k.router = st, r
	k.mu.Unlock()
}

// withdraw closes the port again.
func (k *Keeper) withdraw() {
	k.mu.Lock()
	r, st, pin := k.router, k.status, k.pin
	k.mu.Unlock()
	if r == nil || st.Off {
		return
	}
	if st.Open {
		if err := r.deleteMapping(k.port); err != nil {
			k.log.Printf("could not close port %d on the router: %v (it closes by itself within the hour)", k.port, err)
		}
	}
	if v6, ok := r.(v6router); ok && pin != "" {
		v6.deletePinhole(pin)
	}
}

// findRouter tries UPnP, then NAT-PMP.
func findRouter(port int) router {
	if r, err := discoverUPnP(2 * time.Second); err == nil {
		return r
	}
	if gw, err := gateway(); err == nil {
		r := &natpmp{gw: netip.AddrPortFrom(gw, natpmpPort)}
		if _, err := r.externalIP(); err == nil {
			return r
		}
	}
	return nil
}

var errNoGateway = errors.New("no router found")

// sharedAddress reports an address that isn't really on the internet: the
// provider shares one address among many homes (CGNAT), or there is a
// second router in between.
func sharedAddress(ip netip.Addr) bool {
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsUnspecified() ||
		netip.MustParsePrefix("100.64.0.0/10").Contains(ip)
}

// localAddresses are this computer's IPv4 addresses on the home network,
// and its internet IPv6 addresses.
func localAddresses() (v4 []netip.Addr, v6 []string) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, nil
	}
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip, ok := netip.AddrFromSlice(ipn.IP)
		if !ok {
			continue
		}
		ip = ip.Unmap()
		switch {
		case ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified():
		case ip.Is4() && ip.IsPrivate():
			v4 = append(v4, ip)
		case ip.Is6() && ip.IsGlobalUnicast() && !ip.IsPrivate(): // This leaves out fd00::/8, which is home-only.
			v6 = append(v6, ip.String())
		}
	}
	sort.Slice(v4, func(i, j int) bool { return v4[i].Less(v4[j]) })
	sort.Strings(v6)
	return v4, v6
}

// Addresses are what a guild tells members: host:port for every
// address that should work, best first.
func (s Status) Addresses(port int) []string {
	var out []string
	add := func(host string) {
		if a, err := netip.ParseAddr(host); err == nil {
			out = append(out, netip.AddrPortFrom(a, uint16(port)).String())
		}
	}
	if s.External != "" && !s.CGNAT {
		add(s.External)
	}
	for _, a := range s.IPv6 {
		add(a)
	}
	for _, a := range s.Local {
		add(a)
	}
	return out
}
