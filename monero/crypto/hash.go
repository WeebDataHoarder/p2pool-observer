package crypto

import (
	"git.gammaspectra.live/P2Pool/edwards25519"
	"git.gammaspectra.live/P2Pool/moneroutil"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/sha3"
)

func BytesToScalar(buf []byte) *edwards25519.Scalar {
	_ = buf[31] // bounds check hint to compiler; see golang.org/issue/14808
	var bytes [32]byte
	copy(bytes[:], buf[:])
	scReduce32(bytes[:])
	c, _ := GetEdwards25519Scalar().SetCanonicalBytes(bytes[:])
	return c
}

func Keccak256(data ...[]byte) (result types.Hash) {
	h := sha3.NewLegacyKeccak256()
	for _, b := range data {
		h.Write(b)
	}
	HashFastSum(h, result[:])

	return
}

func Keccak256Single(data []byte) (result types.Hash) {
	h := sha3.NewLegacyKeccak256()
	h.Write(data)
	HashFastSum(h, result[:])

	return
}

func HashToScalar(data ...[]byte) *edwards25519.Scalar {
	h := PooledKeccak256(data...)
	scReduce32(h[:])
	c, _ := GetEdwards25519Scalar().SetCanonicalBytes(h[:])
	return c
}

func HashToScalarNoAllocate(data ...[]byte) edwards25519.Scalar {
	h := Keccak256(data...)
	scReduce32(h[:])

	var c edwards25519.Scalar
	_, _ = c.SetCanonicalBytes(h[:])
	return c
}

func HashToScalarNoAllocateSingle(data []byte) edwards25519.Scalar {
	h := Keccak256Single(data)
	scReduce32(h[:])

	var c edwards25519.Scalar
	_, _ = c.SetCanonicalBytes(h[:])
	return c
}

// HashFastSum sha3.Sum clones the state by allocating memory. prevent that. b must be pre-allocated to the expected size, or larger
func HashFastSum(hash *sha3.HasherState, b []byte) []byte {
	_ = b[31] // bounds check hint to compiler; see golang.org/issue/14808
	_, _ = hash.Read(b[:hash.Size()])
	return b
}

func HashToPoint(publicKey PublicKey) *edwards25519.Point {
	//TODO: make this work with existing edwards25519 library
	input := moneroutil.Key(publicKey.AsBytes())
	var key moneroutil.Key
	(&input).HashToEC().ToBytes(&key)
	p, _ := GetEdwards25519Point().SetBytes(key[:])
	return p
}
