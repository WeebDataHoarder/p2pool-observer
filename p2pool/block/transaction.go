package block

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

const TxExtraTagPadding = 0x00
const TxExtraTagPubKey = 0x01
const TxExtraNonce = 0x02
const TxExtraMergeMiningTag = 0x03
const TxExtraTagAdditionalPubKeys = 0x04

type CoinbaseTransaction struct {
	id         types.Hash
	idLock     sync.Mutex
	Version    uint8
	UnlockTime uint64
	InputCount uint8
	InputType  uint8
	GenHeight  uint64
	Outputs    []*CoinbaseTransactionOutput

	Extra []byte

	ExtraBaseRCT uint8
}

func (c *CoinbaseTransaction) FromReader(reader *bytes.Reader) (err error) {
	var (
		txExtraSize uint64
	)

	if err = binary.Read(reader, binary.BigEndian, &c.Version); err != nil {
		return err
	}

	if c.UnlockTime, err = binary.ReadUvarint(reader); err != nil {
		return err
	}

	if err = binary.Read(reader, binary.BigEndian, &c.InputCount); err != nil {
		return err
	}

	if err = binary.Read(reader, binary.BigEndian, &c.InputType); err != nil {
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
			}
		default:
			return fmt.Errorf("unknown %d TXOUT key", o.Type)
		}

		c.Outputs = append(c.Outputs, o)
	}

	if txExtraSize, err = binary.ReadUvarint(reader); err != nil {
		return err
	}

	c.Extra = make([]byte, txExtraSize)
	if _, err = io.ReadFull(reader, c.Extra); err != nil {
		return err
	}
	if err = binary.Read(reader, binary.BigEndian, &c.ExtraBaseRCT); err != nil {
		return err
	}

	return nil
}

func (c *CoinbaseTransaction) Bytes() []byte {
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
			return nil
		}
	}
	_, _ = buf.Write(varIntBuf[:binary.PutUvarint(varIntBuf, uint64(len(c.Extra)))])
	_, _ = buf.Write(c.Extra)
	_ = binary.Write(buf, binary.BigEndian, c.ExtraBaseRCT)

	return buf.Bytes()
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

	txBytes := c.Bytes()
	// remove base RCT
	idHasher.Write(hashKeccak(txBytes[:len(txBytes)-1]))
	// Base RCT, single 0 byte in miner tx
	idHasher.Write(hashKeccak([]byte{c.ExtraBaseRCT}))
	// Prunable RCT, empty in miner tx
	idHasher.Write(make([]byte, types.HashSize))

	copy(c.id[:], idHasher.Sum([]byte{}))

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
