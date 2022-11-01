package crypto

import (
	"encoding/binary"
	"filippo.io/edwards25519"
	"git.gammaspectra.live/P2Pool/moneroutil"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
)

func GetDerivationForPrivateKey(publicKey *edwards25519.Point, privateKey *edwards25519.Scalar) *edwards25519.Point {
	point := (&edwards25519.Point{}).ScalarMult(privateKey, publicKey)
	return (&edwards25519.Point{}).ScalarMult(scalar8, point)
}

func GetDerivationSharedDataForOutputIndex(derivation *edwards25519.Point, outputIndex uint64) *edwards25519.Scalar {
	varIntBuf := make([]byte, binary.MaxVarintLen64)
	data := append(derivation.Bytes(), varIntBuf[:binary.PutUvarint(varIntBuf, outputIndex)]...)
	return HashToScalar(data)
}

func GetDerivationViewTagForOutputIndex(derivation *edwards25519.Point, outputIndex uint64) uint8 {
	buf := make([]byte, 8+types.HashSize+binary.MaxVarintLen64)
	copy(buf, "view_tag")
	copy(buf[8:], derivation.Bytes())
	binary.PutUvarint(buf[8+types.HashSize:], outputIndex)

	h := moneroutil.Keccak256(buf)
	return h[0]
}
