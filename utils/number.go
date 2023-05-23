package utils

import (
	"encoding/hex"
	"github.com/jxskiss/base62"
	"golang.org/x/exp/constraints"
	"math/bits"
	"strconv"
	"strings"
)

var encoding = base62.NewEncoding("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz")

func DecodeBinaryNumber(i string) uint64 {
	if n, err := strconv.ParseUint(i, 10, 0); strings.Index(i, ".") == -1 && err == nil {
		return n
	}

	if n, err := encoding.ParseUint([]byte(strings.ReplaceAll(i, ".", ""))); err == nil {
		return n
	}

	return 0
}

func EncodeBinaryNumber(n uint64) string {
	v1 := string(encoding.FormatUint(n))
	v2 := strconv.FormatUint(n, 10)

	if !strings.ContainsAny(v1, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz") {
		v1 = "." + v1
	}

	if len(v1) >= len(v2) {
		return v2
	}

	return v1
}

func DecodeHexBinaryNumber(i string) string {
	if _, err := hex.DecodeString(i); strings.Index(i, ".") == -1 && err == nil {
		return i
	}

	if n, err := encoding.Decode([]byte(strings.ReplaceAll(i, ".", ""))); err == nil {
		return hex.EncodeToString(n)
	}

	return ""
}

func EncodeHexBinaryNumber(v2 string) string {
	b, _ := hex.DecodeString(v2)
	v1 := encoding.EncodeToString(b)

	if !strings.ContainsAny(v1, "GHIJKLMNOPQRSTUVWXYZghijklmnopqrstuvwxyz") {
		v1 = "." + v1
	}

	if len(v1) >= len(v2) {
		return v2
	}

	return v1
}

func Min[T constraints.Ordered](v0 T, values ...T) (result T) {
	result = v0
	for _, v := range values {
		if v < result {
			result = v
		}
	}
	return
}

func Max[T constraints.Ordered](v0 T, values ...T) (result T) {
	result = v0
	for _, v := range values {
		if v > result {
			result = v
		}
	}
	return
}

func PreviousPowerOfTwo(x uint64) int {
	if x == 0 {
		return 0
	}
	return 1 << (64 - bits.LeadingZeros64(x) - 1)
}
