package utils

import "testing"

func TestNumber(t *testing.T) {
	s := "S"
	n := uint64(28)

	if DecodeBinaryNumber(s) != n {
		t.Fail()
	}
}
