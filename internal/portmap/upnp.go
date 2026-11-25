package portmap

// A UPnP Internet Gateway Device is found with a multicast "M-SEARCH" on the
// home network (SSDP), then asked with small SOAP messages over HTTP.

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var ssdpAddr = &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: 1900}

var wanServices = []string{
	"urn:schemas-upnp-org:service:WANIPConnection:2",
	"urn:schemas-upnp-org:service:WANIPConnection:1",
	"urn:schemas-upnp-org:service:WANPPPConnection:1",
}

const v6Service = "urn:schemas-upnp-org:service:WANIPv6FirewallControl:1"

type upnp struct {
	control, service string // These reach the port mapping service.
	v6control        string // This reaches the IPv6 firewall, if there is one.
	local            netip.Addr
	client           *http.Client
}

func (u *upnp) name() string { return "UPnP" }

// discoverUPnP finds the router's port mapping service.
func discoverUPnP(wait time.Duration) (*upnp, error) {
	conn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	for _, st := range []string{"urn:schemas-upnp-org:device:InternetGatewayDevice:1", "urn:schemas-upnp-org:device:InternetGatewayDevice:2"} {
		msg := "M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nMAN: \"ssdp:discover\"\r\nMX: 2\r\nST: " + st + "\r\n\r\n"
		conn.WriteToUDP([]byte(msg), ssdpAddr)
	}
	conn.SetReadDeadline(time.Now().Add(wait))
	buf := make([]byte, 2048)
	seen := map[string]bool{}
	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			return nil, errNoGateway
		}
		resp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(buf[:n])), nil)
		if err != nil {
			continue
		}
		loc := resp.Header.Get("Location")
		if loc == "" || seen[loc] {
			continue
		}
		seen[loc] = true
		if u, err := describe(loc); err == nil {
			return u, nil
		}
	}
}

// device is the part of a UPnP description we need.
type device struct {
	URLBase string `xml:"URLBase"`
	Device  node   `xml:"device"`
}

type node struct {
	Services []service `xml:"serviceList>service"`
	Devices  []node    `xml:"deviceList>device"`
}

type service struct {
	Type    string `xml:"serviceType"`
	Control string `xml:"controlURL"`
}

func (n node) all() []service {
	out := append([]service{}, n.Services...)
	for _, d := range n.Devices {
		out = append(out, d.all()...)
	}
	return out
}

// describe reads a router's description and finds its services.
func describe(location string) (*upnp, error) {
	base, err := url.Parse(location)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") {
		return nil, errors.New("UPnP: a strange location")
	}
	host, err := netip.ParseAddr(base.Hostname())
	if err != nil || !(host.IsPrivate() || host.IsLoopback() || host.IsLinkLocalUnicast()) {
		// Requests are never sent outside the home network.
		return nil, errors.New("UPnP: the router must be on the home network")
	}
	client := &http.Client{Timeout: 4 * time.Second}
	resp, err := client.Get(location)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var d device
	if err := xml.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&d); err != nil {
		return nil, err
	}
	if d.URLBase != "" {
		if b, err := url.Parse(d.URLBase); err == nil {
			base = b
		}
	}
	u := &upnp{client: client}
	services := d.Device.all()
	for _, want := range wanServices {
		for _, s := range services {
			if s.Type == want && u.control == "" {
				u.service, u.control = want, resolve(base, s.Control)
			}
		}
	}
	for _, s := range services {
		if s.Type == v6Service {
			u.v6control = resolve(base, s.Control)
		}
	}
	if u.control == "" {
		return nil, errors.New("UPnP: no port mapping service")
	}
	return u, nil
}

func resolve(base *url.URL, ref string) string {
	r, err := url.Parse(strings.TrimSpace(ref))
	if err != nil {
		return ""
	}
	return base.ResolveReference(r).String()
}

// soap calls an action and returns the answer's body.
func (u *upnp) soap(control, svc, action string, args [][2]string) (string, error) {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body>`)
	b.WriteString("<u:" + action + ` xmlns:u="` + svc + `">`)
	for _, a := range args {
		b.WriteString("<" + a[0] + ">")
		xml.EscapeText(&b, []byte(a[1]))
		b.WriteString("</" + a[0] + ">")
	}
	b.WriteString("</u:" + action + "></s:Body></s:Envelope>")
	req, err := http.NewRequest("POST", control, strings.NewReader(b.String()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("SOAPAction", `"`+svc+"#"+action+`"`)
	resp, err := u.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != 200 {
		code := between(string(body), "<errorCode>", "</errorCode>")
		return "", fmt.Errorf("UPnP %s refused (%s %s)", action, resp.Status, code)
	}
	return string(body), nil
}

func between(s, a, b string) string {
	i := strings.Index(s, a)
	if i < 0 {
		return ""
	}
	s = s[i+len(a):]
	if j := strings.Index(s, b); j >= 0 {
		return s[:j]
	}
	return ""
}

func (u *upnp) externalIP() (netip.Addr, error) {
	body, err := u.soap(u.control, u.service, "GetExternalIPAddress", nil)
	if err != nil {
		return netip.Addr{}, err
	}
	return netip.ParseAddr(strings.TrimSpace(between(body, "<NewExternalIPAddress>", "</NewExternalIPAddress>")))
}

func (u *upnp) addMapping(port int, local netip.Addr) error {
	p := strconv.Itoa(port)
	args := func(lease int) [][2]string {
		return [][2]string{{"NewRemoteHost", ""}, {"NewExternalPort", p}, {"NewProtocol", "TCP"},
			{"NewInternalPort", p}, {"NewInternalClient", local.String()}, {"NewEnabled", "1"},
			{"NewPortMappingDescription", description}, {"NewLeaseDuration", strconv.Itoa(lease)}}
	}
	_, err := u.soap(u.control, u.service, "AddPortMapping", args(int(lease/time.Second)))
	if err != nil && strings.Contains(err.Error(), "725") { // The router allows only lasting mappings.
		_, err = u.soap(u.control, u.service, "AddPortMapping", args(0))
	}
	return err
}

func (u *upnp) deleteMapping(port int) error {
	_, err := u.soap(u.control, u.service, "DeletePortMapping",
		[][2]string{{"NewRemoteHost", ""}, {"NewExternalPort", strconv.Itoa(port)}, {"NewProtocol", "TCP"}})
	return err
}

// This is the IPv6 firewall (WANIPv6FirewallControl).

func (u *upnp) addPinhole(port int, local netip.Addr) (string, error) {
	if u.v6control == "" {
		return "", errors.New("UPnP: no IPv6 firewall service")
	}
	body, err := u.soap(u.v6control, v6Service, "AddPinhole", [][2]string{{"RemoteHost", ""}, {"RemotePort", "0"},
		{"InternalClient", local.String()}, {"InternalPort", strconv.Itoa(port)}, {"Protocol", "6"},
		{"LeaseTime", strconv.Itoa(int(lease / time.Second))}})
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(between(body, "<UniqueID>", "</UniqueID>"))
	if id == "" {
		return "", errors.New("UPnP: no pinhole ID")
	}
	return id, nil
}

func (u *upnp) updatePinhole(id string) error {
	if u.v6control == "" {
		return errors.New("UPnP: no IPv6 firewall service")
	}
	_, err := u.soap(u.v6control, v6Service, "UpdatePinhole", [][2]string{{"UniqueID", id}, {"NewLeaseTime", strconv.Itoa(int(lease / time.Second))}})
	return err
}

func (u *upnp) deletePinhole(id string) error {
	if u.v6control == "" {
		return nil
	}
	_, err := u.soap(u.v6control, v6Service, "DeletePinhole", [][2]string{{"UniqueID", id}})
	return err
}
