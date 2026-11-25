//go:build windows

package portmap

import (
	"net/netip"
	"syscall"
	"unsafe"
)

var getBestRoute = syscall.NewLazyDLL("iphlpapi.dll").NewProc("GetBestRoute")

// gateway is the router's home network address: where Windows would send
// a packet bound for the internet (nothing is sent).
func gateway() (netip.Addr, error) {
	var row [56]byte                  // This is a MIB_IPFORWARDROW.
	dest := uint32(1) | uint32(1)<<24 // This is 1.0.0.1, in network order.
	r, _, _ := getBestRoute.Call(uintptr(dest), 0, uintptr(unsafe.Pointer(&row[0])))
	if r != 0 {
		return netip.Addr{}, errNoGateway
	}
	ip := netip.AddrFrom4([4]byte(row[12:16])) // This is dwForwardNextHop.
	if !ip.IsPrivate() {
		return netip.Addr{}, errNoGateway
	}
	return ip, nil
}
