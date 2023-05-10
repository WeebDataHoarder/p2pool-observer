package crypto

import (
	"encoding/binary"
	"filippo.io/edwards25519"
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

func GetDerivationSharedDataAndViewTagForOutputIndexNoAllocate(derivation PublicKey, outputIndex uint64) (edwards25519.Scalar, uint8) {
	var buf [PublicKeySize + binary.MaxVarintLen64]byte
	var k = derivation.AsBytes()
	copy(buf[:], k[:])

	n := binary.PutUvarint(buf[PublicKeySize:], outputIndex)
	return HashToScalarNoAllocateSingle(buf[:PublicKeySize+n]), Keccak256([]byte("view_tag"), buf[:PublicKeySize+n])[0]
}

func GetKeyImage(pair *KeyPair) PublicKey {
	return PublicKeyFromPoint(HashToPoint(pair.PublicKey)).Multiply(pair.PrivateKey.AsScalar())
}
