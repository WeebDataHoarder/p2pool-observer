package p2p

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net/netip"
)

func IsPeerVersionInformation(addr netip.AddrPort) bool {
	if addr.Port() != 0xFFFF {
		return false
	}
	rawIp := addr.Addr().As16()
	return bytes.Compare(rawIp[12:], []byte{0xFF, 0xFF, 0xFF, 0xFF}) == 0
}

type PeerVersionInformation struct {
	Protocol ProtocolVersion
	Version ImplementationVersion
	Code ImplementationCode
}

func (i *PeerVersionInformation) String() string {
	return fmt.Sprintf("%s %s (protocol %s)", i.Code.String(), i.Version.String(), i.Protocol.String())
}

func (i *PeerVersionInformation) ToAddrPort() netip.AddrPort {
	var addr [16]byte

	binary.LittleEndian.PutUint32(addr[:], uint32(i.Protocol))
	binary.LittleEndian.PutUint32(addr[4:], uint32(i.Version))
	binary.LittleEndian.PutUint32(addr[8:], uint32(i.Code))
	binary.LittleEndian.PutUint32(addr[12:], 0xFFFFFFFF)

	return netip.AddrPortFrom(netip.AddrFrom16(addr), 0xFFFF)
}

type ProtocolVersion uint32

func (v ProtocolVersion) Major() uint16 {
	return uint16(v >> 16)
}
func (v ProtocolVersion) Minor() uint16 {
	return uint16(v & 0xFFFF)
}

func (v ProtocolVersion) String() string {
	if v == 0 {
		return "unknown"
	}
	return fmt.Sprintf("%d.%d", v.Major(), v.Minor())
}

const (
	ProtocolVersion_0_0 ProtocolVersion = 0x00000000
	ProtocolVersion_1_0 ProtocolVersion = 0x00010000
	ProtocolVersion_1_1 ProtocolVersion = 0x00010001
)

const SupportedProtocolVersion = ProtocolVersion_1_1

type ImplementationVersion uint32

func (v ImplementationVersion) Major() uint16 {
	return uint16(v >> 16)
}
func (v ImplementationVersion) Minor() uint16 {
	return uint16(v & 0xFFFF)
}

func (v ImplementationVersion) String() string {
	if v == 0 {
		return "unknown"
	}
	return fmt.Sprintf("v%d.%d", v.Major(), v.Minor())
}

const CurrentImplementationVersion ImplementationVersion = 0x00000001

type ImplementationCode uint32

func (c ImplementationCode) String() string {
	switch c {
	case ImplementationCodeP2Pool:
		return "P2Pool"
	case ImplementationCodeGoObserver:
		return "GoObserver"
	default:
		return fmt.Sprintf("Unknown(%08x)", uint32(c))
	}
}

const (
	ImplementationCodeP2Pool ImplementationCode = 0x00000000
	ImplementationCodeGoObserver ImplementationCode = 0x624F6F47 //GoOb, little endian
)
