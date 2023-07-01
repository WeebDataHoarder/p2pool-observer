package address

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"unsafe"
)

type PackedAddress [2]crypto.PublicKeyBytes

func NewPackedAddressFromBytes(spend, view crypto.PublicKeyBytes) (result PackedAddress) {
	copy(result[0][:], spend[:])
	copy(result[1][:], view[:])
	return
}

func NewPackedAddress(spend, view crypto.PublicKey) (result PackedAddress) {
	return NewPackedAddressFromBytes(spend.AsBytes(), view.AsBytes())
}

func (p *PackedAddress) PublicKeys() (spend, view crypto.PublicKey) {
	return &(*p)[0], &(*p)[1]
}

func (p *PackedAddress) SpendPublicKey() *crypto.PublicKeyBytes {
	return &(*p)[0]
}

func (p *PackedAddress) ViewPublicKey() *crypto.PublicKeyBytes {
	return &(*p)[1]
}

func (p *PackedAddress) ToPackedAddress() PackedAddress {
	return *p
}

// Compare special consensus comparison
func (p *PackedAddress) Compare(b Interface) int {
	//compare spend key

	resultSpendKey := crypto.CompareConsensusPublicKeyBytes(&p[0], b.SpendPublicKey())
	if resultSpendKey != 0 {
		return resultSpendKey
	}

	// compare view key
	return crypto.CompareConsensusPublicKeyBytes(&p[1], b.ViewPublicKey())
}

func (p PackedAddress) ComparePacked(other *PackedAddress) int {
	//compare spend key

	resultSpendKey := crypto.CompareConsensusPublicKeyBytes(&p[0], &other[0])
	if resultSpendKey != 0 {
		return resultSpendKey
	}

	// compare view key
	return crypto.CompareConsensusPublicKeyBytes(&p[1], &other[1])
}

func (p *PackedAddress) ToAddress(network uint8, err ...error) *Address {
	if len(err) > 0 && err[0] != nil {
		return nil
	}
	return FromRawAddress(network, p.SpendPublicKey(), p.ViewPublicKey())
}

func (p PackedAddress) Reference() *PackedAddress {
	return &p
}

func (p PackedAddress) Bytes() []byte {
	return (*[crypto.PublicKeySize * 2]byte)(unsafe.Pointer(&p))[:]
}
