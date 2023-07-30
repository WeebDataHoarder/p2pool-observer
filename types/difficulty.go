package types

import (
	"bytes"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"github.com/holiman/uint256"
	fasthex "github.com/tmthrgd/go-hex"
	"io"
	"lukechampine.com/uint128"
	"math"
	"math/big"
	"math/bits"
	"strconv"
	"strings"
)

const DifficultySize = 16

var ZeroDifficulty = Difficulty(uint128.Zero)
var MaxDifficulty = Difficulty(uint128.Max)

type Difficulty uint128.Uint128

func (d Difficulty) IsZero() bool {
	return uint128.Uint128(d).IsZero()
}

func (d Difficulty) Equals(v Difficulty) bool {
	return uint128.Uint128(d).Equals(uint128.Uint128(v))
}

func (d Difficulty) Equals64(v uint64) bool {
	return uint128.Uint128(d).Equals64(v)
}

func (d Difficulty) Cmp(v Difficulty) int {
	//return uint128.Uint128(d).Cmp(uint128.Uint128(v))
	if d == v {
		return 0
	} else if d.Hi < v.Hi || (d.Hi == v.Hi && d.Lo < v.Lo) {
		return -1
	} else {
		return 1
	}
}

func (d Difficulty) Cmp64(v uint64) int {
	return uint128.Uint128(d).Cmp64(v)
}

func (d Difficulty) And(v Difficulty) Difficulty {
	return Difficulty(uint128.Uint128(d).And(uint128.Uint128(v)))
}

func (d Difficulty) And64(v uint64) Difficulty {
	return Difficulty(uint128.Uint128(d).And64(v))
}

func (d Difficulty) Or(v Difficulty) Difficulty {
	return Difficulty(uint128.Uint128(d).Or(uint128.Uint128(v)))
}

func (d Difficulty) Or64(v uint64) Difficulty {
	return Difficulty(uint128.Uint128(d).Or64(v))
}

func (d Difficulty) Xor(v Difficulty) Difficulty {
	return Difficulty(uint128.Uint128(d).Xor(uint128.Uint128(v)))
}

func (d Difficulty) Xor64(v uint64) Difficulty {
	return Difficulty(uint128.Uint128(d).Xor64(v))
}

// Add All calls can wrap
func (d Difficulty) Add(v Difficulty) Difficulty {
	//return Difficulty(uint128.Uint128(d).AddWrap(uint128.Uint128(v)))
	lo, carry := bits.Add64(d.Lo, v.Lo, 0)
	hi, _ := bits.Add64(d.Hi, v.Hi, carry)
	return Difficulty{Lo: lo, Hi: hi}
}

// Add64 All calls can wrap
func (d Difficulty) Add64(v uint64) Difficulty {
	return Difficulty(uint128.Uint128(d).AddWrap64(v))
}

// Sub All calls can wrap
func (d Difficulty) Sub(v Difficulty) Difficulty {
	return Difficulty(uint128.Uint128(d).SubWrap(uint128.Uint128(v)))
}

// Sub64 All calls can wrap
func (d Difficulty) Sub64(v uint64) Difficulty {
	return Difficulty(uint128.Uint128(d).SubWrap64(v))
}

// Mul All calls can wrap
func (d Difficulty) Mul(v Difficulty) Difficulty {
	return Difficulty(uint128.Uint128(d).MulWrap(uint128.Uint128(v)))
}

// Mul64 All calls can wrap
func (d Difficulty) Mul64(v uint64) Difficulty {
	//return Difficulty(uint128.Uint128(d).MulWrap64(v))
	hi, lo := bits.Mul64(d.Lo, v)
	hi += d.Hi * v
	return Difficulty{Lo: lo, Hi: hi}
}

func (d Difficulty) Div(v Difficulty) Difficulty {
	return Difficulty(uint128.Uint128(d).Div(uint128.Uint128(v)))
}

func (d Difficulty) Div64(v uint64) Difficulty {
	return Difficulty(uint128.Uint128(d).Div64(v))
}

func (d Difficulty) QuoRem(v Difficulty) (q, r Difficulty) {
	qq, rr := uint128.Uint128(d).QuoRem(uint128.Uint128(v))
	return Difficulty(qq), Difficulty(rr)
}

func (d Difficulty) QuoRem64(v uint64) (q Difficulty, r uint64) {
	qq, rr := uint128.Uint128(d).QuoRem64(v)
	return Difficulty(qq), rr
}

func (d Difficulty) Mod(v Difficulty) (r Difficulty) {
	return Difficulty(uint128.Uint128(d).Mod(uint128.Uint128(v)))
}

func (d Difficulty) Mod64(v uint64) (r uint64) {
	return uint128.Uint128(d).Mod64(v)
}

func (d Difficulty) Lsh(n uint) (s Difficulty) {
	return Difficulty(uint128.Uint128(d).Lsh(n))
}

func (d Difficulty) Rsh(n uint) (s Difficulty) {
	return Difficulty(uint128.Uint128(d).Rsh(n))
}

func (d Difficulty) LeadingZeros() int {
	return uint128.Uint128(d).LeadingZeros()
}

func (d Difficulty) TrailingZeros() int {
	return uint128.Uint128(d).TrailingZeros()
}

func (d Difficulty) OnesCount() int {
	return uint128.Uint128(d).OnesCount()
}

func (d Difficulty) RotateLeft(k int) Difficulty {
	return Difficulty(uint128.Uint128(d).RotateLeft(k))
}

func (d Difficulty) RotateRight(k int) Difficulty {
	return Difficulty(uint128.Uint128(d).RotateRight(k))
}

func (d Difficulty) Reverse() Difficulty {
	return Difficulty(uint128.Uint128(d).Reverse())
}

func (d Difficulty) ReverseBytes() Difficulty {
	return Difficulty(uint128.Uint128(d).ReverseBytes())
}

func (d Difficulty) Len() int {
	return uint128.Uint128(d).Len()
}

func (d Difficulty) PutBytes(b []byte) {
	uint128.Uint128(d).PutBytes(b)
}

func (d Difficulty) PutBytesBE(b []byte) {
	uint128.Uint128(d).PutBytesBE(b)
}

// Big returns u as a *big.Int.
func (d Difficulty) Big() *big.Int {
	return uint128.Uint128(d).Big()
}

func (d Difficulty) MarshalJSON() ([]byte, error) {
	if d.Hi == 0 {
		return []byte(strconv.FormatUint(d.Lo, 10)), nil
	}

	var encodeBuf [DifficultySize]byte
	d.PutBytesBE(encodeBuf[:])

	var buf [DifficultySize*2 + 2]byte
	buf[0] = '"'
	buf[DifficultySize*2+1] = '"'
	fasthex.Encode(buf[1:], encodeBuf[:])
	return buf[:], nil
}

func MustDifficultyFromString(s string) Difficulty {
	if d, err := DifficultyFromString(s); err != nil {
		panic(err)
	} else {
		return d
	}
}

func DifficultyFromString(s string) (Difficulty, error) {
	if strings.HasPrefix(s, "0x") {
		if buf, err := hex.DecodeString(s[2:]); err != nil {
			return ZeroDifficulty, err
		} else {
			//TODO: check this
			var d [DifficultySize]byte
			copy(d[DifficultySize-len(buf):], buf)
			return DifficultyFromBytes(d[:]), nil
		}
	} else {
		if buf, err := hex.DecodeString(s); err != nil {
			return ZeroDifficulty, err
		} else {
			if len(buf) != DifficultySize {
				return ZeroDifficulty, errors.New("wrong difficulty size")
			}

			return DifficultyFromBytes(buf), nil
		}
	}
}

func DifficultyFromBytes(buf []byte) Difficulty {
	return Difficulty(uint128.FromBytesBE(buf))
}

func NewDifficulty(lo, hi uint64) Difficulty {
	return Difficulty{Lo: lo, Hi: hi}
}

func DifficultyFrom64(v uint64) Difficulty {
	return NewDifficulty(v, 0)
}

func (d *Difficulty) UnmarshalJSON(b []byte) error {
	if len(b) == 0 {
		return io.ErrUnexpectedEOF
	}

	if b[0] == '"' {
		if len(b) < 2 || (len(b)%2) != 0 || b[len(b)-1] != '"' {
			return errors.New("invalid bytes")
		}

		if len(b) == DifficultySize*2+2 {
			// fast path
			var buf [DifficultySize]byte
			if _, err := fasthex.Decode(buf[:], b[1:len(b)-1]); err != nil {
				return err
			} else {
				*d = DifficultyFromBytes(buf[:])
				return nil
			}
		}

		if diff, err := DifficultyFromString(string(b[1 : len(b)-1])); err != nil {
			return err
		} else {
			*d = diff

			return nil
		}
	} else {
		// Difficulty as uint64
		var err error
		if d.Lo, err = utils.ParseUint64(b); err != nil {
			return err
		} else {
			d.Hi = 0
			return nil
		}
	}
}

func (d Difficulty) Bytes() []byte {
	var buf [DifficultySize]byte
	d.PutBytesBE(buf[:])
	return buf[:]
}

func (d Difficulty) String() string {
	return hex.EncodeToString(d.Bytes())
}

func (d Difficulty) StringNumeric() string {
	return uint128.Uint128(d).String()
}

func (d *Difficulty) Scan(src any) error {
	if src == nil {
		return nil
	} else if buf, ok := src.([]byte); ok {
		if len(buf) == 0 {
			return nil
		}

		if len(buf) != DifficultySize {
			return errors.New("invalid difficulty size")
		}

		*d = DifficultyFromBytes(buf)

		return nil
	}
	return errors.New("invalid type")
}

func (d *Difficulty) Value() (driver.Value, error) {
	return d.Bytes(), nil
}

// TODO: remove uint256 dependency as it's unique to this section
var powBase = uint256.NewInt(0).SetBytes32(bytes.Repeat([]byte{0xff}, 32))

func DifficultyFromPoW(powHash Hash) Difficulty {
	if powHash == ZeroHash {
		return ZeroDifficulty
	}

	pow := uint256.NewInt(0).SetBytes32(powHash[:])
	pow = &uint256.Int{bits.ReverseBytes64(pow[3]), bits.ReverseBytes64(pow[2]), bits.ReverseBytes64(pow[1]), bits.ReverseBytes64(pow[0])}

	powResult := uint256.NewInt(0).Div(powBase, pow).Bytes32()
	return DifficultyFromBytes(powResult[16:])
}

func (d Difficulty) CheckPoW(pow Hash) bool {
	return DifficultyFromPoW(pow).Cmp(d) >= 0
}

// Target
// Finds a 64-bit target for mining (target = 2^64 / difficulty) and rounds up the result of division
// Because of that, there's a very small chance that miners will find a hash that meets the target but is still wrong (hash * difficulty >= 2^256)
// A proper difficulty check is in check_pow()
func (d Difficulty) Target() uint64 {
	if d.Hi > 0 {
		return 1
	}

	// Safeguard against division by zero (CPU will trigger it even if lo = 1 because result doesn't fit in 64 bits)
	if d.Lo <= 1 {
		return math.MaxUint64
	}

	q, rem := Difficulty{Hi: 1, Lo: 0}.QuoRem64(d.Lo)
	if rem > 0 {
		return q.Lo + 1
	} else {
		return q.Lo
	}
}
