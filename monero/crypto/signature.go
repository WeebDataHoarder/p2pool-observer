package crypto

import (
	"filippo.io/edwards25519"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
)

type Signature struct {
	C, R *edwards25519.Scalar
}

func NewSignatureFromBytes(buf []byte) *Signature {
	if len(buf) != types.HashSize*2 {
		return nil
	}
	signature := &Signature{}
	var err error
	if signature.C, err = edwards25519.NewScalar().SetCanonicalBytes(buf[:32]); err != nil {
		return nil
	} else if signature.R, err = edwards25519.NewScalar().SetCanonicalBytes(buf[32:]); err != nil {
		return nil
	} else {
		return signature
	}
}

func (s *Signature) Bytes() []byte {
	buf := make([]byte, 0, types.HashSize*2)
	buf = append(buf, s.C.Bytes()...)
	buf = append(buf, s.R.Bytes()...)
	return buf
}

func GenerateSignature(prefixHash types.Hash, keyPair *KeyPair) (signature *Signature) {
	buf := &SignatureComm{}
	buf.Hash = prefixHash
	buf.Key = keyPair.PublicKey
	buf.Comm = &edwards25519.Point{}
	sig := &Signature{
		R: &edwards25519.Scalar{},
	}
	for {
		k := NewKeyPairFromPrivate(RandomScalar())
		buf.Comm = k.PublicKey
		sig.C = HashToScalar(buf.Bytes())
		if sig.C.Equal(zeroScalar) == 1 {
			continue
		}

		sig.R = sig.R.Subtract(k.PrivateKey, edwards25519.NewScalar().Multiply(sig.C, keyPair.PrivateKey))

		if sig.R.Equal(zeroScalar) == 1 {
			continue
		}

		return sig
	}
}

func CheckSignature(prefixHash types.Hash, publicKey *edwards25519.Point, signature *Signature) bool {
	buf := &SignatureComm{}
	buf.Hash = prefixHash
	buf.Key = publicKey
	//get secret r public key
	buf.Comm = (&edwards25519.Point{}).VarTimeDoubleScalarBaseMult(signature.C, publicKey, signature.R)
	if buf.Comm.Equal(infinityPoint) == 1 {
		return false
	}
	c := HashToScalar(buf.Bytes())
	return c.Equal(signature.C) == 1
}
