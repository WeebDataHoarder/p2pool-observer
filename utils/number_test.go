package utils

import (
	"testing"
)

func TestNumber(t *testing.T) {
	s := "S"
	n := uint64(28)

	if DecodeBinaryNumber(s) != n {
		t.Fail()
	}
}

func TestPreviousPowerOfTwo(t *testing.T) {
	loopPath := func(x uint64) int {
		//find closest low power of two
		var cnt uint64
		for cnt = 1; cnt <= x; cnt <<= 1 {
		}
		cnt >>= 1
		return int(cnt)
	}
	for i := uint64(1); i < 65536; i++ {
		if PreviousPowerOfTwo(i) != loopPath(i) {
			t.Fatalf("expected %d, got %d for iteration %d", loopPath(i), PreviousPowerOfTwo(i), i)
		}
	}
}
