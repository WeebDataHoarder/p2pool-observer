package types

import (
	"bytes"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"errors"
	"runtime"
	"unsafe"
)

const HashSize = 32

type Hash [HashSize]byte

var ZeroHash Hash

func (h Hash) MarshalJSON() ([]byte, error) {
	return json.Marshal(h.String())
}

func MustHashFromString(s string) Hash {
	if h, err := HashFromString(s); err != nil {
		panic(err)
		return ZeroHash
	} else {
		return h
	}
}

func HashFromString(s string) (Hash, error) {
	var h Hash
	if buf, err := hex.DecodeString(s); err != nil {
		return h, err
	} else {
		if len(buf) != HashSize {
			return h, errors.New("wrong hash size")
		}
		copy(h[:], buf)
		return h, nil
	}
}

func HashFromBytes(buf []byte) (h Hash) {
	if len(buf) != HashSize {
		return
	}
	copy(h[:], buf)
	return
}

// Compare consensus way of comparison
func (h Hash) Compare(other Hash) int {
	//golang might free other otherwise
	defer runtime.KeepAlive(other)
	defer runtime.KeepAlive(h)
	a := unsafe.Slice((*uint64)(unsafe.Pointer(&h)), len(h)/int(unsafe.Sizeof(uint64(0))))
	b := unsafe.Slice((*uint64)(unsafe.Pointer(&other)), len(other)/int(unsafe.Sizeof(uint64(0))))

	if a[3] < b[3] {
		return -1
	}
	if a[3] > b[3] {
		return 1
	}

	if a[2] < b[2] {
		return -1
	}
	if a[2] > b[2] {
		return 1
	}

	if a[1] < b[1] {
		return -1
	}
	if a[1] > b[1] {
		return 1
	}

	if a[0] < b[0] {
		return -1
	}
	if a[0] > b[0] {
		return 1
	}

	return 0
}

func (h Hash) Equals(o Hash) bool {
	return bytes.Compare(h[:], o[:]) == 0
}

func (h Hash) String() string {
	return hex.EncodeToString(h[:])
}

func (h *Hash) Scan(src any) error {
	if src == nil {
		return nil
	} else if buf, ok := src.([]byte); ok {
		if len(buf) == 0 {
			return nil
		}
		if len(buf) != HashSize {
			return errors.New("invalid hash size")
		}
		copy((*h)[:], buf)

		return nil
	}
	return errors.New("invalid type")
}

func (h *Hash) Value() (driver.Value, error) {
	if *h == ZeroHash {
		return nil, nil
	}
	return (*h)[:], nil
}

func (h *Hash) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}

	if buf, err := hex.DecodeString(s); err != nil {
		return err
	} else {
		if len(buf) != HashSize {
			return errors.New("wrong hash size")
		}

		copy(h[:], buf)
		return nil
	}
}

type Bytes []byte

func (b Bytes) MarshalJSON() ([]byte, error) {
	return json.Marshal(b.String())
}
func (b Bytes) String() string {
	return hex.EncodeToString(b)
}
func (b *Bytes) UnmarshalJSON(buf []byte) error {
	var s string
	if err := json.Unmarshal(buf, &s); err != nil {
		return err
	}

	if buf2, err := hex.DecodeString(s); err != nil {
		return err
	} else {
		*b = append(*b, buf2...)
		return nil
	}
}
