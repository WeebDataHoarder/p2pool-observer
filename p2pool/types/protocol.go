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

type ProtocolFeature int

const (
	FeatureCompactBroadcast = ProtocolFeature(iota)
	InternalFeatureFastTemplateHeaderSync
)

type PeerVersionInformation struct {
	Protocol        ProtocolVersion
	SoftwareVersion SoftwareVersion
	SoftwareId      SoftwareId
}

func (i *PeerVersionInformation) SupportsFeature(feature ProtocolFeature) bool {
	switch feature {
	case FeatureCompactBroadcast:
		return i.Protocol >= ProtocolVersion_1_1
	/*case InternalFeatureFastTemplateHeaderSync:
		return i.Protocol >= ProtocolVersion_1_1 && i.SoftwareId == SoftwareIdGoObserver && i.SoftwareVersion.Major() == 1 && i.SoftwareVersion >= ((1<<16)|1)*/
	default:
		return false
	}
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

type ProtocolVersion SemanticVersion

func (v ProtocolVersion) Major() uint16 {
	return SemanticVersion(v).Major()
}
func (v ProtocolVersion) Minor() uint16 {
	return SemanticVersion(v).Minor()
}

func (v ProtocolVersion) String() string {
	return SemanticVersion(v).String()
}

const (
	ProtocolVersion_0_0 ProtocolVersion = (0 << 16) | 0
	ProtocolVersion_1_0 ProtocolVersion = (1 << 16) | 0
	ProtocolVersion_1_1 ProtocolVersion = (1 << 16) | 1
)

type SoftwareVersion SemanticVersion

func (v SoftwareVersion) Major() uint16 {
	return SemanticVersion(v).Major()
}
func (v SoftwareVersion) Minor() uint16 {
	return SemanticVersion(v).Minor()
}

func (v SoftwareVersion) String() string {
	return SemanticVersion(v).String()
}

const SupportedProtocolVersion = ProtocolVersion_1_1
const CurrentSoftwareVersion SoftwareVersion = (2 << 16) | 0
const CurrentSoftwareId = SoftwareIdGoObserver

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
