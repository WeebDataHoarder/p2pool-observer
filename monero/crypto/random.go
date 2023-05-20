package crypto

import (
	"crypto/rand"
	"encoding/binary"
	"git.gammaspectra.live/P2Pool/edwards25519"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"unsafe"
)

func RandomScalar() *edwards25519.Scalar {
	buf := make([]byte, 32)
	for {
		if _, err := rand.Read(buf); err != nil {
			return nil
		}

		if !less32(buf, limit) {
			continue
		}

		scalar := BytesToScalar(buf)
		if scalar.Equal(zeroScalar) == 0 {
			return scalar
		}
	}
}

// DeterministicScalar consensus way of generating a deterministic scalar from given entropy
func DeterministicScalar(entropy []byte) *edwards25519.Scalar {

	var counter uint32

	entropy = append(entropy, make([]byte, int(unsafe.Sizeof(counter)))...)
	n := len(entropy) - int(unsafe.Sizeof(counter))
	h := GetKeccak256Hasher()
	defer PutKeccak256Hasher(h)
	var hash types.Hash

	scalar := GetEdwards25519Scalar()

	for {
		h.Reset()
		counter++
		binary.LittleEndian.PutUint32(entropy[n:], counter)
		_, _ = h.Write(entropy)
		HashFastSum(h, hash[:])
		if !less32(hash[:], limit) {
			continue
		}
		scReduce32(hash[:])
		scalar, _ = scalar.SetCanonicalBytes(hash[:])

		if scalar.Equal(zeroScalar) == 0 {
			return scalar
		}
	}
}
