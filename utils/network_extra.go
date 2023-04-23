//go:build !linux

package utils

import "net"

// InterfaceAddrs returns a list of unicast interface addresses for a specific
// interface.
func InterfaceAddrs(ifi *net.Interface) ([]*ExtendedIPNet, error) {
	a, err := ifi.Addrs()
	if err != nil {
		return nil, err
	}
	var addrs []*ExtendedIPNet
	for _, e := range a {
		if ipNet, ok := e.(*net.IPNet); ok {
			addrs = append(addrs, &ExtendedIPNet{
				IP:   ipNet.IP,
				Mask: ipNet.Mask,
			})
		}
	}
	return addrs, nil
}
