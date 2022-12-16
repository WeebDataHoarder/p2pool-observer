package block

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"github.com/floatdrop/lru"
	"sync"
)

func HashBlob(height uint64, blob []byte) (hash types.Hash, err error) {

	if seed, err := client.GetDefaultClient().GetSeedByHeight(height); err != nil {
		return types.ZeroHash, err
	} else {
		return hasher.Hash(seed[:], blob)
	}
}

var blockHeaderByHash = lru.New[types.Hash, *Header](128)
var blockHeaderByHashLock sync.Mutex

func GetBlockHeaderByHeight(height uint64) *Header {
	//TODO: cache
	if header, err := client.GetDefaultClient().GetBlockHeaderByHeight(height); err != nil {
		return nil
	} else {
		prevHash, _ := types.HashFromString(header.BlockHeader.PrevHash)
		h, _ := types.HashFromString(header.BlockHeader.Hash)
		return &Header{
			MajorVersion: uint8(header.BlockHeader.MajorVersion),
			MinorVersion: uint8(header.BlockHeader.MinorVersion),
			Timestamp:    uint64(header.BlockHeader.Timestamp),
			PreviousId:   prevHash,
			Height:       header.BlockHeader.Difficulty,
			Nonce:        uint32(header.BlockHeader.Nonce),
			Reward:       header.BlockHeader.Reward,
			Id:           h,
			Difficulty:   types.DifficultyFrom64(header.BlockHeader.Difficulty),
		}
	}
}

func GetBlockHeaderByHash(hash types.Hash) *Header {
	blockHeaderByHashLock.Lock()
	defer blockHeaderByHashLock.Unlock()
	if h := blockHeaderByHash.Get(hash); h == nil {
		if header, err := client.GetDefaultClient().GetBlockHeaderByHash(hash); err != nil || len(header.BlockHeaders) != 1 {
			return nil
		} else {
			prevHash, _ := types.HashFromString(header.BlockHeaders[0].PrevHash)
			blockHash, _ := types.HashFromString(header.BlockHeaders[0].Hash)
			blockHeader := &Header{
				MajorVersion: uint8(header.BlockHeaders[0].MajorVersion),
				MinorVersion: uint8(header.BlockHeaders[0].MinorVersion),
				Timestamp:    uint64(header.BlockHeaders[0].Timestamp),
				PreviousId:   prevHash,
				Height:       header.BlockHeaders[0].Height,
				Nonce:        uint32(header.BlockHeaders[0].Nonce),
				Reward:       header.BlockHeaders[0].Reward,
				Id:           blockHash,
				Difficulty:   types.DifficultyFrom64(header.BlockHeaders[0].Difficulty),
			}

			blockHeaderByHash.Set(hash, blockHeader)

			return blockHeader
		}
	} else {
		return *h
	}
}

func GetLastBlockHeader() *Header {
	if header, err := client.GetDefaultClient().GetLastBlockHeader(); err != nil {
		return nil
	} else {
		prevHash, _ := types.HashFromString(header.BlockHeader.PrevHash)
		h, _ := types.HashFromString(header.BlockHeader.Hash)
		return &Header{
			MajorVersion: uint8(header.BlockHeader.MajorVersion),
			MinorVersion: uint8(header.BlockHeader.MinorVersion),
			Timestamp:    uint64(header.BlockHeader.Timestamp),
			PreviousId:   prevHash,
			Height:       header.BlockHeader.Difficulty,
			Nonce:        uint32(header.BlockHeader.Nonce),
			Reward:       header.BlockHeader.Reward,
			Id:           h,
			Difficulty:   types.DifficultyFrom64(header.BlockHeader.Difficulty),
		}
	}
}
