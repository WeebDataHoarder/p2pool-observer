package transaction

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"git.gammaspectra.live/P2Pool/moneroutil"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"golang.org/x/crypto/sha3"
	"io"
	"sync"
)

const TxInGen = 0xff

const TxOutToKey = 2
const TxOutToTaggedKey = 3

type CoinbaseTransaction struct {
	id         types.Hash
	idLock     sync.Mutex
	Version    uint8
	UnlockTime uint64
	InputCount uint8
	InputType  uint8
	GenHeight  uint64
	Outputs    []*CoinbaseTransactionOutput

	// OutputsBlobSize length of serialized Outputs. Used by p2pool serialized pruned blocks, filled regardless
	OutputsBlobSize uint64
	// TotalReward amount of reward existing Outputs. Used by p2pool serialized pruned blocks, filled regardless
	TotalReward uint64

	Extra ExtraTags

	ExtraBaseRCT uint8
}

type readerAndByteReader interface {
	io.Reader
	io.ByteReader
}

func (c *CoinbaseTransaction) UnmarshalBinary(data []byte) error {
	reader := bytes.NewReader(data)
	return c.FromReader(reader)
}

func (c *CoinbaseTransaction) FromReader(reader readerAndByteReader) (err error) {
	var (
		txExtraSize uint64
	)

	c.TotalReward = 0
	c.OutputsBlobSize = 0

	if c.Version, err = reader.ReadByte(); err != nil {
		return err
	}

	if c.Version != 2 {
		return errors.New("version not supported")
	}

	if c.UnlockTime, err = binary.ReadUvarint(reader); err != nil {
		return err
	}

	if c.InputCount, err = reader.ReadByte(); err != nil {
		return err
	}

	if c.InputType, err = reader.ReadByte(); err != nil {
		return err
	}

	if c.InputType != TxInGen {
		return errors.New("invalid coinbase input type")
	}

	if c.GenHeight, err = binary.ReadUvarint(reader); err != nil {
		return err
	}

	var outputCount uint64

	if outputCount, err = binary.ReadUvarint(reader); err != nil {
		return err
	}

	if outputCount > 0 {
		if outputCount < 8192 {
			c.Outputs = make([]*CoinbaseTransactionOutput, 0, outputCount)
		}

		for index := 0; index < int(outputCount); index++ {
			o := &CoinbaseTransactionOutput{
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
					if err = binary.Read(reader, binary.BigEndian, &o.ViewTag); err != nil {
						return err
					}
					c.OutputsBlobSize += 1 + types.HashSize + 1
				} else {
					c.OutputsBlobSize += 1 + types.HashSize
				}
			default:
				return fmt.Errorf("unknown %d TXOUT key", o.Type)
			}

			c.TotalReward += o.Reward
			c.Outputs = append(c.Outputs, o)
		}
	} else {
		// Outputs are not in the buffer and must be calculated from sidechain data
		// We only have total reward and outputs blob size here
		//special case, pruned block. outputs have to be generated from chain

		if c.TotalReward, err = binary.ReadUvarint(reader); err != nil {
			return err
		}

		if c.OutputsBlobSize, err = binary.ReadUvarint(reader); err != nil {
			return err
		}
	}

	if txExtraSize, err = binary.ReadUvarint(reader); err != nil {
		return err
	}
	if txExtraSize > 65536 {
		return errors.New("tx extra too large")
	}

	txExtra := make([]byte, txExtraSize)
	if _, err = io.ReadFull(reader, txExtra); err != nil {
		return err
	}
	if err = c.Extra.UnmarshalBinary(txExtra); err != nil {
		return err
	}
	if err = binary.Read(reader, binary.BigEndian, &c.ExtraBaseRCT); err != nil {
		return err
	}

	if c.ExtraBaseRCT != 0 {
		return errors.New("invalid extra base RCT")
	}

	return nil
}

func (c *CoinbaseTransaction) MarshalBinary() ([]byte, error) {
	buf := new(bytes.Buffer)

	varIntBuf := make([]byte, binary.MaxVarintLen64)

	_ = binary.Write(buf, binary.BigEndian, c.Version)
	_, _ = buf.Write(varIntBuf[:binary.PutUvarint(varIntBuf, c.UnlockTime)])
	_ = binary.Write(buf, binary.BigEndian, c.InputCount)
	_ = binary.Write(buf, binary.BigEndian, c.InputType)
	_, _ = buf.Write(varIntBuf[:binary.PutUvarint(varIntBuf, c.GenHeight)])

	_, _ = buf.Write(varIntBuf[:binary.PutUvarint(varIntBuf, uint64(len(c.Outputs)))])

	for _, o := range c.Outputs {
		_, _ = buf.Write(varIntBuf[:binary.PutUvarint(varIntBuf, o.Reward)])
		_ = binary.Write(buf, binary.BigEndian, o.Type)

		switch o.Type {
		case TxOutToTaggedKey, TxOutToKey:
			_, _ = buf.Write(o.EphemeralPublicKey[:])

			if o.Type == TxOutToTaggedKey {
				_ = binary.Write(buf, binary.BigEndian, o.ViewTag)
			}
		default:
			return nil, errors.New("unknown output type")
		}
	}

	txExtra, _ := c.Extra.MarshalBinary()
	_, _ = buf.Write(varIntBuf[:binary.PutUvarint(varIntBuf, uint64(len(txExtra)))])
	_, _ = buf.Write(txExtra)
	_ = binary.Write(buf, binary.BigEndian, c.ExtraBaseRCT)

	return buf.Bytes(), nil
}

func (c *CoinbaseTransaction) OutputsBlob() ([]byte, error) {
	buf := new(bytes.Buffer)

	varIntBuf := make([]byte, binary.MaxVarintLen64)

	for _, o := range c.Outputs {
		_, _ = buf.Write(varIntBuf[:binary.PutUvarint(varIntBuf, o.Reward)])
		_ = binary.Write(buf, binary.BigEndian, o.Type)

		switch o.Type {
		case TxOutToTaggedKey, TxOutToKey:
			_, _ = buf.Write(o.EphemeralPublicKey[:])

			if o.Type == TxOutToTaggedKey {
				_ = binary.Write(buf, binary.BigEndian, o.ViewTag)
			}
		default:
			return nil, errors.New("unknown output type")
		}
	}

	return buf.Bytes(), nil
}

func (c *CoinbaseTransaction) SideChainHashingBlob() ([]byte, error) {
	buf := new(bytes.Buffer)

	varIntBuf := make([]byte, binary.MaxVarintLen64)

	_ = binary.Write(buf, binary.BigEndian, c.Version)
	_, _ = buf.Write(varIntBuf[:binary.PutUvarint(varIntBuf, c.UnlockTime)])
	_ = binary.Write(buf, binary.BigEndian, c.InputCount)
	_ = binary.Write(buf, binary.BigEndian, c.InputType)
	_, _ = buf.Write(varIntBuf[:binary.PutUvarint(varIntBuf, c.GenHeight)])

	_, _ = buf.Write(varIntBuf[:binary.PutUvarint(varIntBuf, uint64(len(c.Outputs)))])

	for _, o := range c.Outputs {
		_, _ = buf.Write(varIntBuf[:binary.PutUvarint(varIntBuf, o.Reward)])
		_ = binary.Write(buf, binary.BigEndian, o.Type)

		switch o.Type {
		case TxOutToTaggedKey, TxOutToKey:
			_, _ = buf.Write(o.EphemeralPublicKey[:])

			if o.Type == TxOutToTaggedKey {
				_ = binary.Write(buf, binary.BigEndian, o.ViewTag)
			}
		default:
			return nil, errors.New("unknown output type")
		}
	}

	txExtra, _ := c.Extra.SideChainHashingBlob()
	_, _ = buf.Write(varIntBuf[:binary.PutUvarint(varIntBuf, uint64(len(txExtra)))])
	_, _ = buf.Write(txExtra)
	_ = binary.Write(buf, binary.BigEndian, c.ExtraBaseRCT)

	return buf.Bytes(), nil
}

func (c *CoinbaseTransaction) Id() types.Hash {
	if c.id != (types.Hash{}) {
		return c.id
	}

	c.idLock.Lock()
	defer c.idLock.Unlock()
	if c.id != (types.Hash{}) {
		return c.id
	}

	idHasher := sha3.NewLegacyKeccak256()

	txBytes, _ := c.MarshalBinary()
	// remove base RCT
	idHasher.Write(hashKeccak(txBytes[:len(txBytes)-1]))
	// Base RCT, single 0 byte in miner tx
	idHasher.Write(hashKeccak([]byte{c.ExtraBaseRCT}))
	// Prunable RCT, empty in miner tx
	idHasher.Write(make([]byte, types.HashSize))

	copy(c.id[:], idHasher.Sum(nil))

	return c.id
}

func hashKeccak(data ...[]byte) []byte {
	d := moneroutil.Keccak256(data...)
	return d[:]
}

type CoinbaseTransactionOutput struct {
	Index              uint64
	Reward             uint64
	Type               uint8
	EphemeralPublicKey types.Hash
	ViewTag            uint8
}
