package main

import (
	"context"
	"flag"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/cache/archive"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/cache/legacy"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"github.com/floatdrop/lru"
	"log"
	"math"
	"os"
)

type loadee struct {
	c  *sidechain.Consensus
	cb func(block *sidechain.PoolBlock)
}

func (l *loadee) Consensus() *sidechain.Consensus {
	return l.c
}

func (l *loadee) AddCachedBlock(block *sidechain.PoolBlock) {
	l.cb(block)
}

func main() {
	inputConsensus := flag.String("consensus", "config.json", "Input config.json consensus file")
	inputFile := flag.String("input", "p2pool.cache", "Input p2pool.cache path")
	outputArchive := flag.String("output", "", "Output path for archive database")

	flag.Parse()

	cf, err := os.ReadFile(*inputConsensus)

	consensus, err := sidechain.NewConsensusFromJSON(cf)
	if err != nil {
		log.Panic(err)
	}

	cache, err := legacy.NewCache(consensus, *inputFile)
	if err != nil {
		log.Panic(err)
	}
	defer cache.Close()

	difficultyCache := lru.New[uint64, types.Difficulty](1024)

	getDifficultyByHeight := func(height uint64) types.Difficulty {
		if v := difficultyCache.Get(height); v == nil {
			if r, err := client.GetDefaultClient().GetBlockHeaderByHeight(height, context.Background()); err == nil {
				d := types.DifficultyFrom64(r.BlockHeader.Difficulty)
				difficultyCache.Set(height, d)
				return d
			}
			return types.ZeroDifficulty
		} else {
			return *v
		}
	}

	archiveCache, err := archive.NewCache(*outputArchive, consensus, getDifficultyByHeight)
	if err != nil {
		log.Panic(err)
	}
	defer archiveCache.Close()

	cachedBlocks := make(map[types.Hash]*sidechain.PoolBlock)

	l := &loadee{
		c: consensus,
		cb: func(block *sidechain.PoolBlock) {
			expectedBlockId := types.HashFromBytes(block.CoinbaseExtra(sidechain.SideTemplateId))
			calculatedBlockId := block.SideTemplateId(consensus)

			if expectedBlockId != calculatedBlockId {
				utils.Errorf("ERROR: block height %d, template id %s, expected %s", block.Side.Height, calculatedBlockId, expectedBlockId)
			} else {
				cachedBlocks[expectedBlockId] = block
			}
		},
	}

	cache.LoadAll(l)

	var storeBlock func(b *sidechain.PoolBlock)
	storeBlock = func(b *sidechain.PoolBlock) {
		if parent := cachedBlocks[b.Side.Parent]; parent != nil {
			b.FillTransactionParentIndices(parent)
			storeBlock(parent)
		}
		b.Depth.Store(math.MaxUint64)
		archiveCache.Store(b)
	}
	for _, b := range cachedBlocks {
		if b.Depth.Load() == math.MaxUint64 {
			continue
		}
		storeBlock(b)
	}
}
