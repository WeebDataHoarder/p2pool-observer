package main

import (
	"context"
	"flag"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/cache/archive"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"github.com/floatdrop/lru"
	"math"
	"os"
)

func main() {
	inputConsensus := flag.String("consensus", "config.json", "Input config.json consensus file")
	inputArchive := flag.String("input", "", "Input path for archive database")
	outputArchive := flag.String("output", "", "Output path for archive database")
	moneroHost := flag.String("host", "127.0.0.1", "IP address of your Monero node")
	moneroRpcPort := flag.Uint("rpc-port", 18081, "monerod RPC API port number")

	flag.Parse()

	client.SetDefaultClientSettings(fmt.Sprintf("http://%s:%d", *moneroHost, *moneroRpcPort))

	cf, err := os.ReadFile(*inputConsensus)

	consensus, err := sidechain.NewConsensusFromJSON(cf)
	if err != nil {
		utils.Panic(err)
	}

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

	inputCache, err := archive.NewCache(*inputArchive, consensus, getDifficultyByHeight)
	if err != nil {
		utils.Panic(err)
	}
	defer inputCache.Close()

	outputCache, err := archive.NewCache(*outputArchive, consensus, getDifficultyByHeight)
	if err != nil {
		utils.Panic(err)
	}
	defer outputCache.Close()

	derivationCache := sidechain.NewDerivationLRUCache()

	blockCache := lru.New[types.Hash, *sidechain.PoolBlock](int(consensus.ChainWindowSize * 4))

	getByTemplateId := func(h types.Hash) *sidechain.PoolBlock {
		if v := blockCache.Get(h); v == nil {
			if bs := inputCache.LoadByTemplateId(h); len(bs) != 0 {
				bs[0].Depth.Store(math.MaxUint64)
				blockCache.Set(h, bs[0])
				return bs[0]
			} else if bs = outputCache.LoadByTemplateId(h); len(bs) != 0 {
				bs[0].Depth.Store(math.MaxUint64)
				blockCache.Set(h, bs[0])
				return bs[0]
			} else {
				return nil
			}
		} else {
			return *v
		}
	}

	preAllocatedShares := sidechain.PreAllocateShares(consensus.ChainWindowSize * 4)
	blocksProcessed := 0
	for blocksAtHeight := range inputCache.ScanHeights(0, math.MaxUint64) {
		for i, b := range blocksAtHeight {
			blocksProcessed++
			if _, err := b.PreProcessBlock(consensus, derivationCache, preAllocatedShares, getDifficultyByHeight, getByTemplateId); err != nil {
				utils.Errorf("", "error processing block %s at %d, %s", types.HashFromBytes(b.CoinbaseExtra(sidechain.SideTemplateId)), b.Side.Height, err)
			} else {
				if parent := getByTemplateId(b.Side.Parent); parent != nil {
					topDepth := parent.Depth.Load()
					if topDepth == math.MaxUint64 {
						b.Depth.Store(consensus.ChainWindowSize * 2)
					} else if topDepth == 0 {
						b.Depth.Store(0)
					} else {
						b.Depth.Store(topDepth - 1)
					}
				} else {
					utils.Errorf("", "block %s at %d, could not get parent, fallback", types.HashFromBytes(b.CoinbaseExtra(sidechain.SideTemplateId)), b.Side.Height)
					b.Depth.Store(math.MaxUint64)
				}
				outputCache.Store(b)
				if i == 0 {
					blockCache.Set(types.HashFromBytes(b.CoinbaseExtra(sidechain.SideTemplateId)), b)
				}
			}
		}
	}

	utils.Logf("Archive", "blocks processed %d", blocksProcessed)
}
