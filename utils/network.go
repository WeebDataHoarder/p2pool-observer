package utils

import (
	"net"
	"net/netip"
)

type ExtendedIPFlags uint

type ExtendedIPNet struct {
	IP    net.IP     // network number
	Mask  net.IPMask // network mask
	Flags ExtendedIPFlags
}

func (n *ExtendedIPNet) String() string {
	if n == nil {
		return "<nil>"
	}
	return (&net.IPNet{IP: n.IP, Mask: n.Mask}).String()
}

const (
	FlagTemporary ExtendedIPFlags = 1 << iota
	FlagPermanent
	FlagNoPrefixRoute
	FlagManageTempAddress
	FlagStablePrivacy
	FlagDeprecated
)

func GetOutboundIPv6() ([]netip.Addr, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var addresses []netip.Addr
	for _, i := range ifaces {
		if (i.Flags&net.FlagRunning) > 0 && (i.Flags&net.FlagUp) > 0 &&
			(i.Flags&net.FlagPointToPoint) == 0 &&
			(i.Flags&net.FlagLoopback) == 0 {
			addrs, err := InterfaceAddrs(&i)
			if err != nil {
				continue
			}

			for _, a := range addrs {
				if addr, ok := netip.AddrFromSlice(a.IP); ok && addr.Is6() && !addr.Is4In6() {
					//Filter undesired addresses
					if addr.IsUnspecified() || addr.IsLoopback() || addr.IsLinkLocalMulticast() || addr.IsLinkLocalUnicast() || addr.IsInterfaceLocalMulticast() {
						continue
					}

					//Filter generated privacy addresses directly
					if onesCount, _ := a.Mask.Size(); onesCount == 128 {
						continue
					}

					//Filter
					if (a.Flags & FlagNoPrefixRoute) > 0 {
						continue
					}
					if (a.Flags & FlagDeprecated) > 0 {
						continue
					}
					addresses = append(addresses, netip.MustParseAddr(a.IP.String()))
				}
			}
		}
	}

	return addresses, nil
}

var cgnatStart = netip.MustParseAddr("100.64.0.0")
var cgnatEnd = netip.MustParseAddr("100.127.255.255")

func NetIPIsCGNAT(addr netip.Addr) bool {
	return addr.Is4() && addr.Compare(cgnatStart) >= 0 && addr.Compare(cgnatEnd) <= 0
}

func NetIPGetEUI48Information(addr netip.Addr) {
	if !addr.Is6() || addr.Is4In6() {
		return
	}
}
