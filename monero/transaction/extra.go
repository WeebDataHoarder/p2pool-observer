package transaction

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"io"
)

const TxExtraTagPadding = 0x00
const TxExtraTagPubKey = 0x01
const TxExtraTagNonce = 0x02
const TxExtraTagMergeMining = 0x03
const TxExtraTagAdditionalPubKeys = 0x04
const TxExtraTagMysteriousMinergate = 0xde

const TxExtraPaddingMaxCount = 255
const TxExtraNonceMaxCount = 255
const TxExtraAdditionalPubKeysMaxCount = 4096

const TxExtraTemplateNonceSize = 4

type ExtraTags []ExtraTag

type ExtraTag struct {
	Tag uint8 `json:"tag"`
	// VarInt has different meanings. In TxExtraTagMergeMining it is depth, while in others it is length
	VarInt    uint64      `json:"var_int"`
	HasVarInt bool        `json:"has_var_int"`
	Data      types.Bytes `json:"data"`
}

func (t *ExtraTags) UnmarshalBinary(data []byte) (err error) {
	reader := bytes.NewReader(data)
	return t.FromReader(reader)
}

func (t *ExtraTags) BufferLength() (length int) {
	for _, tag := range *t {
		length += tag.BufferLength()
	}
	return
}

func (t *ExtraTags) MarshalBinary() ([]byte, error) {
	if t == nil {
		return nil, nil
	}
	buf := make([]byte, 0, t.BufferLength())
	for _, tag := range *t {
		if b, err := tag.MarshalBinary(); err != nil {
			return nil, err
		} else {
			buf = append(buf, b...)
		}
	}

	return buf, nil
}

func (t *ExtraTags) SideChainHashingBlob(zeroTemplateId bool) ([]byte, error) {
	if t == nil {
		return nil, nil
	}
	buf := make([]byte, 0, t.BufferLength())
	for _, tag := range *t {
		if b, err := tag.SideChainHashingBlob(zeroTemplateId); err != nil {
			return nil, err
		} else {
			buf = append(buf, b...)
		}
	}

	return buf, nil
}

func (t *ExtraTags) FromReader(reader readerAndByteReader) (err error) {
	var tag ExtraTag
	for {
		if err = tag.FromReader(reader); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if t.GetTag(tag.Tag) != nil {
			return errors.New("tag already exists")
		}
		*t = append(*t, tag)
	}
}

func (t *ExtraTags) GetTag(tag uint8) *ExtraTag {
	for i := range *t {
		if (*t)[i].Tag == tag {
			return &(*t)[i]
		}
	}

	return nil
}

func (t *ExtraTag) UnmarshalBinary(data []byte) error {
	reader := bytes.NewReader(data)
	return t.FromReader(reader)
}

func (t *ExtraTag) BufferLength() int {
	return len(t.Data) + 1 + binary.MaxVarintLen64
}

func (t *ExtraTag) MarshalBinary() ([]byte, error) {
	buf := make([]byte, 0, t.BufferLength())
	buf = append(buf, t.Tag)
	if t.HasVarInt {
		buf = binary.AppendUvarint(buf, t.VarInt)
	}
	buf = append(buf, t.Data...)
	return buf, nil
}

func (t *ExtraTag) SideChainHashingBlob(zeroTemplateId bool) ([]byte, error) {
	buf := make([]byte, 0, t.BufferLength())
	buf = append(buf, t.Tag)
	if t.HasVarInt {
		buf = binary.AppendUvarint(buf, t.VarInt)
	}
	if zeroTemplateId && t.Tag == TxExtraTagMergeMining {
		buf = append(buf, make([]byte, len(t.Data))...)
	} else if t.Tag == TxExtraTagNonce {
		b := make([]byte, len(t.Data))
		//Replace only the first four bytes
		if len(t.Data) > TxExtraTemplateNonceSize {
			copy(b[TxExtraTemplateNonceSize:], t.Data[TxExtraTemplateNonceSize:])
		}
		buf = append(buf, b...)
	} else {
		buf = append(buf, t.Data...)
	}
	return buf, nil
}

func (t *ExtraTag) FromReader(reader readerAndByteReader) (err error) {

	if err = binary.Read(reader, binary.LittleEndian, &t.Tag); err != nil {
		return err
	}

	switch t.Tag {
	default:
		return fmt.Errorf("unknown extra tag %d", t.Tag)
	case TxExtraTagPadding:
		var size uint64
		var zero byte
		for size = 1; size <= TxExtraPaddingMaxCount; size++ {
			if zero, err = reader.ReadByte(); err != nil {
				if err == io.EOF {
					break
				} else {
					return err
				}
			}

			if zero != 0 {
				return errors.New("padding is not zero")
			}
		}

		if size > TxExtraPaddingMaxCount {
			return errors.New("padding is too big")
		}

		t.Data = make([]byte, size-2)
	case TxExtraTagPubKey:
		t.Data = make([]byte, crypto.PublicKeySize)
		if _, err = io.ReadFull(reader, t.Data); err != nil {
			return err
		}
	case TxExtraTagNonce:
		t.HasVarInt = true
		if t.VarInt, err = binary.ReadUvarint(reader); err != nil {
			return err
		} else {
			if t.VarInt > TxExtraNonceMaxCount {
				return errors.New("nonce is too big")
			}

			t.Data = make([]byte, t.VarInt)
			if _, err = io.ReadFull(reader, t.Data); err != nil {
				return err
			}
		}
	case TxExtraTagMergeMining:
		t.HasVarInt = true
		if t.VarInt, err = binary.ReadUvarint(reader); err != nil {
			return err
		} else {
			t.Data = make([]byte, types.HashSize)
			if _, err = io.ReadFull(reader, t.Data); err != nil {
				return err
			}
		}
	case TxExtraTagAdditionalPubKeys:
		t.HasVarInt = true
		if t.VarInt, err = binary.ReadUvarint(reader); err != nil {
			return err
		} else {
			if t.VarInt > TxExtraAdditionalPubKeysMaxCount {
				return errors.New("too many public keys")
			}

			t.Data = make([]byte, types.HashSize*t.VarInt)
			if _, err = io.ReadFull(reader, t.Data); err != nil {
				return err
			}
		}
	case TxExtraTagMysteriousMinergate:
		t.HasVarInt = true
		if t.VarInt, err = binary.ReadUvarint(reader); err != nil {
			return err
		} else {
			if t.VarInt > TxExtraNonceMaxCount {
				return errors.New("nonce is too big")
			}

			t.Data = make([]byte, t.VarInt)
			if _, err = io.ReadFull(reader, t.Data); err != nil {
				return err
			}
		}
	}

	return nil
}
