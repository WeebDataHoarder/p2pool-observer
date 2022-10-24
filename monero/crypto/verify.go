package crypto

import (
	"filippo.io/edwards25519"
	"git.gammaspectra.live/P2Pool/moneroutil"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
)

type Signature struct {
	C, R *edwards25519.Scalar
}

func NewSignatureFromBytes(buf []byte) *Signature {
	if len(buf) != 64 {
		return nil
	}
	if c, err := edwards25519.NewScalar().SetCanonicalBytes(buf[:32]); err != nil {
		return nil
	} else if r, err := edwards25519.NewScalar().SetCanonicalBytes(buf[32:]); err != nil {
		return nil
	} else {
		return &Signature{
			C: c,
			R: r,
		}
	}
}

var infinity = edwards25519.NewIdentityPoint()

func CheckSignature(hash types.Hash, publicKey *edwards25519.Point, signature *Signature) bool {
	tmp2 := (&edwards25519.Point{}).VarTimeDoubleScalarBaseMult(signature.C, publicKey, signature.R)
	if tmp2.Equal(infinity) == 1 {
		return false
	}

	buf := make([]byte, 0, 16+32+32)
	buf = append(buf, hash[:]...)           //h
	buf = append(buf, publicKey.Bytes()...) //key
	buf = append(buf, tmp2.Bytes()...)      //comm
	c := HashToScalar(types.Hash(moneroutil.Keccak256(buf)))
	return c.Equal(signature.C) == 1
}
