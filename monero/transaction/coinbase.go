package transaction

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"io"
	"sync"
)

type CoinbaseTransaction struct {
	id         types.Hash
	idLock     sync.Mutex
	Version    uint8   `json:"version"`
	UnlockTime uint64  `json:"unlock_time"`
	InputCount uint8   `json:"input_count"`
	InputType  uint8   `json:"input_type"`
	GenHeight  uint64  `json:"gen_height"`
	Outputs    Outputs `json:"outputs"`

	// OutputsBlobSize length of serialized Outputs. Used by p2pool serialized pruned blocks, filled regardless
	OutputsBlobSize uint64 `json:"-"`
	// TotalReward amount of reward existing Outputs. Used by p2pool serialized pruned blocks, filled regardless
	TotalReward uint64 `json:"total_reward"`

	Extra ExtraTags `json:"extra"`

	ExtraBaseRCT uint8 `json:"extra_base_rct"`
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

	if c.InputCount != 1 {
		return errors.New("invalid input count")
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

	if c.UnlockTime != (c.GenHeight + monero.MinerRewardUnlockTime) {
		return errors.New("invalid unlock time")
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

func (c *CoinbaseTransaction) SideChainHashingBlob(zeroTemplateId bool) ([]byte, error) {
	buf := new(bytes.Buffer)

	varIntBuf := make([]byte, binary.MaxVarintLen64)

	_ = binary.Write(buf, binary.BigEndian, c.Version)
	_, _ = buf.Write(varIntBuf[:binary.PutUvarint(varIntBuf, c.UnlockTime)])
	_ = binary.Write(buf, binary.BigEndian, c.InputCount)
	_ = binary.Write(buf, binary.BigEndian, c.InputType)
	_, _ = buf.Write(varIntBuf[:binary.PutUvarint(varIntBuf, c.GenHeight)])

	outputs, _ := c.Outputs.MarshalBinary()
	_, _ = buf.Write(outputs)

	txExtra, _ := c.Extra.SideChainHashingBlob(zeroTemplateId)
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

	c.id = c.CalculateId()

	return c.id
}

func (c *CoinbaseTransaction) CalculateId() (hash types.Hash) {
	idHasher := crypto.GetKeccak256Hasher()
	defer crypto.PutKeccak256Hasher(idHasher)

	txBytes, _ := c.MarshalBinary()
	// remove base RCT
	idHasher.Write(hashKeccak(txBytes[:len(txBytes)-1]))
	// Base RCT, single 0 byte in miner tx
	idHasher.Write(hashKeccak([]byte{c.ExtraBaseRCT}))
	// Prunable RCT, empty in miner tx
	idHasher.Write(types.ZeroHash[:])
	crypto.HashFastSum(idHasher, hash[:])
	return hash
}

func hashKeccak(data ...[]byte) []byte {
	d := crypto.PooledKeccak256(data...)
	return d[:]
}
