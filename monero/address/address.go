package address

import (
	"bytes"
	"encoding/json"
	"errors"
	"git.gammaspectra.live/P2Pool/moneroutil"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
)

type Address struct {
	Network  uint8
	SpendPub crypto.PublicKey
	ViewPub  crypto.PublicKey
	checksum []byte
	// IsSubAddress Always false
	IsSubAddress bool
}

func (a *Address) Compare(b Interface) int {
	//TODO
	return a.ToPackedAddress().Compare(b)
}

func (a *Address) PublicKeys() (spend, view crypto.PublicKey) {
	return a.SpendPub, a.ViewPub
}

func (a *Address) SpendPublicKey() crypto.PublicKey {
	return a.SpendPub
}

func (a *Address) ViewPublicKey() crypto.PublicKey {
	return a.ViewPub
}

func (a *Address) ToAddress() *Address {
	return a
}

func (a *Address) ToPackedAddress() *PackedAddress {
	p := NewPackedAddress(a.SpendPub, a.ViewPub)
	return &p
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

	var spend, view crypto.PublicKeyBytes
	copy(spend[:], raw[1:33])
	copy(view[:], raw[33:65])

	a.SpendPub, a.ViewPub = &spend, &view

	return a
}

func FromRawAddress(network uint8, spend, view crypto.PublicKey) *Address {
	var nice [69]byte
	nice[0] = network
	copy(nice[1:], spend.AsSlice())
	copy(nice[33:], view.AsSlice())

	//TODO: cache checksum?
	checksum := moneroutil.GetChecksum(nice[:65])
	a := &Address{
		Network:  nice[0],
		checksum: checksum[:],
	}

	a.SpendPub = spend
	a.ViewPub = view

	return a
}

func (a *Address) ToBase58() string {
	return moneroutil.EncodeMoneroBase58([]byte{a.Network}, a.SpendPub.AsSlice(), a.ViewPub.AsSlice(), a.checksum[:])
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
