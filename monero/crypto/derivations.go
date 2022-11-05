package crypto

import (
	"encoding/binary"
	"git.gammaspectra.live/P2Pool/moneroutil"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
)

func GetDerivationSharedDataForOutputIndex(derivation PublicKey, outputIndex uint64) PrivateKey {
	varIntBuf := make([]byte, binary.MaxVarintLen64)
	data := append(derivation.AsSlice(), varIntBuf[:binary.PutUvarint(varIntBuf, outputIndex)]...)
	return PrivateKeyFromScalar(HashToScalar(data))
}

func GetDerivationViewTagForOutputIndex(derivation PublicKey, outputIndex uint64) uint8 {
	buf := make([]byte, 8+types.HashSize+binary.MaxVarintLen64)
	copy(buf, "view_tag")
	copy(buf[8:], derivation.AsSlice())
	binary.PutUvarint(buf[8+types.HashSize:], outputIndex)

	h := moneroutil.Keccak256(buf)
	return h[0]
}
