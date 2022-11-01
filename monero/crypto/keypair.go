package crypto

import (
	"filippo.io/edwards25519"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
)

type KeyPair struct {
	PrivateKey *edwards25519.Scalar
	PublicKey  *edwards25519.Point
}

func NewKeyPairFromPrivate(privateKey *edwards25519.Scalar) *KeyPair {
	return &KeyPair{
		PrivateKey: privateKey,
		PublicKey:  (&edwards25519.Point{}).ScalarBaseMult(privateKey),
	}
}

func NewKeyPairFromPrivateHash(privateKey types.Hash) *KeyPair {
	secret, _ := edwards25519.NewScalar().SetCanonicalBytes(privateKey[:])
	return &KeyPair{
		PrivateKey: secret,
		PublicKey:  (&edwards25519.Point{}).ScalarBaseMult(secret),
	}
}
