package address

import (
	"git.gammaspectra.live/P2Pool/moneroutil"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"unsafe"
)

const PackedAddressSpend = 0
const PackedAddressView = 1

// PackedAddress 0 = spend, 1 = view
type PackedAddress [2]crypto.PublicKeyBytes

func NewPackedAddressFromBytes(spend, view crypto.PublicKeyBytes) (result PackedAddress) {
	copy(result[PackedAddressSpend][:], spend[:])
	copy(result[PackedAddressView][:], view[:])
	return
}

func NewPackedAddress(spend, view crypto.PublicKey) (result PackedAddress) {
	return NewPackedAddressFromBytes(spend.AsBytes(), view.AsBytes())
}

func (p *PackedAddress) PublicKeys() (spend, view crypto.PublicKey) {
	return &(*p)[PackedAddressSpend], &(*p)[PackedAddressView]
}

func (p *PackedAddress) SpendPublicKey() *crypto.PublicKeyBytes {
	return &(*p)[PackedAddressSpend]
}

func (p *PackedAddress) ViewPublicKey() *crypto.PublicKeyBytes {
	return &(*p)[PackedAddressView]
}

func (p *PackedAddress) ToPackedAddress() PackedAddress {
	return *p
}

// Compare special consensus comparison
func (p *PackedAddress) Compare(b Interface) int {
	//compare spend key

	resultSpendKey := crypto.CompareConsensusPublicKeyBytes(&p[PackedAddressSpend], b.SpendPublicKey())
	if resultSpendKey != 0 {
		return resultSpendKey
	}

	// compare view key
	return crypto.CompareConsensusPublicKeyBytes(&p[PackedAddressView], b.ViewPublicKey())
}

func (p PackedAddress) ComparePacked(other *PackedAddress) int {
	//compare spend key

	resultSpendKey := crypto.CompareConsensusPublicKeyBytes(&p[PackedAddressSpend], &other[PackedAddressSpend])
	if resultSpendKey != 0 {
		return resultSpendKey
	}

	// compare view key
	return crypto.CompareConsensusPublicKeyBytes(&p[PackedAddressView], &other[PackedAddressView])
}

func (p *PackedAddress) ToAddress(network uint8, err ...error) *Address {
	if len(err) > 0 && err[0] != nil {
		return nil
	}
	return FromRawAddress(network, p.SpendPublicKey(), p.ViewPublicKey())
}

func (p PackedAddress) ToBase58(network uint8, err ...error) []byte {
	var nice [69]byte
	nice[0] = network
	copy(nice[1:], p[PackedAddressSpend][:])
	copy(nice[1+crypto.PublicKeySize:], p[PackedAddressView][:])
	sum := crypto.PooledKeccak256(nice[:65])

	buf := make([]byte, 0, 95)
	return moneroutil.EncodeMoneroBase58PreAllocated(buf, []byte{network}, p[PackedAddressSpend][:], p[PackedAddressView][:], sum[:4])
}

func (p PackedAddress) Reference() *PackedAddress {
	return &p
}

func (p PackedAddress) Bytes() []byte {
	return (*[crypto.PublicKeySize * 2]byte)(unsafe.Pointer(&p))[:]
}
