package address

import (
	"bytes"
	"git.gammaspectra.live/P2Pool/moneroutil"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
)


type PackedAddress [crypto.PublicKeySize*2]byte

func NewPackedAddressFromBytes(spend, view crypto.PublicKeyBytes) (result PackedAddress) {
	copy(result[:], spend[:])
	copy(result[crypto.PublicKeySize:], view[:])
	return
}

func NewPackedAddress(spend, view crypto.PublicKey) (result PackedAddress) {
	return NewPackedAddressFromBytes(spend.AsBytes(), view.AsBytes())
}

func (p *PackedAddress) PublicKeys() (spend, view crypto.PublicKey) {
	var s, v crypto.PublicKeyBytes
	copy(s[:], (*p)[:])
	copy(v[:], (*p)[crypto.PublicKeySize:])
	return &s, &v
}

func (p *PackedAddress) SpendPublicKey() crypto.PublicKey {
	var s crypto.PublicKeyBytes
	copy(s[:], (*p)[:])
	return &s
}

func (p *PackedAddress) ViewPublicKey() crypto.PublicKey {
	var v crypto.PublicKeyBytes
	copy(v[:], (*p)[crypto.PublicKeySize:])
	return &v
}

func (p *PackedAddress) ToPackedAddress() *PackedAddress {
	return p
}

func (p *PackedAddress) Compare(b Interface) int {
	other := b.ToPackedAddress()
	return bytes.Compare((*p)[:], other[:])
}

func (p *PackedAddress) ToAddress() *Address {
	return FromRawAddress(moneroutil.MainNetwork, p.SpendPublicKey(), p.ViewPublicKey())
}

func (p *PackedAddress) ToBase58() string {
	return p.ToAddress().ToBase58()
}