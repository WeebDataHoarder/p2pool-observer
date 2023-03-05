package block

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
)

type GetDifficultyByHeightFunc func(height uint64) types.Difficulty
type GetSeedByHeightFunc func(height uint64) (hash types.Hash)
type GetBlockHeaderByHashFunc func(hash types.Hash) *Header
type GetBlockHeaderByHeightFunc func(height uint64) *Header
