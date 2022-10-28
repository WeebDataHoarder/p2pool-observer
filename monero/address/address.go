package address

import (
	"bytes"
	"encoding/json"
	"errors"
	"filippo.io/edwards25519"
	"git.gammaspectra.live/P2Pool/moneroutil"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"sync/atomic"
)

type Address struct {
	Network  uint8
	SpendPub edwards25519.Point
	ViewPub  edwards25519.Point
	Checksum []byte

	moneroAddress atomic.Pointer[edwards25519.Scalar]
}

var scalar8, _ = edwards25519.NewScalar().SetCanonicalBytes([]byte{8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})

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
		Checksum: checksum[:],
	}

	if _, err := a.SpendPub.SetBytes(raw[1:33]); err != nil {
		return nil
	}
	if _, err := a.ViewPub.SetBytes(raw[33:65]); err != nil {
		return nil
	}

	return a
}

func FromRawAddress(network uint8, spend, view types.Hash) *Address {
	nice := make([]byte, 69)
	nice[0] = network
	copy(nice[1:], spend[:])
	copy(nice[33:], view[:])

	checksum := moneroutil.GetChecksum(nice[:65])
	a := &Address{
		Network:  nice[0],
		Checksum: checksum[:],
	}

	if _, err := a.SpendPub.SetBytes(nice[1:33]); err != nil {
		return nil
	}
	if _, err := a.ViewPub.SetBytes(nice[33:65]); err != nil {
		return nil
	}

	return a
}

func (a *Address) ToBase58() string {
	return moneroutil.EncodeMoneroBase58([]byte{a.Network}, a.SpendPub.Bytes(), a.ViewPub.Bytes(), a.Checksum[:])
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
		a.Checksum = addr.Checksum
		return nil
	} else {
		return errors.New("invalid address")
	}
}
