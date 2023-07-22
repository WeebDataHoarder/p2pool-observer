package address

import (
	"bytes"
	"errors"
	"git.gammaspectra.live/P2Pool/moneroutil"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"slices"
)

type Address struct {
	SpendPub    crypto.PublicKeyBytes
	ViewPub     crypto.PublicKeyBytes
	Network     uint8
	hasChecksum bool
	checksum    moneroutil.Checksum
}

func (a *Address) Compare(b Interface) int {
	//compare spend key

	resultSpendKey := crypto.CompareConsensusPublicKeyBytes(&a.SpendPub, b.SpendPublicKey())
	if resultSpendKey != 0 {
		return resultSpendKey
	}

	// compare view key
	return crypto.CompareConsensusPublicKeyBytes(&a.ViewPub, b.ViewPublicKey())
}

func (a *Address) PublicKeys() (spend, view crypto.PublicKey) {
	return &a.SpendPub, &a.ViewPub
}

func (a *Address) SpendPublicKey() *crypto.PublicKeyBytes {
	return &a.SpendPub
}

func (a *Address) ViewPublicKey() *crypto.PublicKeyBytes {
	return &a.ViewPub
}

func (a *Address) ToAddress(network uint8, err ...error) *Address {
	if a.Network != network || (len(err) > 0 && err[0] != nil) {
		return nil
	}
	return a
}

func (a *Address) ToPackedAddress() PackedAddress {
	return NewPackedAddressFromBytes(a.SpendPub, a.ViewPub)
}

func FromBase58(address string) *Address {
	preAllocatedBuf := make([]byte, 0, 69)
	raw := moneroutil.DecodeMoneroBase58PreAllocated(preAllocatedBuf, []byte(address))

	if len(raw) != 69 {
		return nil
	}

	switch raw[0] {
	case moneroutil.MainNetwork, moneroutil.TestNetwork, moneroutil.StageNetwork:
		break
	case moneroutil.IntegratedMainNetwork, moneroutil.IntegratedTestNetwork, moneroutil.IntegratedStageNetwork:
		return nil
	case moneroutil.SubAddressMainNetwork, moneroutil.SubAddressTestNetwork, moneroutil.SubAddressStageNetwork:
		return nil
	default:
		return nil
	}

	checksum := moneroutil.GetChecksum(raw[:65])
	if bytes.Compare(checksum[:], raw[65:]) != 0 {
		return nil
	}
	a := &Address{
		Network:     raw[0],
		checksum:    checksum,
		hasChecksum: true,
	}

	copy(a.SpendPub[:], raw[1:33])
	copy(a.ViewPub[:], raw[33:65])

	return a
}

func FromBase58NoChecksumCheck(address []byte) *Address {
	preAllocatedBuf := make([]byte, 0, 69)
	raw := moneroutil.DecodeMoneroBase58PreAllocated(preAllocatedBuf, address)

	if len(raw) != 69 {
		return nil
	}

	switch raw[0] {
	case moneroutil.MainNetwork, moneroutil.TestNetwork, moneroutil.StageNetwork:
		break
	case moneroutil.IntegratedMainNetwork, moneroutil.IntegratedTestNetwork, moneroutil.IntegratedStageNetwork:
		return nil
	case moneroutil.SubAddressMainNetwork, moneroutil.SubAddressTestNetwork, moneroutil.SubAddressStageNetwork:
		return nil
	default:
		return nil
	}

	a := &Address{
		Network: raw[0],
	}
	copy(a.checksum[:], slices.Clone(raw[65:]))
	a.hasChecksum = true

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
		Network: nice[0],
	}
	copy(a.checksum[:], checksum[:4])
	a.hasChecksum = true

	a.SpendPub = spend.AsBytes()
	a.ViewPub = view.AsBytes()

	return a
}

func (a *Address) verifyChecksum() {
	if !a.hasChecksum {
		var nice [69]byte
		nice[0] = a.Network
		copy(nice[1:], a.SpendPub.AsSlice())
		copy(nice[1+crypto.PublicKeySize:], a.ViewPub.AsSlice())
		sum := crypto.PooledKeccak256(nice[:65])
		//this race is ok
		copy(a.checksum[:], sum[:4])
		a.hasChecksum = true
	}
}

func (a *Address) ToBase58() []byte {
	a.verifyChecksum()
	buf := make([]byte, 0, 95)
	return moneroutil.EncodeMoneroBase58PreAllocated(buf, []byte{a.Network}, a.SpendPub.AsSlice(), a.ViewPub.AsSlice(), a.checksum[:])
}

func (a *Address) MarshalJSON() ([]byte, error) {
	a.verifyChecksum()
	buf := make([]byte, 95+2)
	buf[0] = '"'
	moneroutil.EncodeMoneroBase58PreAllocated(buf[1:1], []byte{a.Network}, a.SpendPub.AsSlice(), a.ViewPub.AsSlice(), a.checksum[:])
	buf[len(buf)-1] = '"'
	return buf, nil
}

func (a *Address) UnmarshalJSON(b []byte) error {
	if len(b) < 2 {
		return errors.New("unsupported length")
	}

	if addr := FromBase58NoChecksumCheck(b[1 : len(b)-1]); addr != nil {
		a.Network = addr.Network
		a.SpendPub = addr.SpendPub
		a.ViewPub = addr.ViewPub
		a.checksum = addr.checksum
		a.hasChecksum = addr.hasChecksum
		return nil
	} else {
		return errors.New("invalid address")
	}
}
