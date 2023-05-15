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
	SpendPub crypto.PublicKeyBytes
	ViewPub  crypto.PublicKeyBytes
	checksum []byte
	// IsSubAddress Always false
	IsSubAddress bool
}

func (a *Address) Compare(b Interface) int {
	//TODO
	return a.ToPackedAddress().Reference().Compare(b)
}

func (a *Address) PublicKeys() (spend, view crypto.PublicKey) {
	return &a.SpendPub, &a.ViewPub
}

func (a *Address) SpendPublicKey() crypto.PublicKey {
	return &a.SpendPub
}

func (a *Address) ViewPublicKey() crypto.PublicKey {
	return &a.ViewPub
}

func (a *Address) ToAddress() *Address {
	return a
}

func (a *Address) ToPackedAddress() PackedAddress {
	return NewPackedAddressFromBytes(a.SpendPub, a.ViewPub)
}

func FromBase58(address string) *Address {
	raw := moneroutil.DecodeMoneroBase58(address)

	if len(raw) != 69 {
		return nil
	}

	if raw[0] != moneroutil.MainNetwork {
		//TODO: support other chains
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

	copy(a.SpendPub[:], raw[1:33])
	copy(a.ViewPub[:], raw[33:65])

	return a
}

func FromRawAddress(network uint8, spend, view crypto.PublicKey) *Address {
	var nice [69]byte
	nice[0] = network
	copy(nice[1:], spend.AsSlice())
	copy(nice[33:], view.AsSlice())

	//TODO: cache checksum?
	checksum := crypto.PooledKeccak256(nice[:65])
	a := &Address{
		Network:  nice[0],
		checksum: checksum[:4],
	}

	a.SpendPub = spend.AsBytes()
	a.ViewPub = view.AsBytes()

	return a
}

func (a *Address) ToBase58() string {
	if a.checksum == nil {
		var nice [69]byte
		nice[0] = a.Network
		copy(nice[1:], a.SpendPub.AsSlice())
		copy(nice[1+crypto.PublicKeySize:], a.ViewPub.AsSlice())
		sum := crypto.PooledKeccak256(nice[:65])
		//this race is ok
		a.checksum = sum[:4]
	}
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
