package transaction

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"io"
)

type Outputs []*Output


func (s *Outputs) FromReader(reader readerAndByteReader) (err error) {
	var outputCount uint64

	if outputCount, err = binary.ReadUvarint(reader); err != nil {
		return err
	}

	if outputCount > 0 {
		if outputCount < 8192 {
			*s = make(Outputs, 0, outputCount)
		}

		for index := 0; index < int(outputCount); index++ {
			o := &Output{
				Index: uint64(index),
			}

			if o.Reward, err = binary.ReadUvarint(reader); err != nil {
				return err
			}

			if err = binary.Read(reader, binary.BigEndian, &o.Type); err != nil {
				return err
			}

			switch o.Type {
			case TxOutToTaggedKey, TxOutToKey:
				if _, err = io.ReadFull(reader, o.EphemeralPublicKey[:]); err != nil {
					return err
				}

				if o.Type == TxOutToTaggedKey {
				}
			default:
				return fmt.Errorf("unknown %d TXOUT key", o.Type)
			}

			*s = append(*s, o)
		}
	}
	return nil
}

func (s *Outputs) MarshalBinary() (data []byte, err error) {
	buf := new(bytes.Buffer)

	varIntBuf := make([]byte, binary.MaxVarintLen64)

	_, _ = buf.Write(varIntBuf[:binary.PutUvarint(varIntBuf, uint64(len(*s)))])

	for _, o := range *s {
		_, _ = buf.Write(varIntBuf[:binary.PutUvarint(varIntBuf, o.Reward)])
		_ = binary.Write(buf, binary.BigEndian, o.Type)

		switch o.Type {
		case TxOutToTaggedKey, TxOutToKey:
			_, _ = buf.Write(o.EphemeralPublicKey.AsSlice())

			if o.Type == TxOutToTaggedKey {
				_ = binary.Write(buf, binary.BigEndian, o.ViewTag)
			}
		default:
			return nil, errors.New("unknown output type")
		}
	}
	return buf.Bytes(), nil
}

type Output struct {
	Index              uint64
	Reward             uint64
	Type               uint8
	EphemeralPublicKey crypto.PublicKeyBytes
	ViewTag            uint8
}