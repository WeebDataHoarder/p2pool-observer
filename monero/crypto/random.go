package crypto

import (
	"crypto/rand"
	"encoding/binary"
	"filippo.io/edwards25519"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"golang.org/x/crypto/sha3"
	"unsafe"
)

// limit = 2^252 + 27742317777372353535851937790883648493.
// limit fits 15 times in 32 bytes (iow, 15 l is the highest multiple of l that fits in 32 bytes)
var limit = []byte{0xe3, 0x6a, 0x67, 0x72, 0x8b, 0xce, 0x13, 0x29, 0x8f, 0x30, 0x82, 0x8c, 0x0b, 0xa4, 0x10, 0x39, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf0}

// less32 each input must be at least 32 bytes long
func less32(a, b []byte) bool {
	for n := 31; n >= 0; n-- {
		if a[n] < b[n] {
			return true
		} else if a[n] > b[n] {
			return false
		}
	}

	return false
}

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

func DeterministicScalar(entropy []byte) *edwards25519.Scalar {

	var counter uint32

	buf := make([]byte, len(entropy)+int(unsafe.Sizeof(counter)))
	copy(buf, entropy)
	h := sha3.NewLegacyKeccak256()
	hash := make([]byte, types.HashSize*2)

	scalar := edwards25519.NewScalar()

	for {
		h.Reset()
		counter++
		binary.LittleEndian.PutUint32(buf[len(entropy):], counter)
		_, _ = h.Write(buf)
		_ = h.Sum(hash[:0])
		if !less32(hash[:types.HashSize], limit) {
			continue
		}
		scalar, _ = scalar.SetUniformBytes(hash)

		if scalar.Equal(zeroScalar) == 0 {
			return scalar
		}
	}
}
