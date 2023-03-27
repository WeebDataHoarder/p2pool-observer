package crypto

import (
	"bytes"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"errors"
	"filippo.io/edwards25519"
)

type PrivateKey interface {
	AsSlice() PrivateKeySlice
	AsBytes() PrivateKeyBytes
	AsScalar() *PrivateKeyScalar

	PublicKey() PublicKey

	// GetDerivation derives a secret via a peer PublicKey, ECDH
	GetDerivation(public PublicKey) PublicKey

	// GetDerivationCofactor derives a secret via a peer PublicKey, ECDH, making sure it is in the proper range (*8)
	GetDerivationCofactor(public PublicKey) PublicKey

	String() string
	UnmarshalJSON(b []byte) error
	MarshalJSON() ([]byte, error)
}

const PrivateKeySize = 32

type PrivateKeyScalar edwards25519.Scalar

func (p *PrivateKeyScalar) AsSlice() PrivateKeySlice {
	return p.Scalar().Bytes()
}

func (p *PrivateKeyScalar) AsBytes() (buf PrivateKeyBytes) {
	copy(buf[:], p.AsSlice())
	return
}

func (p *PrivateKeyScalar) AsScalar() *PrivateKeyScalar {
	return p
}

func PrivateKeyFromScalar(scalar *edwards25519.Scalar) *PrivateKeyScalar {
	return (*PrivateKeyScalar)(scalar)
}

func (p *PrivateKeyScalar) Scalar() *edwards25519.Scalar {
	return (*edwards25519.Scalar)(p)
}

func (p *PrivateKeyScalar) PublicKey() PublicKey {
	return PublicKeyFromPoint(GetEdwards25519Point().ScalarBaseMult(p.Scalar()))
}

func (p *PrivateKeyScalar) GetDerivation(public PublicKey) PublicKey {
	return deriveKeyExchangeSecret(p, public.AsPoint())
}

func (p *PrivateKeyScalar) GetDerivationCofactor(public PublicKey) PublicKey {
	return deriveKeyExchangeSecretCofactor(p, public.AsPoint())
}
func (p *PrivateKeyScalar) String() string {
	return hex.EncodeToString(p.Scalar().Bytes())
}

func (p *PrivateKeyScalar) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}

	if buf, err := hex.DecodeString(s); err != nil {
		return err
	} else {
		if len(buf) != PrivateKeySize {
			return errors.New("wrong key size")
		}

		if _, err = p.Scalar().SetCanonicalBytes(buf); err != nil {
			return err
		}

		return nil
	}
}

func (p *PrivateKeyScalar) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.String())
}

type PrivateKeyBytes [PrivateKeySize]byte

func (k *PrivateKeyBytes) AsSlice() PrivateKeySlice {
	return (*k)[:]
}

func (k *PrivateKeyBytes) AsBytes() PrivateKeyBytes {
	return *k
}

func (k *PrivateKeyBytes) AsScalar() *PrivateKeyScalar {
	secret, _ := GetEdwards25519Scalar().SetCanonicalBytes((*k)[:])
	return PrivateKeyFromScalar(secret)
}

func (k *PrivateKeyBytes) PublicKey() PublicKey {
	return PublicKeyFromPoint(GetEdwards25519Point().ScalarBaseMult(k.AsScalar().Scalar()))
}

func (k *PrivateKeyBytes) GetDerivation(public PublicKey) PublicKey {
	return k.AsScalar().GetDerivation(public)
}

func (k *PrivateKeyBytes) GetDerivationCofactor(public PublicKey) PublicKey {
	return k.AsScalar().GetDerivationCofactor(public)
}

func (k *PrivateKeyBytes) String() string {
	return hex.EncodeToString(k.AsSlice())
}

func (k *PrivateKeyBytes) Scan(src any) error {
	if src == nil {
		return nil
	} else if buf, ok := src.([]byte); ok {
		if len(buf) == 0 {
			return nil
		}
		if len(buf) != PrivateKeySize {
			return errors.New("invalid key size")
		}
		copy((*k)[:], buf)

		return nil
	}
	return errors.New("invalid type")
}

func (k *PrivateKeyBytes) Value() (driver.Value, error) {
	var zeroPrivKey PrivateKeyBytes
	if *k == zeroPrivKey {
		return nil, nil
	}
	return []byte((*k)[:]), nil
}

func (k *PrivateKeyBytes) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}

	if buf, err := hex.DecodeString(s); err != nil {
		return err
	} else {
		if len(buf) != PrivateKeySize {
			return errors.New("wrong key size")
		}

		copy((*k)[:], buf)
		return nil
	}
}

func (k *PrivateKeyBytes) MarshalJSON() ([]byte, error) {
	return json.Marshal(k.String())
}

type PrivateKeySlice []byte

func (k *PrivateKeySlice) AsSlice() PrivateKeySlice {
	return *k
}

func (k *PrivateKeySlice) AsBytes() (buf PrivateKeyBytes) {
	copy(buf[:], *k)
	return
}

func (k *PrivateKeySlice) AsScalar() *PrivateKeyScalar {
	secret, _ := GetEdwards25519Scalar().SetCanonicalBytes(*k)
	return PrivateKeyFromScalar(secret)
}

func (k *PrivateKeySlice) PublicKey() PublicKey {
	return PublicKeyFromPoint(GetEdwards25519Point().ScalarBaseMult(k.AsScalar().Scalar()))
}

func (k *PrivateKeySlice) GetDerivation(public PublicKey) PublicKey {
	return k.AsScalar().GetDerivation(public)
}

func (k *PrivateKeySlice) GetDerivationCofactor(public PublicKey) PublicKey {
	return k.AsScalar().GetDerivationCofactor(public)
}

func (k *PrivateKeySlice) String() string {
	return hex.EncodeToString(*k)
}

func (k *PrivateKeySlice) Scan(src any) error {
	if src == nil {
		return nil
	} else if buf, ok := src.([]byte); ok {
		if len(buf) == 0 {
			return nil
		}
		if len(buf) != PrivateKeySize {
			return errors.New("invalid key size")
		}
		copy(*k, buf)

		return nil
	}
	return errors.New("invalid type")
}

func (k *PrivateKeySlice) Value() (driver.Value, error) {
	var zeroPrivKey PublicKeyBytes
	if bytes.Compare(*k, zeroPrivKey[:]) == 0 {
		return nil, nil
	}
	return []byte(*k), nil
}

func (k *PrivateKeySlice) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}

	if buf, err := hex.DecodeString(s); err != nil {
		return err
	} else {
		if len(buf) != PrivateKeySize {
			return errors.New("wrong key size")
		}

		*k = buf
		return nil
	}
}

func (k *PrivateKeySlice) MarshalJSON() ([]byte, error) {
	return json.Marshal(k.String())
}

func deriveKeyExchangeSecretCofactor(private *PrivateKeyScalar, public *PublicKeyPoint) *PublicKeyPoint {
	return public.Multiply(private).Cofactor()
}

func deriveKeyExchangeSecret(private *PrivateKeyScalar, public *PublicKeyPoint) *PublicKeyPoint {
	return public.Multiply(private)
}
