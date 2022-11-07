package crypto

import (
	"encoding/binary"
	"git.gammaspectra.live/P2Pool/moneroutil"
)

func GetDerivationSharedDataForOutputIndex(derivation PublicKey, outputIndex uint64) PrivateKey {
	var k = derivation.AsBytes()
	var varIntBuf [binary.MaxVarintLen64]byte
	return PrivateKeyFromScalar(HashToScalar(k[:], varIntBuf[:binary.PutUvarint(varIntBuf[:], outputIndex)]))
}

func GetDerivationViewTagForOutputIndex(derivation PublicKey, outputIndex uint64) uint8 {
	var k = derivation.AsBytes()
	var varIntBuf [binary.MaxVarintLen64]byte
	h := moneroutil.Keccak256([]byte("view_tag"), k[:], varIntBuf[:binary.PutUvarint(varIntBuf[:], outputIndex)])
	return h[0]
}

func GetDerivationSharedDataAndViewTagForOutputIndex(derivation PublicKey, outputIndex uint64) (PrivateKey, uint8) {
	var k = derivation.AsBytes()
	var varIntBuf [binary.MaxVarintLen64]byte

	n := binary.PutUvarint(varIntBuf[:], outputIndex)
	pK := PrivateKeyFromScalar(HashToScalar(k[:], varIntBuf[:n]))
	h := moneroutil.Keccak256([]byte("view_tag"), k[:], varIntBuf[:n])
	return pK, h[0]
}

func GetKeyImage(pair *KeyPair) PublicKey {
	return PublicKeyFromPoint(HashToPoint(pair.PublicKey)).Multiply(pair.PrivateKey.AsScalar())
}