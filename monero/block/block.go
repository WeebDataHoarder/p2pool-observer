package block

import (
	"bytes"
	"encoding/binary"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/randomx"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/transaction"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"hash"
	"io"
)

type Block struct {
	MajorVersion uint8
	MinorVersion uint8
	Timestamp    uint64
	PreviousId   types.Hash
	Nonce        uint32

	Coinbase *transaction.CoinbaseTransaction

	Transactions []types.Hash
	// TransactionParentIndices amount of reward existing Outputs. Used by p2pool serialized compact broadcasted blocks in protocol >= 1.1, filled only in compact blocks. Will be set to nil after initial pre-processing
	TransactionParentIndices []uint64
}

type Header struct {
	MajorVersion uint8
	MinorVersion uint8
	Timestamp    uint64
	PreviousId   types.Hash
	Height       uint64
	Nonce        uint32
	Reward       uint64
	Difficulty   types.Difficulty
	Id           types.Hash
}

type readerAndByteReader interface {
	io.Reader
	io.ByteReader
}

func (b *Block) MarshalBinary() (buf []byte, err error) {
	var txBuf []byte
	if txBuf, err = b.Coinbase.MarshalBinary(); err != nil {
		return nil, err
	}
	buf = make([]byte, 0, 1+1+binary.MaxVarintLen64+types.HashSize+4+len(txBuf)+binary.MaxVarintLen64+types.HashSize*len(b.Transactions))
	buf = append(buf, b.MajorVersion)
	buf = append(buf, b.MinorVersion)
	buf = binary.AppendUvarint(buf, b.Timestamp)
	buf = append(buf, b.PreviousId[:]...)
	buf = binary.LittleEndian.AppendUint32(buf, b.Nonce)

	buf = append(buf, txBuf[:]...)

	buf = binary.AppendUvarint(buf, uint64(len(b.Transactions)))
	for _, txId := range b.Transactions {
		buf = append(buf, txId[:]...)
	}

	return buf, nil
}

func (b *Block) FromReader(reader readerAndByteReader) (err error) {
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

	if txCount, err = binary.ReadUvarint(reader); err != nil {
		return err
	}

	if txCount < 8192 {
		b.Transactions = make([]types.Hash, 0, txCount)
	}

	for i := 0; i < int(txCount); i++ {
		if _, err = io.ReadFull(reader, transactionHash[:]); err != nil {
			return err
		}
		b.Transactions = append(b.Transactions, transactionHash)
	}

	return nil
}


func (b *Block) FromCompactReader(reader readerAndByteReader) (err error) {
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

	if txCount, err = binary.ReadUvarint(reader); err != nil {
		return err
	}

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

	return nil
}

func (b *Block) UnmarshalBinary(data []byte) error {
	reader := bytes.NewReader(data)
	return b.FromReader(reader)
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

func (b *Block) HeaderBlob() []byte {
	//TODO: cache
	buf := make([]byte, 0, 1+1+binary.MaxVarintLen64+types.HashSize+4+types.HashSize+binary.MaxVarintLen64) //predict its use on HashingBlob
	buf = append(buf, b.MajorVersion)
	buf = append(buf, b.MinorVersion)
	buf = binary.AppendUvarint(buf, b.Timestamp)
	buf = append(buf, b.PreviousId[:]...)
	buf = binary.LittleEndian.AppendUint32(buf, b.Nonce)

	return buf
}

func (b *Block) SideChainHashingBlob() (buf []byte, err error) {
	var txBuf []byte
	if txBuf, err = b.Coinbase.SideChainHashingBlob(); err != nil {
		return nil, err
	}
	buf = make([]byte, 0, 1+1+binary.MaxVarintLen64+types.HashSize+4+len(txBuf)+binary.MaxVarintLen64+types.HashSize*len(b.Transactions))
	buf = append(buf, b.MajorVersion)
	buf = append(buf, b.MinorVersion)
	buf = binary.AppendUvarint(buf, b.Timestamp)
	buf = append(buf, b.PreviousId[:]...)
	buf = binary.LittleEndian.AppendUint32(buf, 0) //replaced

	buf = append(buf, txBuf[:]...)

	buf = binary.AppendUvarint(buf, uint64(len(b.Transactions)))
	for _, txId := range b.Transactions {
		buf = append(buf, txId[:]...)
	}

	return buf, nil
}

func (b *Block) HashingBlob() []byte {
	//TODO: cache
	buf := b.HeaderBlob()

	txTreeHash := b.TxTreeHash()
	buf = append(buf, txTreeHash[:]...)

	buf = binary.AppendUvarint(buf, uint64(len(b.Transactions)+1))

	return buf
}

func (b *Block) TxTreeHash() (rootHash types.Hash) {
	//TODO: cache
	//transaction hashes
	h := make([]byte, 0, types.HashSize*len(b.Transactions)+types.HashSize)
	coinbaseTxId := b.Coinbase.Id()

	h = append(h, coinbaseTxId[:]...)
	for _, txId := range b.Transactions {
		h = append(h, txId[:]...)
	}

	count := len(b.Transactions) + 1
	if count == 1 {
		rootHash = types.HashFromBytes(h)
	} else if count == 2 {
		rootHash = crypto.PooledKeccak256(h)
	} else {
		hashInstance := crypto.GetKeccak256Hasher()
		defer crypto.PutKeccak256Hasher(hashInstance)
		var cnt int

		{
			//TODO: expand this loop properly
			//find closest low power of two
			for cnt = 1; cnt <= count; cnt <<= 1 {
			}
			cnt >>= 1
		}

		ints := make([]byte, cnt*types.HashSize)
		copy(ints, h[:(cnt*2-count)*types.HashSize])

		{
			i := cnt*2 - count
			j := cnt*2 - count
			for j < cnt {
				keccakl(hashInstance, ints[j*types.HashSize:], h[i*types.HashSize:], types.HashSize*2)
				i += 2
				j++
			}
		}

		for cnt > 2 {
			cnt >>= 1
			{
				i := 0
				j := 0

				for j < cnt {
					keccakl(hashInstance, ints[j*types.HashSize:], ints[i*types.HashSize:], types.HashSize*2)

					i += 2
					j++
				}
			}
		}

		keccakl(hashInstance, rootHash[:], ints, types.HashSize*2)
	}

	return
}

func (b *Block) Difficulty() types.Difficulty {
	//cached by sidechain.Share
	d, _ := client.GetClient().GetDifficultyByHeight(b.Coinbase.GenHeight)
	return d
}

func (b *Block) PowHash() types.Hash {
	//cached by sidechain.Share
	h, _ := HashBlob(b.Coinbase.GenHeight, b.HashingBlob())
	return h
}

func (b *Block) PowHashWithError() (types.Hash, error) {
	//not cached
	return HashBlob(b.Coinbase.GenHeight, b.HashingBlob())
}

func (b *Block) Id() types.Hash {
	//cached by sidechain.Share
	buf := b.HashingBlob()
	return crypto.PooledKeccak256(binary.AppendUvarint(nil, uint64(len(buf))), buf)
}

var hasher = randomx.NewRandomX()

func keccakl(hasher hash.Hash, dst []byte, data []byte, len int) {
	hasher.Reset()
	hasher.Write(data[:len])
	crypto.HashFastSum(hasher, dst)
}
