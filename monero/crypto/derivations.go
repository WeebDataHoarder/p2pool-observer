package crypto

import (
	"encoding/binary"
	"git.gammaspectra.live/P2Pool/moneroutil"
)

func GetDerivationSharedDataForOutputIndex(derivation PublicKey, outputIndex uint64) PrivateKey {
	return PrivateKeyFromScalar(HashToScalar(derivation.AsSlice(), binary.AppendUvarint(nil, outputIndex)))
}

func GetDerivationViewTagForOutputIndex(derivation PublicKey, outputIndex uint64) uint8 {
	h := moneroutil.Keccak256([]byte("view_tag"), derivation.AsSlice(), binary.AppendUvarint(nil, outputIndex))
	return h[0]
}
