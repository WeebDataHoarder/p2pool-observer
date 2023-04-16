package main

import (
	"flag"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/cache/legacy"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"log"
	"os"
	"path"
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
	outputFolder := flag.String("output", "shares", "Output path for extracted shares")

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

	l := &loadee{
		c: consensus,
		cb: func(block *sidechain.PoolBlock) {
			expectedBlockId := types.HashFromBytes(block.CoinbaseExtra(sidechain.SideTemplateId))
			calculatedBlockId := block.SideTemplateId(consensus)

			if expectedBlockId != calculatedBlockId {
				log.Printf("ERROR: block height %d, template id %s, expected %s", block.Side.Height, calculatedBlockId, expectedBlockId)
			} else {
				blob, err := block.MarshalBinary()
				if err != nil {
					log.Panic(err)
				}
				log.Printf("block height %d, template id %s, version %d", block.Side.Height, calculatedBlockId, block.ShareVersion())

				_ = os.WriteFile(path.Join(*outputFolder, expectedBlockId.String()+".raw"), blob, 0664)
			}
		},
	}

	cache.LoadAll(l)
}
