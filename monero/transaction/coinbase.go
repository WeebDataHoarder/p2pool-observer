package transaction

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"golang.org/x/crypto/sha3"
	"io"
	"sync"
)

type CoinbaseTransaction struct {
	id         types.Hash
	idLock     sync.Mutex
	Version    uint8
	UnlockTime uint64
	InputCount uint8
	InputType  uint8
	GenHeight  uint64
	Outputs    Outputs

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

	if err = c.Outputs.FromReader(reader); err != nil {
		return err
	} else if len(c.Outputs) != 0 {
		for _, o := range c.Outputs {
			switch o.Type {
			case TxOutToTaggedKey:
				c.OutputsBlobSize += 1 + types.HashSize + 1
			case TxOutToKey:
				c.OutputsBlobSize += 1 + types.HashSize
			default:
				return fmt.Errorf("unknown %d TXOUT key", o.Type)
			}
			c.TotalReward += o.Reward
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
	return c.MarshalBinaryFlags(false)
}

func (c *CoinbaseTransaction) MarshalBinaryFlags(pruned bool) ([]byte, error) {
	buf := new(bytes.Buffer)

	varIntBuf := make([]byte, binary.MaxVarintLen64)

	_ = binary.Write(buf, binary.BigEndian, c.Version)
	_, _ = buf.Write(varIntBuf[:binary.PutUvarint(varIntBuf, c.UnlockTime)])
	_ = binary.Write(buf, binary.BigEndian, c.InputCount)
	_ = binary.Write(buf, binary.BigEndian, c.InputType)
	_, _ = buf.Write(varIntBuf[:binary.PutUvarint(varIntBuf, c.GenHeight)])

	outputs, _ := c.OutputsBlob()
	if pruned {
		//pruned output
		_, _ = buf.Write(varIntBuf[:binary.PutUvarint(varIntBuf, 0)])
		_, _ = buf.Write(varIntBuf[:binary.PutUvarint(varIntBuf, c.TotalReward)])
		_, _ = buf.Write(varIntBuf[:binary.PutUvarint(varIntBuf, uint64(len(outputs)))])
	} else {
		_, _ = buf.Write(outputs)
	}

	txExtra, _ := c.Extra.MarshalBinary()
	_, _ = buf.Write(varIntBuf[:binary.PutUvarint(varIntBuf, uint64(len(txExtra)))])
	_, _ = buf.Write(txExtra)
	_ = binary.Write(buf, binary.BigEndian, c.ExtraBaseRCT)

	return buf.Bytes(), nil
}

func (c *CoinbaseTransaction) OutputsBlob() ([]byte, error) {
	return c.Outputs.MarshalBinary()
}

func (c *CoinbaseTransaction) SideChainHashingBlob() ([]byte, error) {
	buf := new(bytes.Buffer)

	varIntBuf := make([]byte, binary.MaxVarintLen64)

	_ = binary.Write(buf, binary.BigEndian, c.Version)
	_, _ = buf.Write(varIntBuf[:binary.PutUvarint(varIntBuf, c.UnlockTime)])
	_ = binary.Write(buf, binary.BigEndian, c.InputCount)
	_ = binary.Write(buf, binary.BigEndian, c.InputType)
	_, _ = buf.Write(varIntBuf[:binary.PutUvarint(varIntBuf, c.GenHeight)])

	outputs, _ := c.Outputs.MarshalBinary()
	_, _ = buf.Write(outputs)

	txExtra, _ := c.Extra.SideChainHashingBlob()
	_, _ = buf.Write(varIntBuf[:binary.PutUvarint(varIntBuf, uint64(len(txExtra)))])
	_, _ = buf.Write(txExtra)
	_ = binary.Write(buf, binary.BigEndian, c.ExtraBaseRCT)

	return buf.Bytes(), nil
}

func (c *CoinbaseTransaction) Id() types.Hash {
	if c.id != types.ZeroHash {
		return c.id
	}

	c.idLock.Lock()
	defer c.idLock.Unlock()
	if c.id != types.ZeroHash {
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
	d := crypto.PooledKeccak256(data...)
	return d[:]
}
