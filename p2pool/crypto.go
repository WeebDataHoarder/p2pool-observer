package p2pool

import (
	"filippo.io/edwards25519"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
)

func GetDeterministicTransactionPrivateKey(spendPublicKey *edwards25519.Point, prevId types.Hash) *edwards25519.Scalar {
	entropy := make([]byte, 0, 13+types.HashSize*2)
	entropy = append(entropy, "tx_secret_key"...)
	entropy = append(entropy, spendPublicKey.Bytes()...)
	entropy = append(entropy, prevId[:]...)
	return crypto.DeterministicScalar(entropy)
}
