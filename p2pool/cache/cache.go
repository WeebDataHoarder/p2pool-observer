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
	LoadByTemplateId(id types.Hash) sidechain.UniquePoolBlockSlice
	LoadBySideChainHeight(height uint64) sidechain.UniquePoolBlockSlice
	LoadByMainChainHeight(height uint64) sidechain.UniquePoolBlockSlice

	// ProcessBlock blocks returned on other Load methods may return pruned/compact blocks. Use this to process them
	ProcessBlock(block *sidechain.PoolBlock) error
}

type IndexedCache interface {
	AddressableCache
}
