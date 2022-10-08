package types

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"lukechampine.com/uint128"
)

const HashSize = 32
const DifficultySize = 16
const NonceSize = 4

type Hash [HashSize]byte

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

type Difficulty struct {
	uint128.Uint128
}

func (d Difficulty) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

func DifficultyFromString(s string) (Difficulty, error) {
	if buf, err := hex.DecodeString(s); err != nil {
		return Difficulty{}, err
	} else {
		if len(buf) != DifficultySize {
			return Difficulty{}, errors.New("wrong hash size")
		}

		return Difficulty{Uint128: uint128.FromBytes(buf).ReverseBytes()}, nil
	}
}

func (d *Difficulty) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}

	if diff, err := DifficultyFromString(s); err != nil {
		return err
	} else {
		d.Uint128 = diff.Uint128

		return nil
	}
}

func (d Difficulty) String() string {
	var buf [DifficultySize]byte
	d.ReverseBytes().PutBytes(buf[:])
	return hex.EncodeToString(buf[:])
}

type Nonce [NonceSize]byte
