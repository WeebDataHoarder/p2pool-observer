package utils

import (
	"encoding/hex"
	"github.com/jxskiss/base62"
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
	if _, err := hex.DecodeString(i); strings.Index(i, ".") == -1 && err != nil {
		return i
	}

	if n, err := encoding.Decode([]byte(strings.ReplaceAll(i, ".", ""))); err == nil {
		return hex.EncodeToString(n)
	}

	return ""
}

func EncodeHexBinaryNumber(v2 string) string {
	b, _ := hex.DecodeString(v2)
	v1 := string(encoding.Encode(b))

	if !strings.ContainsAny(v1, "GHIJKLMNOPQRSTUVWXYZghijklmnopqrstuvwxyz") {
		v1 = "." + v1
	}

	if len(v1) >= len(v2) {
		return v2
	}

	return v1
}
