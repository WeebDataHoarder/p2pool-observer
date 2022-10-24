package crypto

import (
	"bytes"
	"filippo.io/edwards25519"
	"golang.org/x/exp/rand"
)

// limit = 2^252 + 27742317777372353535851937790883648493.
// limit fits 15 times in 32 bytes (iow, 15 l is the highest multiple of l that fits in 32 bytes)
var limit = []byte{0xe3, 0x6a, 0x67, 0x72, 0x8b, 0xce, 0x13, 0x29, 0x8f, 0x30, 0x82, 0x8c, 0x0b, 0xa4, 0x10, 0x39, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf0}

func RandomScalar() *edwards25519.Scalar {
	buf := make([]byte, 32)
	for {
		if _, err := rand.Read(buf); err != nil {
			return nil
		}

		if bytes.Compare(buf, limit) > 0 {
			continue
		}

		scalar := BytesToScalar(buf)
		if scalar.Equal(edwards25519.NewScalar()) == 0 {
			return scalar
		}
	}
}
