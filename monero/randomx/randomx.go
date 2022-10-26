package randomx

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
)

type Hasher interface {
	Hash(key []byte, input []byte) (types.Hash, error)
}

func SeedHeights(height uint64) (seedHeight, nextHeight uint64) {
	return SeedHeight(height), SeedHeight(height + SeedHashEpochLag)
}

func SeedHeight(height uint64) uint64 {
	if height <= SeedHashEpochBlocks+SeedHashEpochLag {
		return 0
	}

	return (height - SeedHashEpochLag - 1) & (^uint64(SeedHashEpochBlocks - 1))
}

const (
	SeedHashEpochLag    = 64
	SeedHashEpochBlocks = 2048
)
