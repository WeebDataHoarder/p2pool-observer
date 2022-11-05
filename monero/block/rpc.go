package block

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
)

func HashBlob(height uint64, blob []byte) (hash types.Hash, err error) {

	if seed, err := client.GetClient().GetSeedByHeight(height); err != nil {
		return types.ZeroHash, err
	} else {
		return hasher.Hash(seed[:], blob)
	}
}

func GetBlockHeaderByHeight(height uint64) *Header {
	//TODO: cache
	if header, err := client.GetClient().GetBlockHeaderByHeight(height); err != nil {
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
	if header, err := client.GetClient().GetBlockHeaderByHash(hash); err != nil || len(header.BlockHeaders) != 1 {
		return nil
	} else {
		prevHash, _ := types.HashFromString(header.BlockHeaders[0].PrevHash)
		h, _ := types.HashFromString(header.BlockHeaders[0].Hash)
		return &Header{
			MajorVersion: uint8(header.BlockHeaders[0].MajorVersion),
			MinorVersion: uint8(header.BlockHeaders[0].MinorVersion),
			Timestamp:    uint64(header.BlockHeaders[0].Timestamp),
			PreviousId:   prevHash,
			Height:       header.BlockHeaders[0].Height,
			Nonce:        uint32(header.BlockHeaders[0].Nonce),
			Reward:       header.BlockHeaders[0].Reward,
			Id:           h,
			Difficulty:   types.DifficultyFrom64(header.BlockHeaders[0].Difficulty),
		}
	}
}

func GetLastBlockHeader() *Header {
	if header, err := client.GetClient().GetLastBlockHeader(); err != nil {
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
