package sidechain

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/sha3"
)

type NilDerivationCache struct {
}

func (d *NilDerivationCache) Clear() {

}

func (d *NilDerivationCache) GetEphemeralPublicKey(a *address.PackedAddress, _ crypto.PrivateKeySlice, txKeyScalar *crypto.PrivateKeyScalar, outputIndex uint64, hasher *sha3.HasherState) (crypto.PublicKeyBytes, uint8) {
	ephemeralPubKey, viewTag := address.GetEphemeralPublicKeyAndViewTagNoAllocate(a.SpendPublicKey().AsPoint().Point(), address.GetDerivationNoAllocate(a.ViewPublicKey().AsPoint().Point(), txKeyScalar.Scalar()), txKeyScalar.Scalar(), outputIndex, hasher)

	return ephemeralPubKey.AsBytes(), viewTag
}

func (d *NilDerivationCache) GetDeterministicTransactionKey(seed types.Hash, prevId types.Hash) *crypto.KeyPair {
	return crypto.NewKeyPairFromPrivate(address.GetDeterministicTransactionPrivateKey(seed, prevId))
}
