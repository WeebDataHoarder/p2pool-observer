package block

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/randomx"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/transaction"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"io"
)

type Block struct {
	MajorVersion uint8      `json:"major_version"`
	MinorVersion uint8      `json:"minor_version"`
	Timestamp    uint64     `json:"timestamp"`
	PreviousId   types.Hash `json:"previous_id"`
	Nonce        uint32     `json:"nonce"`

	Coinbase *transaction.CoinbaseTransaction `json:"coinbase"`

	Transactions []types.Hash `json:"transactions"`
	// TransactionParentIndices amount of reward existing Outputs. Used by p2pool serialized compact broadcasted blocks in protocol >= 1.1, filled only in compact blocks or by pre-processing.
	TransactionParentIndices []uint64 `json:"-"`
}

type Header struct {
	MajorVersion uint8            `json:"major_version"`
	MinorVersion uint8            `json:"minor_version"`
	Timestamp    uint64           `json:"timestamp"`
	PreviousId   types.Hash       `json:"previous_id"`
	Height       uint64           `json:"height"`
	Nonce        uint32           `json:"nonce"`
	Reward       uint64           `json:"reward"`
	Difficulty   types.Difficulty `json:"difficulty"`
	Id           types.Hash       `json:"id"`
}

func (b *Block) MarshalBinary() (buf []byte, err error) {
	return b.MarshalBinaryFlags(false, false)
}

func (b *Block) BufferLength() int {
	return 1 + 1 + binary.MaxVarintLen64 + types.HashSize + 4 + b.Coinbase.BufferLength() + binary.MaxVarintLen64 + types.HashSize*len(b.Transactions)
}

func (b *Block) MarshalBinaryFlags(pruned, compact bool) (buf []byte, err error) {
	return b.AppendBinaryFlags(make([]byte, 0, b.BufferLength()), pruned, compact)
}

func (b *Block) AppendBinaryFlags(preAllocatedBuf []byte, pruned, compact bool) (buf []byte, err error) {
	buf = preAllocatedBuf
	buf = append(buf, b.MajorVersion)
	if b.MajorVersion > monero.HardForkSupportedVersion {
		return nil, fmt.Errorf("unsupported version %d", b.MajorVersion)
	}
	buf = append(buf, b.MinorVersion)
	if b.MinorVersion < b.MajorVersion {
		return nil, fmt.Errorf("minor version %d smaller than major %d", b.MinorVersion, b.MajorVersion)
	}
	buf = binary.AppendUvarint(buf, b.Timestamp)
	buf = append(buf, b.PreviousId[:]...)
	buf = binary.LittleEndian.AppendUint32(buf, b.Nonce)

	if buf, err = b.Coinbase.AppendBinaryFlags(buf, pruned); err != nil {
		return nil, err
	}

	buf = binary.AppendUvarint(buf, uint64(len(b.Transactions)))
	if compact {
		for i, txId := range b.Transactions {
			if i < len(b.TransactionParentIndices) && b.TransactionParentIndices[i] != 0 {
				buf = binary.AppendUvarint(buf, b.TransactionParentIndices[i])
			} else {
				buf = binary.AppendUvarint(buf, 0)
				buf = append(buf, txId[:]...)
			}
		}
	} else {
		for _, txId := range b.Transactions {
			buf = append(buf, txId[:]...)
		}
	}

	return buf, nil
}

func (b *Block) FromReader(reader utils.ReaderAndByteReader) (err error) {
	return b.FromReaderFlags(reader, false)
}

func (b *Block) FromCompactReader(reader utils.ReaderAndByteReader) (err error) {
	return b.FromReaderFlags(reader, true)
}

func (b *Block) UnmarshalBinary(data []byte) error {
	reader := bytes.NewReader(data)
	return b.FromReader(reader)
}

func (b *Block) FromReaderFlags(reader utils.ReaderAndByteReader, compact bool) (err error) {
	var (
		txCount         uint64
		transactionHash types.Hash
	)

	if b.MajorVersion, err = reader.ReadByte(); err != nil {
		return err
	}
	if b.MinorVersion, err = reader.ReadByte(); err != nil {
		return err
	}

	if b.Timestamp, err = binary.ReadUvarint(reader); err != nil {
		return err
	}

	if _, err = io.ReadFull(reader, b.PreviousId[:]); err != nil {
		return err
	}

	if err = binary.Read(reader, binary.LittleEndian, &b.Nonce); err != nil {
		return err
	}

	// Coinbase Tx Decoding
	{
		b.Coinbase = &transaction.CoinbaseTransaction{}
		if err = b.Coinbase.FromReader(reader); err != nil {
			return err
		}
	}

	//TODO: verify hardfork major versions

	if txCount, err = binary.ReadUvarint(reader); err != nil {
		return err
	}

	if compact {
		if txCount < 8192 {
			b.Transactions = make([]types.Hash, 0, txCount)
			b.TransactionParentIndices = make([]uint64, 0, txCount)
		}

		var parentIndex uint64
		for i := 0; i < int(txCount); i++ {
			if parentIndex, err = binary.ReadUvarint(reader); err != nil {
				return err
			}

			if parentIndex == 0 {
				//not in lookup
				if _, err = io.ReadFull(reader, transactionHash[:]); err != nil {
					return err
				}

				b.Transactions = append(b.Transactions, transactionHash)
			} else {
				b.Transactions = append(b.Transactions, types.ZeroHash)
			}

			b.TransactionParentIndices = append(b.TransactionParentIndices, parentIndex)
		}
	} else {
		if txCount < 8192 {
			b.Transactions = make([]types.Hash, 0, txCount)
		}

		for i := 0; i < int(txCount); i++ {
			if _, err = io.ReadFull(reader, transactionHash[:]); err != nil {
				return err
			}
			b.Transactions = append(b.Transactions, transactionHash)
		}
	}

	return nil
}

func (b *Block) Header() *Header {
	return &Header{
		MajorVersion: b.MajorVersion,
		MinorVersion: b.MinorVersion,
		Timestamp:    b.Timestamp,
		PreviousId:   b.PreviousId,
		Height:       b.Coinbase.GenHeight,
		Nonce:        b.Nonce,
		Reward:       b.Coinbase.TotalReward,
		Id:           b.Id(),
		Difficulty:   types.ZeroDifficulty,
	}
}

func (b *Block) HeaderBlobBufferLength() int {
	return 1 + 1 + binary.MaxVarintLen64 + types.HashSize + 4
}

func (b *Block) HeaderBlob(preAllocatedBuf []byte) []byte {
	buf := preAllocatedBuf
	buf = append(buf, b.MajorVersion)
	buf = append(buf, b.MinorVersion)
	buf = binary.AppendUvarint(buf, b.Timestamp)
	buf = append(buf, b.PreviousId[:]...)
	buf = binary.LittleEndian.AppendUint32(buf, b.Nonce)

	return buf
}

// SideChainHashingBlob Same as MarshalBinary but with nonce or template id set to 0
func (b *Block) SideChainHashingBlob(preAllocatedBuf []byte, zeroTemplateId bool) (buf []byte, err error) {
	buf = preAllocatedBuf
	buf = append(buf, b.MajorVersion)
	buf = append(buf, b.MinorVersion)
	buf = binary.AppendUvarint(buf, b.Timestamp)
	buf = append(buf, b.PreviousId[:]...)
	buf = binary.LittleEndian.AppendUint32(buf, 0) //replaced

	if buf, err = b.Coinbase.SideChainHashingBlob(buf, zeroTemplateId); err != nil {
		return nil, err
	}

	buf = binary.AppendUvarint(buf, uint64(len(b.Transactions)))
	for _, txId := range b.Transactions {
		buf = append(buf, txId[:]...)
	}

	return buf, nil
}

func (b *Block) HashingBlobBufferLength() int {
	return b.HeaderBlobBufferLength() + types.HashSize + binary.MaxVarintLen64
}

func (b *Block) HashingBlob(preAllocatedBuf []byte) []byte {
	buf := b.HeaderBlob(preAllocatedBuf)

	merkleTree := make(crypto.BinaryTreeHash, len(b.Transactions)+1)
	merkleTree[0] = b.Coinbase.Id()
	copy(merkleTree[1:], b.Transactions)
	txTreeHash := merkleTree.RootHash()
	buf = append(buf, txTreeHash[:]...)

	buf = binary.AppendUvarint(buf, uint64(len(b.Transactions)+1))

	return buf
}

func (b *Block) Difficulty(f GetDifficultyByHeightFunc) types.Difficulty {
	//cached by sidechain.Share
	return f(b.Coinbase.GenHeight)
}

func (b *Block) PowHashWithError(hasher randomx.Hasher, f GetSeedByHeightFunc) (types.Hash, error) {
	//not cached
	if seed := f(b.Coinbase.GenHeight); seed == types.ZeroHash {
		return types.ZeroHash, errors.New("could not get seed")
	} else {
		return hasher.Hash(seed[:], b.HashingBlob(make([]byte, 0, b.HashingBlobBufferLength())))
	}
}

func (b *Block) Id() types.Hash {
	//cached by sidechain.Share
	var varIntBuf [binary.MaxVarintLen64]byte
	buf := b.HashingBlob(make([]byte, 0, b.HashingBlobBufferLength()))
	return crypto.PooledKeccak256(varIntBuf[:binary.PutUvarint(varIntBuf[:], uint64(len(buf)))], buf)
}
