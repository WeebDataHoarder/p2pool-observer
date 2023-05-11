package crypto

import (
	"encoding/binary"
	"filippo.io/edwards25519"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"hash"
)

func GetDerivationSharedDataForOutputIndex(derivation PublicKey, outputIndex uint64) PrivateKey {
	var k = derivation.AsBytes()
	var varIntBuf [binary.MaxVarintLen64]byte
	return PrivateKeyFromScalar(HashToScalar(k[:], varIntBuf[:binary.PutUvarint(varIntBuf[:], outputIndex)]))
}

var viewTagDomain = []byte("view_tag")

func GetDerivationViewTagForOutputIndex(derivation PublicKey, outputIndex uint64) uint8 {
	var k = derivation.AsBytes()
	var varIntBuf [binary.MaxVarintLen64]byte
	return PooledKeccak256(viewTagDomain, k[:], varIntBuf[:binary.PutUvarint(varIntBuf[:], outputIndex)])[0]
}

func GetDerivationSharedDataAndViewTagForOutputIndex(derivation PublicKey, outputIndex uint64) (PrivateKey, uint8) {
	var k = derivation.AsBytes()
	var varIntBuf [binary.MaxVarintLen64]byte

	n := binary.PutUvarint(varIntBuf[:], outputIndex)
	pK := PrivateKeyFromScalar(HashToScalar(k[:], varIntBuf[:n]))
	return pK, PooledKeccak256(viewTagDomain, k[:], varIntBuf[:n])[0]
}

// GetDerivationSharedDataAndViewTagForOutputIndexNoAllocate Special version of GetDerivationSharedDataAndViewTagForOutputIndex
func GetDerivationSharedDataAndViewTagForOutputIndexNoAllocate(k PublicKeyBytes, outputIndex uint64, hasher hash.Hash) (edwards25519.Scalar, uint8) {
	var buf [PublicKeySize + binary.MaxVarintLen64]byte
	copy(buf[:], k[:])

	n := binary.PutUvarint(buf[PublicKeySize:], outputIndex)
	var h types.Hash
	hasher.Reset()
	hasher.Write(buf[:PublicKeySize+n])
	HashFastSum(hasher, h[:])
	scReduce32(h[:])

	var c edwards25519.Scalar
	_, _ = c.SetCanonicalBytes(h[:])

	hasher.Reset()
	hasher.Write(viewTagDomain)
	hasher.Write(buf[:PublicKeySize+n])
	HashFastSum(hasher, h[:])

	return c, h[0]
}

func GetKeyImage(pair *KeyPair) PublicKey {
	return PublicKeyFromPoint(HashToPoint(pair.PublicKey)).Multiply(pair.PrivateKey.AsScalar())
}
