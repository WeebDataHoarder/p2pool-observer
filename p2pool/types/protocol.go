package types

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
	Protocol        ProtocolVersion
	SoftwareVersion SoftwareVersion
	SoftwareId      SoftwareId
}

func (i *PeerVersionInformation) String() string {
	return fmt.Sprintf("%s %s (protocol %s)", i.SoftwareId.String(), i.SoftwareVersion.String(), i.Protocol.String())
}

func (i *PeerVersionInformation) ToAddrPort() netip.AddrPort {
	var addr [16]byte

	binary.LittleEndian.PutUint32(addr[:], uint32(i.Protocol))
	binary.LittleEndian.PutUint32(addr[4:], uint32(i.SoftwareVersion))
	binary.LittleEndian.PutUint32(addr[8:], uint32(i.SoftwareId))
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

type SoftwareVersion uint32

func (v SoftwareVersion) Major() uint16 {
	return uint16(v >> 16)
}
func (v SoftwareVersion) Minor() uint16 {
	return uint16(v & 0xFFFF)
}

func (v SoftwareVersion) String() string {
	if v == 0 {
		return "unknown"
	}
	return fmt.Sprintf("v%d.%d", v.Major(), v.Minor())
}

const CurrentSoftwareVersion SoftwareVersion = 0x00000001

type SoftwareId uint32

func (c SoftwareId) String() string {
	switch c {
	case SoftwareIdP2Pool:
		return "P2Pool"
	case SoftwareIdGoObserver:
		return "GoObserver"
	default:
		return fmt.Sprintf("Unknown(%08x)", uint32(c))
	}
}

const (
	SoftwareIdP2Pool     SoftwareId = 0x00000000
	SoftwareIdGoObserver SoftwareId = 0x624F6F47 //GoOb, little endian
)
