package crypto

import (
	"encoding/binary"
)

func GetDerivationSharedDataForOutputIndex(derivation PublicKey, outputIndex uint64) PrivateKey {
	var k = derivation.AsBytes()
	var varIntBuf [binary.MaxVarintLen64]byte
	return PrivateKeyFromScalar(HashToScalar(k[:], varIntBuf[:binary.PutUvarint(varIntBuf[:], outputIndex)]))
}

func GetDerivationViewTagForOutputIndex(derivation PublicKey, outputIndex uint64) uint8 {
	var k = derivation.AsBytes()
	var varIntBuf [binary.MaxVarintLen64]byte
	return PooledKeccak256([]byte("view_tag"), k[:], varIntBuf[:binary.PutUvarint(varIntBuf[:], outputIndex)])[0]
}

func GetDerivationSharedDataAndViewTagForOutputIndex(derivation PublicKey, outputIndex uint64) (PrivateKey, uint8) {
	var k = derivation.AsBytes()
	var varIntBuf [binary.MaxVarintLen64]byte

	n := binary.PutUvarint(varIntBuf[:], outputIndex)
	pK := PrivateKeyFromScalar(HashToScalar(k[:], varIntBuf[:n]))
	return pK, PooledKeccak256([]byte("view_tag"), k[:], varIntBuf[:n])[0]
}

func GetKeyImage(pair *KeyPair) PublicKey {
	return PublicKeyFromPoint(HashToPoint(pair.PublicKey)).Multiply(pair.PrivateKey.AsScalar())
}
