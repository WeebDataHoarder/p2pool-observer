package address

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"filippo.io/edwards25519"
	"git.gammaspectra.live/P2Pool/moneroutil"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"golang.org/x/crypto/sha3"
	"strings"
)

type Address struct {
	Network  uint8
	SpendPub edwards25519.Point
	ViewPub  edwards25519.Point
	Checksum []byte
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

func (a *Address) GetDerivationForPrivateKey(privateKey *edwards25519.Scalar) *edwards25519.Point {
	point := (&edwards25519.Point{}).ScalarMult(privateKey, &a.ViewPub)
	return (&edwards25519.Point{}).ScalarMult(scalar8, point)
}

func GetDerivationSharedDataForOutputIndex(derivation *edwards25519.Point, outputIndex uint64) *edwards25519.Scalar {
	varIntBuf := make([]byte, binary.MaxVarintLen64)
	data := append(derivation.Bytes(), varIntBuf[:binary.PutUvarint(varIntBuf, outputIndex)]...)
	hasher := sha3.NewLegacyKeccak256()
	if _, err := hasher.Write(data); err != nil {
		return nil
	}

	var wideBytes [64]byte
	copy(wideBytes[:], hasher.Sum([]byte{}))
	scalar, _ := edwards25519.NewScalar().SetUniformBytes(wideBytes[:])

	return scalar
}

func (a *Address) GetPublicKeyForSharedData(sharedData *edwards25519.Scalar) *edwards25519.Point {
	sG := (&edwards25519.Point{}).ScalarBaseMult(sharedData)

	return (&edwards25519.Point{}).Add(sG, &a.SpendPub)

}

func (a *Address) GetEphemeralPublicKey(privateKey types.Hash, outputIndex uint64) (result types.Hash) {
	pK, _ := edwards25519.NewScalar().SetCanonicalBytes(privateKey[:])
	copy(result[:], a.GetPublicKeyForSharedData(GetDerivationSharedDataForOutputIndex(a.GetDerivationForPrivateKey(pK), outputIndex)).Bytes())

	return
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

type SignatureVerifyResult int

const (
	ResultFail SignatureVerifyResult = iota
	ResultSuccessSpend
	ResultSuccessView
)

func (a *Address) getMessageHash(message []byte, mode uint8) types.Hash {
	buf := make([]byte, 0)
	buf = append(buf, []byte("MoneroMessageSignature")...)
	buf = append(buf, []byte{0}...) //null byte for previous string
	buf = append(buf, a.SpendPub.Bytes()...)
	buf = append(buf, a.ViewPub.Bytes()...)
	buf = append(buf, []byte{mode}...) //mode, 0 = sign with spend key, 1 = sign with view key
	buf = binary.AppendUvarint(buf, uint64(len(message)))
	buf = append(buf, message...)
	return types.Hash(moneroutil.Keccak256(buf))
}

func (a *Address) Verify(message []byte, signature string) SignatureVerifyResult {
	var hash types.Hash

	if strings.HasPrefix(signature, "SigV1") {
		hash = types.Hash(moneroutil.Keccak256(message))
	} else if strings.HasPrefix(signature, "SigV2") {
		hash = a.getMessageHash(message, 0)
	} else {
		return ResultFail
	}
	raw := moneroutil.DecodeMoneroBase58(signature[5:])

	sig := crypto.NewSignatureFromBytes(raw)

	if sig == nil {
		return ResultFail
	}

	if crypto.CheckSignature(hash, &a.SpendPub, sig) {
		return ResultSuccessSpend
	}

	if strings.HasPrefix(signature, "SigV2") {
		hash = a.getMessageHash(message, 1)
	}

	if crypto.CheckSignature(hash, &a.ViewPub, sig) {
		return ResultSuccessView
	}

	return ResultFail
}
