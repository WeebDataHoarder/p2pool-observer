package crypto

import (
	"bytes"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"git.gammaspectra.live/P2Pool/edwards25519"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
)

type PublicKey interface {
	AsSlice() PublicKeySlice
	AsBytes() PublicKeyBytes
	AsPoint() *PublicKeyPoint

	String() string
	UnmarshalJSON(b []byte) error
	MarshalJSON() ([]byte, error)
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
	if err := utils.UnmarshalJSON(b, &s); err != nil {
		return err
	}

	if buf, err := hex.DecodeString(s); err != nil {
		return err
	} else {
		if len(buf) != PublicKeySize {
			return errors.New("wrong key size")
		}

		if _, err = k.Point().SetBytes(buf); err != nil {
			return err
		}

		return nil
	}
}

func (k *PublicKeyPoint) MarshalJSON() ([]byte, error) {
	return utils.MarshalJSON(k.String())
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

func (k *PublicKeyBytes) Scan(src any) error {
	if src == nil {
		return nil
	} else if buf, ok := src.([]byte); ok {
		if len(buf) == 0 {
			return nil
		}
		if len(buf) != PublicKeySize {
			return errors.New("invalid key size")
		}
		copy((*k)[:], buf)

		return nil
	}
	return errors.New("invalid type")
}

func (k *PublicKeyBytes) Value() (driver.Value, error) {
	var zeroPubKey PublicKeyBytes
	if *k == zeroPubKey {
		return nil, nil
	}
	return []byte((*k)[:]), nil
}

func (k *PublicKeyBytes) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || len(b) == 2 {
		return nil
	}

	if len(b) != PublicKeySize*2+2 {
		return errors.New("wrong key size")
	}

	if _, err := hex.Decode(k[:], b[1:len(b)-1]); err != nil {
		return err
	} else {
		return nil
	}
}

func (k *PublicKeyBytes) MarshalJSON() ([]byte, error) {
	var buf [PublicKeySize*2 + 2]byte
	buf[0] = '"'
	buf[PublicKeySize*2+1] = '"'
	hex.Encode(buf[1:], k[:])
	return buf[:], nil
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

func (k *PublicKeySlice) Scan(src any) error {
	if src == nil {
		return nil
	} else if buf, ok := src.([]byte); ok {
		if len(buf) == 0 {
			return nil
		}
		if len(buf) != PublicKeySize {
			return errors.New("invalid key size")
		}
		copy(*k, buf)

		return nil
	}
	return errors.New("invalid type")
}

func (k *PublicKeySlice) Value() (driver.Value, error) {
	var zeroPubKey PublicKeyBytes
	if bytes.Compare(*k, zeroPubKey[:]) == 0 {
		return nil, nil
	}
	return []byte(*k), nil
}

func (k *PublicKeySlice) UnmarshalJSON(b []byte) error {
	var s string
	if err := utils.UnmarshalJSON(b, &s); err != nil {
		return err
	}

	if buf, err := hex.DecodeString(s); err != nil {
		return err
	} else {
		if len(buf) != PublicKeySize {
			return errors.New("wrong key size")
		}

		*k = buf
		return nil
	}
}

func (k *PublicKeySlice) MarshalJSON() ([]byte, error) {
	var buf [PublicKeySize*2 + 2]byte
	buf[0] = '"'
	buf[PublicKeySize*2+1] = '"'
	hex.Encode(buf[1:], (*k)[:])
	return buf[:], nil
}
