package sidechain

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"git.gammaspectra.live/P2Pool/moneroutil"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	mainblock "git.gammaspectra.live/P2Pool/p2pool-observer/monero/block"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/transaction"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"github.com/holiman/uint256"
	"io"
	"math/bits"
	"sync"
)

type CoinbaseExtraTag int

const SideExtraNonceSize = 4
const SideExtraNonceMaxSize = SideExtraNonceSize + 10

const (
	SideCoinbasePublicKey = transaction.TxExtraTagPubKey
	SideExtraNonce        = transaction.TxExtraTagNonce
	SideTemplateId        = transaction.TxExtraTagMergeMining
)

type Share struct {
	Main mainblock.Block

	Side SideData

	c struct {
		lock           sync.RWMutex
		mainId         types.Hash
		mainDifficulty types.Difficulty
		templateId     types.Hash
		powHash        types.Hash
	}
}

func NewShareFromExportedBytes(buf []byte) (*Share, error) {
	b := &Share{}

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

		if _, err = io.ReadFull(reader, b.c.mainId[:]); err != nil {
			return nil, err
		}

		if _, err = io.ReadFull(reader, b.c.powHash[:]); err != nil {
			return nil, err
		}

		if err = binary.Read(reader, binary.BigEndian, &b.c.mainDifficulty.Hi); err != nil {
			return nil, err
		}
		if err = binary.Read(reader, binary.BigEndian, &b.c.mainDifficulty.Lo); err != nil {
			return nil, err
		}

		b.c.mainDifficulty.ReverseBytes()

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

	b.c.templateId = types.HashFromBytes(b.CoinbaseExtra(SideTemplateId))

	return b, nil
}

func (b *Share) CoinbaseExtra(tag CoinbaseExtraTag) []byte {
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
			if len(t.Data) != types.HashSize {
				return nil
			}
			return t.Data
		}
	}

	return nil
}

func (b *Share) MainId() types.Hash {
	if hash, ok := func() (types.Hash, bool) {
		b.c.lock.RLock()
		defer b.c.lock.RUnlock()

		if b.c.mainId != types.ZeroHash {
			return b.c.mainId, true
		}
		return types.ZeroHash, false
	}(); ok {
		return hash
	} else {
		b.c.lock.Lock()
		defer b.c.lock.Unlock()
		if b.c.mainId == types.ZeroHash { //check again for race
			b.c.mainId = b.Main.Id()
		}
		return b.c.mainId
	}
}

func (b *Share) MainDifficulty() types.Difficulty {
	if difficulty, ok := func() (types.Difficulty, bool) {
		b.c.lock.RLock()
		defer b.c.lock.RUnlock()

		if b.c.mainDifficulty != types.ZeroDifficulty {
			return b.c.mainDifficulty, true
		}
		return types.ZeroDifficulty, false
	}(); ok {
		return difficulty
	} else {
		b.c.lock.Lock()
		defer b.c.lock.Unlock()
		if b.c.mainDifficulty == types.ZeroDifficulty { //check again for race
			b.c.mainDifficulty = b.Main.Difficulty()
		}
		return b.c.mainDifficulty
	}
}

func (b *Share) SideTemplateId(consensus *Consensus) types.Hash {
	if hash, ok := func() (types.Hash, bool) {
		b.c.lock.RLock()
		defer b.c.lock.RUnlock()

		if b.c.templateId != types.ZeroHash {
			return b.c.templateId, true
		}
		return types.ZeroHash, false
	}(); ok {
		return hash
	} else {
		b.c.lock.Lock()
		defer b.c.lock.Unlock()
		if b.c.templateId == types.ZeroHash { //check again for race
			b.c.templateId = consensus.CalculateSideTemplateId(&b.Main, &b.Side)
		}
		return b.c.templateId
	}
}

func (b *Share) PowHash() types.Hash {
	if hash, ok := func() (types.Hash, bool) {
		b.c.lock.RLock()
		defer b.c.lock.RUnlock()

		if b.c.powHash != types.ZeroHash {
			return b.c.powHash, true
		}
		return types.ZeroHash, false
	}(); ok {
		return hash
	} else {
		b.c.lock.Lock()
		defer b.c.lock.Unlock()
		if b.c.powHash == types.ZeroHash { //check again for race
			b.c.powHash = b.Main.PowHash()
		}
		return b.c.powHash
	}
}

func (b *Share) UnmarshalBinary(data []byte) error {
	reader := bytes.NewReader(data)
	return b.FromReader(reader)
}

func (b *Share) MarshalBinary() ([]byte, error) {
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

func (b *Share) FromReader(reader readerAndByteReader) (err error) {
	if err = b.Main.FromReader(reader); err != nil {
		return err
	}

	if err = b.Side.FromReader(reader); err != nil {
		return err
	}

	return nil
}

func (b *Share) IsProofHigherThanDifficulty() bool {
	if mainDifficulty := b.MainDifficulty(); mainDifficulty == types.ZeroDifficulty {
		//TODO: err
		return false
	} else {
		return b.GetProofDifficulty().Cmp(mainDifficulty) >= 0
	}
}

func (b *Share) GetProofDifficulty() types.Difficulty {
	base := uint256.NewInt(0).SetBytes32(bytes.Repeat([]byte{0xff}, 32))

	powHash := b.PowHash()
	pow := uint256.NewInt(0).SetBytes32(powHash[:])
	pow = &uint256.Int{bits.ReverseBytes64(pow[3]), bits.ReverseBytes64(pow[2]), bits.ReverseBytes64(pow[1]), bits.ReverseBytes64(pow[0])}

	if pow.Eq(uint256.NewInt(0)) {
		return types.Difficulty{}
	}

	powResult := uint256.NewInt(0).Div(base, pow).Bytes32()
	return types.DifficultyFromBytes(powResult[16:])
}

func (b *Share) GetAddress() *address.Address {
	return address.FromRawAddress(moneroutil.MainNetwork, b.Side.PublicSpendKey, b.Side.PublicViewKey)
}
