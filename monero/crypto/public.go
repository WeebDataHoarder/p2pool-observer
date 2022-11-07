package crypto

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"filippo.io/edwards25519"
)

type PublicKey interface {
	AsSlice() PublicKeySlice
	AsBytes() PublicKeyBytes
	AsPoint() *PublicKeyPoint

	String() string
	UnmarshalJSON(b []byte) error
}

const PublicKeySize = 32

type PublicKeyPoint edwards25519.Point

func (k *PublicKeyPoint) AsSlice() PublicKeySlice {
	return k.Point().Bytes()
}

func (k *PublicKeyPoint) AsBytes() (buf PublicKeyBytes) {
	copy(buf[:], k.AsSlice())
	return
}

func (k *PublicKeyPoint) AsPoint() *PublicKeyPoint {
	return k
}

func (k *PublicKeyPoint) Point() *edwards25519.Point {
	return (*edwards25519.Point)(k)
}

func (k *PublicKeyPoint) Add(b *PublicKeyPoint) *PublicKeyPoint {
	return PublicKeyFromPoint(GetEdwards25519Point().Add(k.Point(), b.Point()))
}

func (k *PublicKeyPoint) Subtract(b *PublicKeyPoint) *PublicKeyPoint {
	return PublicKeyFromPoint(GetEdwards25519Point().Subtract(k.Point(), b.Point()))
}

func (k *PublicKeyPoint) Multiply(b *PrivateKeyScalar) *PublicKeyPoint {
	return PublicKeyFromPoint(GetEdwards25519Point().ScalarMult(b.Scalar(), k.Point()))
}

func (k *PublicKeyPoint) Cofactor() *PublicKeyPoint {
	return PublicKeyFromPoint(GetEdwards25519Point().MultByCofactor(k.Point()))
}

func PublicKeyFromPoint(point *edwards25519.Point, _ ...any) *PublicKeyPoint {
	return (*PublicKeyPoint)(point)
}

func (k *PublicKeyPoint) String() string {
	return hex.EncodeToString(k.Point().Bytes())
}

func (k *PublicKeyPoint) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}

	if buf, err := hex.DecodeString(s); err != nil {
		return err
	} else {
		if len(buf) != PublicKeySize {
			return errors.New("wrong hash size")
		}

		if _, err = k.Point().SetBytes(buf); err != nil {
			return err
		}

		return nil
	}
}

type PublicKeyBytes [PublicKeySize]byte

func (k *PublicKeyBytes) AsSlice() PublicKeySlice {
	return (*k)[:]
}

func (k *PublicKeyBytes) AsBytes() PublicKeyBytes {
	return *k
}

func (k *PublicKeyBytes) AsPoint() *PublicKeyPoint {
	return PublicKeyFromPoint(GetEdwards25519Point().SetBytes(k.AsSlice()))
}

func (k *PublicKeyBytes) String() string {
	return hex.EncodeToString(k.AsSlice())
}

func (k *PublicKeyBytes) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}

	if buf, err := hex.DecodeString(s); err != nil {
		return err
	} else {
		if len(buf) != PublicKeySize {
			return errors.New("wrong hash size")
		}

		copy((*k)[:], buf)
		return nil
	}
}

type PublicKeySlice []byte

func (k *PublicKeySlice) AsSlice() PublicKeySlice {
	return *k
}

func (k *PublicKeySlice) AsBytes() (buf PublicKeyBytes) {
	copy(buf[:], *k)
	return buf
}

func (k *PublicKeySlice) AsPoint() *PublicKeyPoint {
	return PublicKeyFromPoint(GetEdwards25519Point().SetBytes(*k))
}

func (k *PublicKeySlice) String() string {
	return hex.EncodeToString(*k)
}

func (k *PublicKeySlice) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}

	if buf, err := hex.DecodeString(s); err != nil {
		return err
	} else {
		if len(buf) != PublicKeySize {
			return errors.New("wrong hash size")
		}

		*k = buf
		return nil
	}
}
