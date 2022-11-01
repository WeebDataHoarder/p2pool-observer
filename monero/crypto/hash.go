package crypto

import (
	"filippo.io/edwards25519"
	"git.gammaspectra.live/P2Pool/moneroutil"
)

func BytesToScalar(buf []byte) *edwards25519.Scalar {
	var wideBytes [64]byte
	copy(wideBytes[:], buf[:])
	c, _ := edwards25519.NewScalar().SetUniformBytes(wideBytes[:])
	return c
}

func HashToScalar(data ...[]byte) *edwards25519.Scalar {
	h := moneroutil.Keccak256(data...)
	var wideBytes [64]byte
	copy(wideBytes[:], h[:])
	c, _ := edwards25519.NewScalar().SetUniformBytes(wideBytes[:])
	return c
}

func HashToPoint(data ...[]byte) *edwards25519.Point {
	h := moneroutil.Keccak256(data...)
	p, _ := (&edwards25519.Point{}).SetBytes(h[:])
	return p.ScalarMult(scalar8, p)
}
