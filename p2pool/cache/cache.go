package cache

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
)

type Cache interface {
	Store(block *sidechain.PoolBlock)
	Close()
}

type Loadee interface {
	sidechain.ConsensusProvider
	AddCachedBlock(block *sidechain.PoolBlock)
}

type HeapCache interface {
	Cache
	LoadAll(l Loadee)
}

type AddressableCache interface {
	Cache

	RemoveByMainId(id types.Hash)
	RemoveByTemplateId(id types.Hash)

	LoadByMainId(id types.Hash) *sidechain.PoolBlock
	// LoadByTemplateId returns a slice of loaded blocks. If more than one, these might have colliding nonce / extra nonce values
	LoadByTemplateId(id types.Hash) []*sidechain.PoolBlock
	LoadBySideChainHeight(height uint64) []*sidechain.PoolBlock
	LoadByMainChainHeight(height uint64) []*sidechain.PoolBlock
}

type IndexedCache interface {
	AddressableCache
}
