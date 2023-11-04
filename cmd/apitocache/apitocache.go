package main

import (
	"flag"
	"fmt"
	cmdutils "git.gammaspectra.live/P2Pool/p2pool-observer/cmd/utils"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/cache/legacy"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"log"
	"slices"
	"strconv"
)

func main() {
	selectedApiUrl := flag.String("api", "", "Input API url, for example, https://p2pool.observer/api/")
	outputFile := flag.String("output", "p2pool.cache", "Output p2pool.cache path")
	fromBlock := flag.String("from", "tip", "Block to start from. Can be an ID or a height")

	flag.Parse()

	apiUrl = *selectedApiUrl

	poolInfo := getTypeFromAPI[cmdutils.PoolInfoResult]("pool_info")
	if poolInfo == nil {
		panic("could not fetch consensus")
	}

	consensusData, _ := utils.MarshalJSON(poolInfo.SideChain.Consensus)
	consensus, err := sidechain.NewConsensusFromJSON(consensusData)
	if err != nil {
		log.Panic(err)
	}

	log.Printf("Consensus id = %s", consensus.Id)

	cache, err := legacy.NewCache(consensus, *outputFile)
	if err != nil {
		log.Panic(err)
	}
	defer cache.Close()

	var toFetchUrls []string

	if *fromBlock == "tip" {
		toFetchUrls = append(toFetchUrls, fmt.Sprintf("redirect/tip/raw"))
	} else if n, err := strconv.ParseUint(*fromBlock, 10, 0); err == nil {
		toFetchUrls = append(toFetchUrls, fmt.Sprintf("block_by_height/%d/raw", n))
	} else {
		toFetchUrls = append(toFetchUrls, fmt.Sprintf("block_by_id/%s/raw", *fromBlock))
	}

	var fetches int

	addBlockId := func(h types.Hash) {
		k := fmt.Sprintf("block_by_id/%s/raw", h)
		if slices.Contains(toFetchUrls, k) {
			return
		}
		toFetchUrls = append(toFetchUrls, k)
	}

	for len(toFetchUrls) > 0 {
		nextUrl := toFetchUrls[0]
		fmt.Printf("[%d] fetching %s\n", fetches, nextUrl)
		toFetchUrls = slices.Delete(toFetchUrls, 0, 1)

		rawBlock := getFromAPIRaw(nextUrl)
		b := &sidechain.PoolBlock{}
		err := b.UnmarshalBinary(consensus, &sidechain.NilDerivationCache{}, rawBlock)
		if err != nil {
			panic(fmt.Errorf("could not fetch block from url %s: %w", nextUrl, err))
		}

		cache.Store(b)

		for _, u := range b.Side.Uncles {
			addBlockId(u)
		}
		addBlockId(b.Side.Parent)

		fetches++

		if fetches >= legacy.NumBlocks {
			print("reached max limit of block cache, exiting\n")
			break
		}

	}

	cache.Flush()
}
