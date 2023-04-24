package main

import (
	"context"
	"encoding/json"
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
	types2 "git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"golang.org/x/exp/slices"
	"log"
	"net/http"
	"nhooyr.io/websocket"
	"sync"
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

	for status := p2api.Status(); !status.Synchronized; status = p2api.Status() {
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

	var listenerLock sync.RWMutex
	var listenerIdCounter atomic.Uint64
	type listener struct {
		ListenerId    uint64
		SideBlock     func(buf []byte)
		FoundBlock    func(buf []byte)
		OrphanedBlock func(buf []byte)
		Context       context.Context
		Cancel        func()
	}
	var listeners []*listener

	server := &http.Server{
		Addr:        "0.0.0.0:8787",
		ReadTimeout: time.Second * 2,
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			requestTime := time.Now()
			c, err := websocket.Accept(writer, request, nil)
			if err != nil {
				writer.WriteHeader(http.StatusInternalServerError)
				return
			}
			listenerId := listenerIdCounter.Add(1)
			defer func() {
				listenerLock.Lock()
				defer listenerLock.Unlock()
				if i := slices.IndexFunc(listeners, func(listener *listener) bool {
					return listener.ListenerId == listenerId
				}); i != -1 {
					listeners = slices.Delete(listeners, i, i+1)
				}
				log.Printf("[WS] Client %d detached after %.02f seconds", listenerId, time.Now().Sub(requestTime).Seconds())
			}()

			ctx, cancel := context.WithCancel(request.Context())
			defer cancel()
			func() {
				listenerLock.Lock()
				defer listenerLock.Unlock()
				listeners = append(listeners, &listener{
					ListenerId: listenerId,
					SideBlock: func(buf []byte) {
						if c.Write(ctx, websocket.MessageText, buf) != nil {
							cancel()
						}
					},
					FoundBlock: func(buf []byte) {
						if c.Write(ctx, websocket.MessageText, buf) != nil {
							cancel()
						}
					},
					OrphanedBlock: func(buf []byte) {
						if c.Write(ctx, websocket.MessageText, buf) != nil {
							cancel()
						}
					},
					Context: ctx,
					Cancel:  cancel,
				})
				log.Printf("[WS] Client %d attached", listenerId)
			}()
			defer c.Close(websocket.StatusInternalError, "closing")
			//TODO: read only
			select {
			case <-ctx.Done():
				//wait
			}

		}),
	}

	go func() {
		if err := server.ListenAndServe(); err != nil {
			log.Panic(err)
		}
	}()

	go func() {
		//do deep scan for any missed main headers or deep reorgs every once in a while
		for range time.NewTicker(time.Second * monero.BlockTime).C {
			if !doCheckOfOldBlocks.Load() {
				continue
			}
			mainTip := indexDb.GetMainBlockTip()
			for h := mainTip.Height; h >= 0 && h >= (mainTip.Height-monero.TransactionUnlockTime); h-- {
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

	go func() {
		//process older full blocks and sweeps
		for range time.NewTicker(time.Second * monero.BlockTime).C {

			actualTip := indexDb.GetMainBlockTip()
			mainTip := actualTip
			maxDepth := mainTip.Height - randomx.SeedHashEpochBlocks*4

			//find top start height
			for h := mainTip.Height - monero.TransactionUnlockTime; h >= maxDepth; h-- {
				mainTip = indexDb.GetMainBlockByHeight(h)
				if mainTip == nil {
					continue
				}
				if isProcessed, ok := mainTip.GetMetadata("processed").(bool); ok && isProcessed {
					break
				}
			}

			if mainTip.Height == maxDepth {
				log.Printf("Reached maxdepth %d: Use scansweeps to backfill data", maxDepth)
			}

			for h := mainTip.Height - monero.MinerRewardUnlockTime; h <= actualTip.Height-monero.TransactionUnlockTime; h++ {
				b := indexDb.GetMainBlockByHeight(h)
				if b == nil {
					continue
				}
				if isProcessed, ok := b.GetMetadata("processed").(bool); ok && isProcessed {
					continue
				}

				if err := utils2.ProcessFullBlock(b, indexDb); err != nil {
					log.Printf("error processing block %s at %d: %s", b.Id, b.Height, err)
				}
			}
		}
	}()

	//remember last few template ids for events
	sideBlockIdBuffer := utils.NewCircularBuffer[types.Hash](int(p2api.Consensus().ChainWindowSize * 2))
	//remember last few found main ids for events
	foundBlockIdBuffer := utils.NewCircularBuffer[types.Hash](10)
	//initialize
	tip := indexDb.GetSideBlockTip()
	for cur := tip; cur != nil; cur = indexDb.GetTipSideBlockByTemplateId(cur.ParentTemplateId) {
		if (cur.EffectiveHeight + p2api.Consensus().ChainWindowSize) < tip.EffectiveHeight {
			break
		}
		sideBlockIdBuffer.PushUnique(cur.TemplateId)
		for u := range indexDb.GetSideBlocksByUncleId(cur.TemplateId) {
			sideBlockIdBuffer.PushUnique(u.TemplateId)
		}
	}
	for _, b := range indexDb.GetFoundBlocks("", 5) {
		foundBlockIdBuffer.PushUnique(b.MainBlock.Id)
	}

	fillMainCoinbaseOutputs := func(outputs index.MainCoinbaseOutputs) (result index.MainCoinbaseOutputs) {
		result = make(index.MainCoinbaseOutputs, 0, len(outputs))
		for _, output := range outputs {
			miner := indexDb.GetMiner(output.Miner)
			output.MinerAddress = miner.Address()
			output.MinerAlias = miner.Alias()
			result = append(result, output)
		}
		return result
	}

	fillFoundBlockResult := func(foundBlock *index.FoundBlock) *index.FoundBlock {
		miner := indexDb.GetMiner(foundBlock.Miner)
		foundBlock.MinerAddress = miner.Address()
		foundBlock.MinerAlias = miner.Alias()
		return foundBlock
	}

	fillSideBlockResult := func(mainTip *index.MainBlock, minerData *types2.MinerData, sideBlock *index.SideBlock) *index.SideBlock {

		miner := indexDb.GetMiner(sideBlock.Miner)
		sideBlock.MinerAddress = miner.Address()
		sideBlock.MinerAlias = miner.Alias()

		mainTipAtHeight := indexDb.GetMainBlockByHeight(sideBlock.MainHeight)
		if mainTipAtHeight != nil {
			sideBlock.MinedMainAtHeight = mainTipAtHeight.Id == sideBlock.MainId
			sideBlock.MainDifficulty = mainTipAtHeight.Difficulty
		} else {
			if minerData.Height == sideBlock.MainHeight {
				sideBlock.MainDifficulty = minerData.Difficulty.Lo
			} else if mainTip.Height == sideBlock.MainHeight {
				sideBlock.MainDifficulty = mainTip.Difficulty
			}
		}

		for u := range indexDb.GetSideBlocksByUncleId(sideBlock.TemplateId) {
			sideBlock.Uncles = append(sideBlock.Uncles, index.SideBlockUncleEntry{
				TemplateId: u.TemplateId,
				Miner:      u.Miner,
				SideHeight: u.SideHeight,
				Difficulty: u.Difficulty,
			})
		}
		return sideBlock

	}

	var sideBlocksLock sync.RWMutex
	go func() {
		var blocksToReport []*index.SideBlock
		for range time.NewTicker(time.Second * 1).C {
			//reuse
			blocksToReport = blocksToReport[:0]

			func() {
				sideBlocksLock.RLock()
				defer sideBlocksLock.RUnlock()
				minerData := p2api.MinerData()
				mainTip := indexDb.GetMainBlockTip()
				tip := indexDb.GetSideBlockTip()
				for cur := tip; cur != nil; cur = indexDb.GetTipSideBlockByTemplateId(cur.ParentTemplateId) {
					if (cur.EffectiveHeight + p2api.Consensus().ChainWindowSize) < tip.EffectiveHeight {
						break
					}
					var pushedNew bool
					for u := range indexDb.GetSideBlocksByUncleId(cur.TemplateId) {
						if sideBlockIdBuffer.PushUnique(u.TemplateId) {
							//first time seen
							pushedNew = true
							blocksToReport = append(blocksToReport, fillSideBlockResult(mainTip, minerData, u))
						}
					}
					if sideBlockIdBuffer.PushUnique(cur.TemplateId) {
						//first time seen
						pushedNew = true
						blocksToReport = append(blocksToReport, fillSideBlockResult(mainTip, minerData, cur))
					}

					if !pushedNew {
						break
					}
				}
			}()

			//sort for proper order
			slices.SortFunc(blocksToReport, func(a, b *index.SideBlock) bool {
				if a.EffectiveHeight < b.EffectiveHeight {
					return true
				} else if a.EffectiveHeight > b.EffectiveHeight {
					return false
				}
				if a.SideHeight < b.SideHeight {
					return true
				} else if a.SideHeight > b.SideHeight {
					return false
				}
				//same height, sort by main id
				return a.MainId.Compare(b.MainId) < 0
			})

			func() {
				listenerLock.RLock()
				defer listenerLock.RUnlock()
				for _, b := range blocksToReport {
					buf, err := json.Marshal(&utils2.JSONEvent{
						Type:                utils2.JSONEventSideBlock,
						SideBlock:           b,
						FoundBlock:          nil,
						MainCoinbaseOutputs: nil,
					})
					if err != nil {
						continue
					}
					for _, l := range listeners {
						if l.SideBlock == nil {
							continue
						}
						select {
						case <-l.Context.Done():
						default:
							l.SideBlock(buf)
						}
					}
				}
			}()
		}
	}()

	go func() {
		var blocksToReport []*index.FoundBlock
		var unfoundBlocksToReport []*index.SideBlock
		for range time.NewTicker(time.Second * 1).C {
			//reuse
			blocksToReport = blocksToReport[:0]
			unfoundBlocksToReport = unfoundBlocksToReport[:0]

			func() {

				minerData := p2api.MinerData()
				mainTip := indexDb.GetMainBlockTip()
				for _, mainId := range foundBlockIdBuffer.Slice() {
					if indexDb.GetMainBlockById(mainId) == nil {
						//unfound
						unfoundBlocksToReport = append(unfoundBlocksToReport, fillSideBlockResult(mainTip, minerData, indexDb.GetSideBlockByMainId(mainId)))
					}
				}
				for _, b := range indexDb.GetFoundBlocks("", 5) {
					if foundBlockIdBuffer.PushUnique(b.MainBlock.Id) {
						//first time seen
						blocksToReport = append(blocksToReport, fillFoundBlockResult(b))
					}
				}
			}()

			//sort for proper order
			slices.SortFunc(blocksToReport, func(a, b *index.FoundBlock) bool {
				if a.MainBlock.Height < b.MainBlock.Height {
					return true
				} else if a.MainBlock.Height > b.MainBlock.Height {
					return false
				}
				if a.EffectiveHeight < b.EffectiveHeight {
					return true
				} else if a.EffectiveHeight > b.EffectiveHeight {
					return false
				}
				if a.SideHeight < b.SideHeight {
					return true
				} else if a.SideHeight > b.SideHeight {
					return false
				}
				//same height, sort by main id
				return a.MainBlock.Id.Compare(b.MainBlock.Id) < 0
			})

			//sort for proper order
			slices.SortFunc(unfoundBlocksToReport, func(a, b *index.SideBlock) bool {
				if a.MainHeight < b.MainHeight {
					return true
				} else if a.MainHeight > b.MainHeight {
					return false
				}
				if a.EffectiveHeight < b.EffectiveHeight {
					return true
				} else if a.EffectiveHeight > b.EffectiveHeight {
					return false
				}
				if a.SideHeight < b.SideHeight {
					return true
				} else if a.SideHeight > b.SideHeight {
					return false
				}
				//same height, sort by main id
				return a.MainId.Compare(b.MainId) < 0
			})

			func() {
				listenerLock.RLock()
				defer listenerLock.RUnlock()
				for _, b := range unfoundBlocksToReport {
					buf, err := json.Marshal(&utils2.JSONEvent{
						Type:                utils2.JSONEventOrphanedBlock,
						SideBlock:           b,
						FoundBlock:          nil,
						MainCoinbaseOutputs: nil,
					})
					if err != nil {
						continue
					}
					for _, l := range listeners {
						if l.OrphanedBlock == nil {
							continue
						}
						select {
						case <-l.Context.Done():
						default:
							l.OrphanedBlock(buf)
						}
					}
				}
				for _, b := range blocksToReport {
					coinbaseOutputs := fillMainCoinbaseOutputs(indexDb.GetMainCoinbaseOutputs(b.MainBlock.CoinbaseId))
					if len(coinbaseOutputs) == 0 {
						continue
					}
					buf, err := json.Marshal(&utils2.JSONEvent{
						Type:                utils2.JSONEventFoundBlock,
						SideBlock:           nil,
						FoundBlock:          b,
						MainCoinbaseOutputs: coinbaseOutputs,
					})
					if err != nil {
						continue
					}
					for _, l := range listeners {
						if l.FoundBlock == nil {
							continue
						}
						select {
						case <-l.Context.Done():
						default:
							l.FoundBlock(buf)
						}
					}
				}
			}()
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
				func() {
					sideBlocksLock.Lock()
					defer sideBlocksLock.Unlock()
					insertFromTip(tip)
				}()
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
