package utils

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"github.com/jxskiss/base62"
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

func EncodeSliceBinaryNumber(dst, src []byte) []byte {
	if len(dst) < 1+(len(src)*2) {
		return nil
	}
	v := encoding.EncodeToBuf(dst[:0], src)

	if !bytes.ContainsAny(v, "GHIJKLMNOPQRSTUVWXYZghijklmnopqrstuvwxyz") {
		copy(dst[1:], v)
		dst[0] = '.'
		v = dst[:len(v)]
	}

	if len(v) >= (len(src) * 2) {
		hex.Encode(dst, src)
		return dst[:len(src)*2]
	}

	return v
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

func PreviousPowerOfTwo(x uint64) int {
	if x == 0 {
		return 0
	}
	return 1 << (64 - bits.LeadingZeros64(x) - 1)
}

const (
	VarIntLen1 uint64 = 1 << ((iota + 1) * 7)
	VarIntLen2
	VarIntLen3
	VarIntLen4
	VarIntLen5
	VarIntLen6
	VarIntLen7
	VarIntLen8
	VarIntLen9
)

/*

	Checked using this

	var uVarInt64Thresholds [binary.MaxVarintLen64 + 1]uint64

	lastSize := 0
	for i := uint64(1); i > 0 && i < math.MaxUint64; i <<= 1 {
		s := UVarInt64Size(i)
		if s != lastSize {

			n := uVarInt64Thresholds[lastSize]
			ix := sort.Search(int(i-n), func(i int) bool {
				return UVarInt64Size(n+uint64(i)) > lastSize
			})
			uVarInt64Thresholds[s] = n + uint64(ix)
			lastSize = s
		}
	}

	log.Print(uVarInt64Thresholds)

*/

func UVarInt64SliceSize[T uint64 | int](v []T) (n int) {
	for i := range v {
		n += UVarInt64Size(v[i])
	}
	return
}

func UVarInt64Size[T uint64 | int](v T) (n int) {
	x := uint64(v)

	if x < VarIntLen1 {
		return 1
	} else if x < VarIntLen2 {
		return 2
	} else if x < VarIntLen3 {
		return 3
	} else if x < VarIntLen4 {
		return 4
	} else if x < VarIntLen5 {
		return 5
	} else if x < VarIntLen6 {
		return 6
	} else if x < VarIntLen7 {
		return 7
	} else if x < VarIntLen8 {
		return 8
	} else if x < VarIntLen9 {
		return 9
	} else {
		return binary.MaxVarintLen64
	}
}
