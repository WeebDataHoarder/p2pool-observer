package types

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
)

const HashSize = 32

type Hash [HashSize]byte

var ZeroHash Hash

func (h Hash) MarshalJSON() ([]byte, error) {
	return json.Marshal(h.String())
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

func (h Hash) Equals(o Hash) bool {
	return bytes.Compare(h[:], o[:]) == 0
}

func (h Hash) String() string {
	return hex.EncodeToString(h[:])
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
