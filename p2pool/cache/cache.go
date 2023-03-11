package cache

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
)

type Cache interface {
	Store(block *sidechain.PoolBlock)
}

type Loadee interface {
	Consensus() *sidechain.Consensus
	AddCachedBlock(block *sidechain.PoolBlock)
}

type HeapCache interface {
	Cache
	LoadAll(l Loadee)
}

type AddressableCache interface {
	Remove(hash types.Hash)
	Load(hash types.Hash) *sidechain.PoolBlock
}