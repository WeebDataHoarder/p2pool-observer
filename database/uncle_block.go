package database

import "git.gammaspectra.live/P2Pool/p2pool-observer/types"

type UncleBlock struct {
	Block        Block
	ParentId     types.Hash
	ParentHeight uint64
}

func (u *UncleBlock) GetBlock() *Block {
	return &u.Block
}
