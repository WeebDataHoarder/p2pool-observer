package crypto

import (
	"crypto/rand"
	"encoding/binary"
	"filippo.io/edwards25519"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"golang.org/x/crypto/sha3"
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

func DeterministicScalar(entropy ...[]byte) *edwards25519.Scalar {

	var counter uint32

	entropy = append(entropy, make([]byte, int(unsafe.Sizeof(counter))))
	h := sha3.NewLegacyKeccak256()
	var hash types.Hash

	scalar := edwards25519.NewScalar()

	for {
		h.Reset()
		counter++
		binary.LittleEndian.PutUint32(entropy[len(entropy)-1], counter)
		for i := range entropy {
			_, _ = h.Write(entropy[i])
		}
		_ = h.Sum(hash[:0])
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
