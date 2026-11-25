package portmap

// NAT-PMP (RFC 6886) uses two tiny UDP messages to the router.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"
)

const natpmpPort = 5351

type natpmp struct {
	gw netip.AddrPort
}

func (n *natpmp) name() string { return "NAT-PMP" }

// call sends a request and waits for its answer (trying a few times, as
// the standard says).
func (n *natpmp) call(req []byte, op byte) ([]byte, error) {
	conn, err := net.DialUDP("udp", nil, net.UDPAddrFromAddrPort(n.gw))
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	buf := make([]byte, 64)
	wait := 250 * time.Millisecond
	for try := 0; try < 3; try++ {
		if _, err := conn.Write(req); err != nil {
			return nil, err
		}
		conn.SetReadDeadline(time.Now().Add(wait))
		k, err := conn.Read(buf)
		wait *= 2
		if err != nil {
			continue
		}
		if k < 8 || buf[0] != 0 || buf[1] != 128+op {
			return nil, errors.New("NAT-PMP: a strange answer")
		}
		if code := binary.BigEndian.Uint16(buf[2:4]); code != 0 {
			return nil, fmt.Errorf("NAT-PMP: refused (code %d)", code)
		}
		return buf[:k], nil
	}
	return nil, errors.New("NAT-PMP: no answer")
}

func (n *natpmp) externalIP() (netip.Addr, error) {
	r, err := n.call([]byte{0, 0}, 0)
	if err != nil {
		return netip.Addr{}, err
	}
	if len(r) < 12 {
		return netip.Addr{}, errors.New("NAT-PMP: short answer")
	}
	return netip.AddrFrom4([4]byte(r[8:12])), nil
}

func (n *natpmp) mapTCP(port int, life time.Duration) error {
	req := make([]byte, 12)
	req[1] = 2 // This asks for TCP.
	binary.BigEndian.PutUint16(req[4:], uint16(port))
	binary.BigEndian.PutUint16(req[6:], uint16(port))
	binary.BigEndian.PutUint32(req[8:], uint32(life/time.Second))
	r, err := n.call(req, 2)
	if err != nil {
		return err
	}
	if life > 0 && len(r) >= 12 && int(binary.BigEndian.Uint16(r[10:12])) != port {
		n.mapTCP(port, 0) // It gave another port, which people wouldn't know.
		return errors.New("NAT-PMP: the router offered a different port")
	}
	return nil
}

func (n *natpmp) addMapping(port int, _ netip.Addr) error { return n.mapTCP(port, lease) }
func (n *natpmp) deleteMapping(port int) error            { return n.mapTCP(port, 0) }
