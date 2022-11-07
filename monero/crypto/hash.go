package crypto

import (
	"filippo.io/edwards25519"
	"git.gammaspectra.live/P2Pool/moneroutil"
)

func BytesToScalar(buf []byte) *edwards25519.Scalar {
	var bytes [32]byte
	copy(bytes[:], buf[:])
	scReduce32(bytes[:])
	c, _ := edwards25519.NewScalar().SetCanonicalBytes(bytes[:])
	return c
}

func HashToScalar(data ...[]byte) *edwards25519.Scalar {
	h := moneroutil.Keccak256(data...)
	scReduce32(h[:])
	c, _ := edwards25519.NewScalar().SetCanonicalBytes(h[:])
	return c
}

func HashToPoint(publicKey PublicKey) *edwards25519.Point {
	//TODO: make this work with existing edwards25519 library
	input := moneroutil.Key(publicKey.AsBytes())
	var key moneroutil.Key
	(&input).HashToEC().ToBytes(&key)
	p, _ := (&edwards25519.Point{}).SetBytes(key[:])
	return p
}

