package sidechain

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	mainblock "git.gammaspectra.live/P2Pool/p2pool-observer/monero/block"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/transaction"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"io"
	"sync"
	"sync/atomic"
	"unsafe"
)

type CoinbaseExtraTag int

const SideExtraNonceSize = 4
const SideExtraNonceMaxSize = SideExtraNonceSize + 10

const (
	SideCoinbasePublicKey = transaction.TxExtraTagPubKey
	SideExtraNonce        = transaction.TxExtraTagNonce
	SideTemplateId        = transaction.TxExtraTagMergeMining
)

type PoolBlock struct {
	Main mainblock.Block

	Side SideData

	//Temporary data structures
	cache    poolBlockCache
	Depth    atomic.Uint64
	Verified atomic.Bool
	Invalid  atomic.Bool

	WantBroadcast atomic.Bool
	Broadcasted   atomic.Bool

	LocalTimestamp uint64
}

// NewShareFromExportedBytes TODO deprecate this in favor of standard serialized shares
func NewShareFromExportedBytes(buf []byte) (*PoolBlock, error) {
	b := &PoolBlock{}

	if len(buf) < 32 {
		return nil, errors.New("invalid block data")
	}

	reader := bytes.NewReader(buf)

	var (
		err     error
		version uint64

		mainDataSize uint64
		mainData     []byte

		sideDataSize uint64
		sideData     []byte
	)

	if err = binary.Read(reader, binary.BigEndian, &version); err != nil {
		return nil, err
	}

	switch version {
	case 1:

		if _, err = io.ReadFull(reader, b.cache.mainId[:]); err != nil {
			return nil, err
		}

		if _, err = io.ReadFull(reader, b.cache.powHash[:]); err != nil {
			return nil, err
		}

		if err = binary.Read(reader, binary.BigEndian, &b.cache.mainDifficulty.Hi); err != nil {
			return nil, err
		}
		if err = binary.Read(reader, binary.BigEndian, &b.cache.mainDifficulty.Lo); err != nil {
			return nil, err
		}

		b.cache.mainDifficulty.ReverseBytes()

		if err = binary.Read(reader, binary.BigEndian, &mainDataSize); err != nil {
			return nil, err
		}
		mainData = make([]byte, mainDataSize)
		if _, err = io.ReadFull(reader, mainData); err != nil {
			return nil, err
		}

		if err = binary.Read(reader, binary.BigEndian, &sideDataSize); err != nil {
			return nil, err
		}
		sideData = make([]byte, sideDataSize)
		if _, err = io.ReadFull(reader, sideData); err != nil {
			return nil, err
		}

		/*
			//Ignore error when unable to read peer
			_ = func() error {
				var peerSize uint64

				if err = binary.Read(reader, binary.BigEndian, &peerSize); err != nil {
					return err
				}
				b.Extra.Peer = make([]byte, peerSize)
				if _, err = io.ReadFull(reader, b.Extra.Peer); err != nil {
					return err
				}

				return nil
			}()
		*/

	case 0:
		if err = binary.Read(reader, binary.BigEndian, &mainDataSize); err != nil {
			return nil, err
		}
		mainData = make([]byte, mainDataSize)
		if _, err = io.ReadFull(reader, mainData); err != nil {
			return nil, err
		}
		if sideData, err = io.ReadAll(reader); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown block version %d", version)
	}

	if err = b.Main.UnmarshalBinary(mainData); err != nil {
		return nil, err
	}

	if err = b.Side.UnmarshalBinary(sideData); err != nil {
		return nil, err
	}

	b.cache.templateId = types.HashFromBytes(b.CoinbaseExtra(SideTemplateId))

	return b, nil
}

func (b *PoolBlock) CoinbaseExtra(tag CoinbaseExtraTag) []byte {
	switch tag {
	case SideExtraNonce:
		if t := b.Main.Coinbase.Extra.GetTag(uint8(tag)); t != nil {
			if len(t.Data) < SideExtraNonceSize || len(t.Data) > SideExtraNonceMaxSize {
				return nil
			}
			return t.Data
		}
	case SideTemplateId:
		if t := b.Main.Coinbase.Extra.GetTag(uint8(tag)); t != nil {
			if len(t.Data) != types.HashSize {
				return nil
			}
			return t.Data
		}
	case SideCoinbasePublicKey:
		if t := b.Main.Coinbase.Extra.GetTag(uint8(tag)); t != nil {
			if len(t.Data) != crypto.PublicKeySize {
				return nil
			}
			return t.Data
		}
	}

	return nil
}

func (b *PoolBlock) MainId() types.Hash {
	if hash, ok := func() (types.Hash, bool) {
		b.cache.lock.RLock()
		defer b.cache.lock.RUnlock()

		if b.cache.mainId != types.ZeroHash {
			return b.cache.mainId, true
		}
		return types.ZeroHash, false
	}(); ok {
		return hash
	} else {
		b.cache.lock.Lock()
		defer b.cache.lock.Unlock()
		if b.cache.mainId == types.ZeroHash { //check again for race
			b.cache.mainId = b.Main.Id()
		}
		return b.cache.mainId
	}
}

func (b *PoolBlock) FullId(consensus *Consensus) FullId {
	if fullId, ok := func() (FullId, bool) {
		b.cache.lock.RLock()
		defer b.cache.lock.RUnlock()

		if b.cache.fullId != zeroFullId {
			return b.cache.fullId, true
		}
		return zeroFullId, false
	}(); ok {
		return fullId
	} else {
		b.cache.lock.Lock()
		defer b.cache.lock.Unlock()
		if b.cache.fullId == zeroFullId { //check again for race
			b.cache.fullId = b.CalculateFullId(consensus)
		}
		return b.cache.fullId
	}
}

const FullIdSize = int(types.HashSize + unsafe.Sizeof(uint32(0)) + SideExtraNonceSize)

var zeroFullId FullId

type FullId [FullIdSize]byte

func (b *PoolBlock) CalculateFullId(consensus *Consensus) FullId {
	var buf FullId
	sidechainId := b.SideTemplateId(consensus)
	copy(buf[:], sidechainId[:])
	binary.LittleEndian.PutUint32(buf[types.HashSize:], b.Main.Nonce)
	copy(buf[types.HashSize+unsafe.Sizeof(b.Main.Nonce):], b.CoinbaseExtra(SideExtraNonce)[:SideExtraNonceSize])
	return buf
}

func (b *PoolBlock) MainDifficulty() types.Difficulty {
	if difficulty, ok := func() (types.Difficulty, bool) {
		b.cache.lock.RLock()
		defer b.cache.lock.RUnlock()

		if b.cache.mainDifficulty != types.ZeroDifficulty {
			return b.cache.mainDifficulty, true
		}
		return types.ZeroDifficulty, false
	}(); ok {
		return difficulty
	} else {
		b.cache.lock.Lock()
		defer b.cache.lock.Unlock()
		if b.cache.mainDifficulty == types.ZeroDifficulty { //check again for race
			b.cache.mainDifficulty = b.Main.Difficulty()
		}
		return b.cache.mainDifficulty
	}
}

func (b *PoolBlock) SideTemplateId(consensus *Consensus) types.Hash {
	if hash, ok := func() (types.Hash, bool) {
		b.cache.lock.RLock()
		defer b.cache.lock.RUnlock()

		if b.cache.templateId != types.ZeroHash {
			return b.cache.templateId, true
		}
		return types.ZeroHash, false
	}(); ok {
		return hash
	} else {
		b.cache.lock.Lock()
		defer b.cache.lock.Unlock()
		if b.cache.templateId == types.ZeroHash { //check again for race
			b.cache.templateId = consensus.CalculateSideTemplateId(&b.Main, &b.Side)
		}
		return b.cache.templateId
	}
}

func (b *PoolBlock) PowHash() types.Hash {
	h, _ := b.PowHashWithError()
	return h
}

func (b *PoolBlock) PowHashWithError() (powHash types.Hash, err error) {
	if hash, ok := func() (types.Hash, bool) {
		b.cache.lock.RLock()
		defer b.cache.lock.RUnlock()

		if b.cache.powHash != types.ZeroHash {
			return b.cache.powHash, true
		}
		return types.ZeroHash, false
	}(); ok {
		return hash, nil
	} else {
		b.cache.lock.Lock()
		defer b.cache.lock.Unlock()
		if b.cache.powHash == types.ZeroHash { //check again for race
			b.cache.powHash, err = b.Main.PowHashWithError()
		}
		return b.cache.powHash, err
	}
}

func (b *PoolBlock) UnmarshalBinary(data []byte) error {
	reader := bytes.NewReader(data)
	return b.FromReader(reader)
}

func (b *PoolBlock) MarshalBinary() ([]byte, error) {
	if mainData, err := b.Main.MarshalBinary(); err != nil {
		return nil, err
	} else if sideData, err := b.Side.MarshalBinary(); err != nil {
		return nil, err
	} else {
		data := make([]byte, 0, len(mainData)+len(sideData))
		data = append(data, mainData...)
		data = append(data, sideData...)
		return data, nil
	}
}

func (b *PoolBlock) MarshalBinaryFlags(pruned, compact bool) ([]byte, error) {
	if mainData, err := b.Main.MarshalBinaryFlags(pruned, compact); err != nil {
		return nil, err
	} else if sideData, err := b.Side.MarshalBinary(); err != nil {
		return nil, err
	} else {
		data := make([]byte, 0, len(mainData)+len(sideData))
		data = append(data, mainData...)
		data = append(data, sideData...)
		return data, nil
	}
}

func (b *PoolBlock) FromReader(reader readerAndByteReader) (err error) {
	if err = b.Main.FromReader(reader); err != nil {
		return err
	}

	if err = b.Side.FromReader(reader); err != nil {
		return err
	}

	return nil
}

// FromCompactReader used in Protocol 1.1 and above
func (b *PoolBlock) FromCompactReader(reader readerAndByteReader) (err error) {
	if err = b.Main.FromCompactReader(reader); err != nil {
		return err
	}

	if err = b.Side.FromReader(reader); err != nil {
		return err
	}

	return nil
}

func (b *PoolBlock) IsProofHigherThanMainDifficulty() bool {
	r, _ := b.IsProofHigherThanMainDifficultyWithError()
	return r
}

func (b *PoolBlock) IsProofHigherThanMainDifficultyWithError() (bool, error) {
	if mainDifficulty := b.MainDifficulty(); mainDifficulty == types.ZeroDifficulty {
		return false, errors.New("could not get main difficulty")
	} else if powHash, err := b.PowHashWithError(); err != nil {
		return false, err
	} else {
		return mainDifficulty.CheckPoW(powHash), nil
	}
}

func (b *PoolBlock) IsProofHigherThanDifficulty() bool {
	r, _ := b.IsProofHigherThanDifficultyWithError()
	return r
}

func (b *PoolBlock) IsProofHigherThanDifficultyWithError() (bool, error) {
	if powHash, err := b.PowHashWithError(); err != nil {
		return false, err
	} else {
		return b.Side.Difficulty.CheckPoW(powHash), nil
	}
}

func (b *PoolBlock) GetAddress() *address.PackedAddress {
	a := address.NewPackedAddressFromBytes(b.Side.PublicSpendKey, b.Side.PublicViewKey)
	return &a
}

type poolBlockCache struct {
	lock           sync.RWMutex
	mainId         types.Hash
	mainDifficulty types.Difficulty
	templateId     types.Hash
	powHash        types.Hash
	fullId         FullId
}

func (c *poolBlockCache) FromReader(reader readerAndByteReader) (err error) {
	buf := make([]byte, types.HashSize*3+types.DifficultySize+FullIdSize)
	if _, err = reader.Read(buf); err != nil {
		return err
	}
	return c.UnmarshalBinary(buf)
}

func (c *poolBlockCache) UnmarshalBinary(buf []byte) error {
	if len(buf) < types.HashSize*3+types.DifficultySize+FullIdSize {
		return io.ErrUnexpectedEOF
	}
	copy(c.mainId[:], buf)
	c.mainDifficulty = types.DifficultyFromBytes(buf[types.HashSize:])
	copy(c.templateId[:], buf[types.HashSize+types.DifficultySize:])
	copy(c.powHash[:], buf[types.HashSize+types.DifficultySize+types.HashSize:])
	copy(c.fullId[:], buf[types.HashSize+types.DifficultySize+types.HashSize+types.HashSize:])
	return nil
}

func (c *poolBlockCache) MarshalBinary() ([]byte, error) {
	buf := make([]byte, 0, types.HashSize*3+types.DifficultySize+FullIdSize)
	buf = append(buf, c.mainId[:]...)
	buf = append(buf, c.mainDifficulty.Bytes()...)
	buf = append(buf, c.templateId[:]...)
	buf = append(buf, c.powHash[:]...)
	buf = append(buf, c.fullId[:]...)

	return buf, nil
}
