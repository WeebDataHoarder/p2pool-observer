package main

import (
	"context"
	"flag"
	"fmt"
	"git.gammaspectra.live/P2Pool/go-monero/pkg/rpc/daemon"
	utils2 "git.gammaspectra.live/P2Pool/p2pool-observer/cmd/utils"
	"git.gammaspectra.live/P2Pool/p2pool-observer/index"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/randomx"
	p2poolapi "git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/api"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"log"
	"sync/atomic"
	"time"
)

func blockId(b *sidechain.PoolBlock) types.Hash {
	return types.HashFromBytes(b.CoinbaseExtra(sidechain.SideTemplateId))
}

func main() {
	moneroHost := flag.String("host", "127.0.0.1", "IP address of your Monero node")
	moneroRpcPort := flag.Uint("rpc-port", 18081, "monerod RPC API port number")
	startFromHeight := flag.Uint64("from", 0, "Start sync from this height")
	dbString := flag.String("db", "", "")
	p2poolApiHost := flag.String("api-host", "", "Host URL for p2pool go observer consensus")
	fullMode := flag.Bool("full-mode", false, "Allocate RandomX dataset, uses 2GB of RAM")
	flag.Parse()
	randomx.UseFullMemory.Store(*fullMode)

	client.SetDefaultClientSettings(fmt.Sprintf("http://%s:%d", *moneroHost, *moneroRpcPort))

	p2api := p2poolapi.NewP2PoolApi(*p2poolApiHost)

	for status := p2api.Status(); !p2api.Status().Synchronized; status = p2api.Status() {
		log.Printf("[API] Not synchronized (height %d, id %s), waiting five seconds", status.Height, status.Id)
		time.Sleep(time.Second * 5)
	}

	log.Printf("[CHAIN] Consensus id = %s\n", p2api.Consensus().Id())

	indexDb, err := index.OpenIndex(*dbString, p2api.Consensus(), p2api.DifficultyByHeight, p2api.SeedByHeight, p2api.ByTemplateId)
	if err != nil {
		log.Panic(err)
	}
	defer indexDb.Close()

	dbTip := indexDb.GetSideBlockTip()

	var tipHeight uint64
	if dbTip != nil {
		tipHeight = dbTip.SideHeight
	}
	log.Printf("[CHAIN] Last known database tip is %d\n", tipHeight)

	window, uncles := p2api.StateFromTip()
	if *startFromHeight != 0 {
		tip := p2api.BySideHeight(*startFromHeight)
		if len(tip) != 0 {
			window, uncles = p2api.WindowFromTemplateId(blockId(tip[0]))
		} else {
			tip = p2api.BySideHeight(*startFromHeight + p2api.Consensus().ChainWindowSize)
			if len(tip) != 0 {
				window, uncles = p2api.WindowFromTemplateId(blockId(tip[0]))
			}
		}
	}

	insertFromTip := func(tip *sidechain.PoolBlock) {
		if indexDb.GetTipSideBlockByTemplateId(blockId(tip)) != nil {
			//reached old tip
			return
		}
		for cur := tip; cur != nil; cur = p2api.ByTemplateId(cur.Side.Parent) {
			log.Printf("[CHAIN] Inserting share %s at height %d\n", blockId(cur).String(), cur.Side.Height)
			for _, u := range cur.Side.Uncles {
				log.Printf("[CHAIN] Inserting uncle %s at parent height %d\n", u.String(), cur.Side.Height)
			}
			if err := indexDb.InsertOrUpdatePoolBlock(cur, index.InclusionInVerifiedChain); err != nil {
				log.Panic(err)
			}
			if indexDb.GetTipSideBlockByTemplateId(cur.Side.Parent) != nil {
				//reached old tip
				break
			}
		}
	}

	for len(window) > 0 {
		log.Printf("[CHAIN] Found range %d -> %d (%s to %s), %d shares, %d uncles", window[0].Side.Height, window[len(window)-1].Side.Height, window[0].SideTemplateId(p2api.Consensus()), window[len(window)-1].SideTemplateId(p2api.Consensus()), len(window), len(uncles))
		for _, b := range window {
			indexDb.CachePoolBlock(b)
		}
		for _, u := range uncles {
			indexDb.CachePoolBlock(u)
		}
		for _, b := range window {
			if indexDb.GetTipSideBlockByTemplateId(b.SideTemplateId(p2api.Consensus())) != nil {
				//reached old tip
				window = nil
				break
			}
			log.Printf("[CHAIN] Inserting share %s at height %d\n", blockId(b).String(), b.Side.Height)
			for _, u := range b.Side.Uncles {
				log.Printf("[CHAIN] Inserting uncle %s at parent height %d\n", u.String(), b.Side.Height)
			}
			if err := indexDb.InsertOrUpdatePoolBlock(b, index.InclusionInVerifiedChain); err != nil {
				log.Panic(err)
			}
		}

		if len(window) == 0 {
			break
		}

		parent := p2api.ByTemplateId(window[len(window)-1].Side.Parent)
		if parent == nil {
			break
		}
		window, uncles = p2api.WindowFromTemplateId(blockId(parent))
		if len(window) == 0 {
			insertFromTip(parent)
			break
		}
	}

	var maxHeight, currentHeight uint64
	if err = indexDb.Query("SELECT (SELECT MAX(main_height) FROM side_blocks) AS max_height, (SELECT MAX(height) FROM main_blocks) AS current_height;", func(row index.RowScanInterface) error {
		return row.Scan(&maxHeight, &currentHeight)
	}); err != nil {
		log.Panic(err)
	}

	ctx := context.Background()

	scanHeader := func(h daemon.BlockHeader) error {
		if err := utils2.FindAndInsertMainHeader(h, indexDb, func(b *sidechain.PoolBlock) {
			p2api.InsertAlternate(b)
		}, client.GetDefaultClient(), indexDb.GetDifficultyByHeight, indexDb.GetByTemplateId, p2api.ByMainId, p2api.LightByMainHeight, func(b *sidechain.PoolBlock) error {
			_, err := b.PreProcessBlock(p2api.Consensus(), &sidechain.NilDerivationCache{}, sidechain.PreAllocateShares(p2api.Consensus().ChainWindowSize*2), indexDb.GetDifficultyByHeight, indexDb.GetByTemplateId)
			return err
		}); err != nil {
			return err
		}
		return nil
	}

	heightCount := maxHeight - 1 - currentHeight + 1

	const strideSize = 1000
	strides := heightCount / strideSize

	//backfill headers
	for stride := uint64(0); stride <= strides; stride++ {
		start := currentHeight + stride*strideSize
		end := utils.Min(maxHeight-1, currentHeight+stride*strideSize+strideSize)
		log.Printf("checking %d to %d", start, end)
		if headers, err := client.GetDefaultClient().GetBlockHeadersRangeResult(start, end, ctx); err != nil {
			log.Panic(err)
		} else {
			for _, h := range headers.Headers {
				if err := scanHeader(h); err != nil {
					log.Panic(err)
					continue
				}
			}
		}
	}

	var doCheckOfOldBlocks atomic.Bool

	doCheckOfOldBlocks.Store(true)

	go func() {
		//do deep scan for any missed main headers or deep reorgs every once in a while
		for range time.NewTicker(time.Second * monero.BlockTime).C {
			if !doCheckOfOldBlocks.Load() {
				continue
			}
			mainTip := indexDb.GetMainBlockTip()
			for h := mainTip.Height; h >= 0 && h >= (mainTip.Height-10); h-- {
				header := indexDb.GetMainBlockByHeight(h)
				if header == nil {
					break
				}
				cur, _ := client.GetDefaultClient().GetBlockHeaderByHash(header.Id, ctx)
				if cur == nil {
					break
				}
				if err := scanHeader(*cur); err != nil {
					log.Panic(err)
				}
			}
		}
	}()

	for {
		currentTip := indexDb.GetSideBlockTip()
		currentMainTip := indexDb.GetMainBlockTip()

		tip := p2api.Tip()
		mainTip := p2api.MainTip()

		if blockId(tip) != currentTip.TemplateId {
			if tip.Side.Height < currentTip.SideHeight {
				//wtf
				log.Panicf("tip height less than ours, abort: %d < %d", tip.Side.Height, currentTip.SideHeight)
			} else {
				insertFromTip(tip)
			}
		}

		if mainTip.Id != currentMainTip.Id {
			if mainTip.Height < currentMainTip.Height {
				//wtf
				log.Panicf("main tip height less than ours, abort: %d < %d", mainTip.Height, currentMainTip.Height)
			} else {
				var prevHash types.Hash
				for cur, _ := client.GetDefaultClient().GetBlockHeaderByHash(mainTip.Id, ctx); cur != nil; cur, _ = client.GetDefaultClient().GetBlockHeaderByHash(prevHash, ctx) {
					curHash, _ := types.HashFromString(cur.Hash)
					curDb := indexDb.GetMainBlockByHeight(cur.Height)
					if curDb != nil {
						if curDb.Id == curHash {
							break
						} else { //there has been a swap
							doCheckOfOldBlocks.Store(true)
						}
					}
					log.Printf("[MAIN] Insert main block %d, id %s", cur.Height, curHash)
					doCheckOfOldBlocks.Store(true)

					if err := scanHeader(*cur); err != nil {
						log.Panic(err)
					}
					prevHash, _ = types.HashFromString(cur.PrevHash)
				}
			}
		}

		time.Sleep(time.Second * 1)
	}
}
