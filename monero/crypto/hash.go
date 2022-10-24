package crypto

import (
	"filippo.io/edwards25519"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
)

func BytesToScalar(hash []byte) *edwards25519.Scalar {
	var wideBytes [64]byte
	copy(wideBytes[:], hash[:])
	c, _ := edwards25519.NewScalar().SetUniformBytes(wideBytes[:])
	return c
}

func HashToScalar(hash types.Hash) *edwards25519.Scalar {
	var wideBytes [64]byte
	copy(wideBytes[:], hash[:])
	c, _ := edwards25519.NewScalar().SetUniformBytes(wideBytes[:])
	return c
}
