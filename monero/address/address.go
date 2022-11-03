package address

import (
	"bytes"
	"encoding/json"
	"errors"
	"filippo.io/edwards25519"
	"git.gammaspectra.live/P2Pool/moneroutil"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"sync/atomic"
	"unsafe"
)

type PackedAddress struct {
	SpendPub types.Hash
	ViewPub  types.Hash
}

func (p PackedAddress) Compare(b PackedAddress) int {
	return bytes.Compare(unsafe.Slice((*byte)(unsafe.Pointer(&p)), unsafe.Sizeof(p)), unsafe.Slice((*byte)(unsafe.Pointer(&b)), unsafe.Sizeof(b)))
	/*
		if r := bytes.Compare(p.SpendPub[:], b.SpendPub[:]); r != 0 {
			return r
		} else {
			return bytes.Compare(p.ViewPub[:], b.ViewPub[:])
		}
	*/
}

func (p PackedAddress) ToAddress() *Address {
	return FromRawAddress(moneroutil.MainNetwork, p.SpendPub, p.ViewPub)
}

type Address struct {
	Network  uint8
	SpendPub *edwards25519.Point
	ViewPub  *edwards25519.Point
	checksum []byte
	// IsSubAddress Always false
	IsSubAddress bool

	moneroAddress atomic.Pointer[edwards25519.Scalar]
}

func FromBase58(address string) *Address {
	raw := moneroutil.DecodeMoneroBase58(address)

	if len(raw) != 69 {
		return nil
	}
	checksum := moneroutil.GetChecksum(raw[:65])
	if bytes.Compare(checksum[:], raw[65:]) != 0 {
		return nil
	}
	a := &Address{
		Network:  raw[0],
		checksum: checksum[:],
	}

	var err error
	if a.SpendPub, err = (&edwards25519.Point{}).SetBytes(raw[1:33]); err != nil {
		return nil
	}
	if a.ViewPub, err = (&edwards25519.Point{}).SetBytes(raw[33:65]); err != nil {
		return nil
	}

	return a
}

func FromRawAddress(network uint8, spend, view types.Hash) *Address {
	nice := make([]byte, 69)
	nice[0] = network
	copy(nice[1:], spend[:])
	copy(nice[33:], view[:])

	//TODO: cache checksum?
	checksum := moneroutil.GetChecksum(nice[:65])
	a := &Address{
		Network:  nice[0],
		checksum: checksum[:],
	}

	var err error
	if a.SpendPub, err = (&edwards25519.Point{}).SetBytes(spend[:]); err != nil {
		return nil
	}
	if a.ViewPub, err = (&edwards25519.Point{}).SetBytes(view[:]); err != nil {
		return nil
	}

	return a
}

func (a *Address) ToBase58() string {
	return moneroutil.EncodeMoneroBase58([]byte{a.Network}, a.SpendPub.Bytes(), a.ViewPub.Bytes(), a.checksum[:])
}

func (a *Address) ToPacked() PackedAddress {
	return PackedAddress{SpendPub: types.HashFromBytes(a.SpendPub.Bytes()), ViewPub: types.HashFromBytes(a.ViewPub.Bytes())}
}

func (a *Address) MarshalJSON() ([]byte, error) {
	return json.Marshal(a.ToBase58())
}

func (a *Address) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}

	if addr := FromBase58(s); addr != nil {
		a.Network = addr.Network
		a.SpendPub = addr.SpendPub
		a.ViewPub = addr.ViewPub
		a.checksum = addr.checksum
		return nil
	} else {
		return errors.New("invalid address")
	}
}
