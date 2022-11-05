package crypto

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
)

func GetDeterministicTransactionPrivateKey(spendPublicKey crypto.PublicKey, prevId types.Hash) crypto.PrivateKey {
	entropy := make([]byte, 0, 13+types.HashSize*2)
	entropy = append(entropy, "tx_secret_key"...)
	entropy = append(entropy, spendPublicKey.AsSlice()...)
	entropy = append(entropy, prevId[:]...)
	return crypto.PrivateKeyFromScalar(crypto.DeterministicScalar(entropy))
}
