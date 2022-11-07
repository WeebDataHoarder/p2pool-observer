package address

import (
	"git.gammaspectra.live/P2Pool/moneroutil"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"runtime"
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

func (p *PackedAddress) Bytes() []byte {
	return (*[crypto.PublicKeySize*2]byte)(unsafe.Pointer(p))[:]
}

func (p *PackedAddress) PublicKeys() (spend, view crypto.PublicKey) {
	return &(*p)[0], &(*p)[1]
}

func (p *PackedAddress) SpendPublicKey() crypto.PublicKey {
	return &(*p)[0]
}

func (p *PackedAddress) ViewPublicKey() crypto.PublicKey {
	return &(*p)[1]
}

func (p *PackedAddress) ToPackedAddress() *PackedAddress {
	return p
}

// Compare special consensus comparison
func (p *PackedAddress) Compare(otherI Interface) int {
	other := otherI.ToPackedAddress()
	//golang might free other otherwise
	defer runtime.KeepAlive(other)
	defer runtime.KeepAlive(p)
	a := (*[(2*crypto.PublicKeySize)/8]uint64)(unsafe.Pointer(p))
	b := (*[(2*crypto.PublicKeySize)/8]uint64)(unsafe.Pointer(other))

	//compare spend key

	if a[3] < b[3] {
		return -1
	}
	if a[3] > b[3] {
		return 1
	}

	if a[2] < b[2] {
		return -1
	}
	if a[2] > b[2] {
		return 1
	}

	if a[1] < b[1] {
		return -1
	}
	if a[1] > b[1] {
		return 1
	}

	if a[0] < b[0] {
		return -1
	}
	if a[0] > b[0] {
		return 1
	}

	//compare view key

	if a[4+3] < b[4+3] {
		return -1
	}
	if a[4+3] > b[4+3] {
		return 1
	}

	if a[4+2] < b[4+2] {
		return -1
	}
	if a[4+2] > b[4+2] {
		return 1
	}

	if a[4+1] < b[4+1] {
		return -1
	}
	if a[4+1] > b[4+1] {
		return 1
	}

	if a[4+0] < b[4+0] {
		return -1
	}
	if a[4+0] > b[4+0] {
		return 1
	}

	return 0
}

func (p *PackedAddress) ToAddress() *Address {
	return FromRawAddress(moneroutil.MainNetwork, p.SpendPublicKey(), p.ViewPublicKey())
}

func (p *PackedAddress) ToBase58() string {
	return p.ToAddress().ToBase58()
}
