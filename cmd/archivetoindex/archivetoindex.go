package main

import (
	"context"
	"flag"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/cmd/index"
	cmdutils "git.gammaspectra.live/P2Pool/p2pool-observer/cmd/utils"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/block"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/randomx"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/cache/archive"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"github.com/floatdrop/lru"
	"log"
	"math"
	"os"
	"sync"
)

func main() {
	inputConsensus := flag.String("consensus", "config.json", "Input config.json consensus file")
	inputArchive := flag.String("input", "", "Input path for archive database")
	moneroHost := flag.String("host", "127.0.0.1", "IP address of your Monero node")
	moneroRpcPort := flag.Uint("rpc-port", 18081, "monerod RPC API port number")
	connString := flag.String("conn", "", "Connection string for postgres database")

	flag.Parse()

	client.SetDefaultClientSettings(fmt.Sprintf("http://%s:%d", *moneroHost, *moneroRpcPort))
	client.GetDefaultClient().SetThrottle(1000)

	cf, err := os.ReadFile(*inputConsensus)

	consensus, err := sidechain.NewConsensusFromJSON(cf)
	if err != nil {
		log.Panic(err)
	}
	if err = consensus.InitHasher(2, randomx.FlagSecure, randomx.FlagFullMemory); err != nil {
		log.Panic(err)
	}

	var headerCacheLock sync.RWMutex
	headerByHeightCache := make(map[uint64]*block.Header)
	headerByIdCache := make(map[types.Hash]*block.Header)

	getHeaderByHeight := func(height uint64) *block.Header {
		if v := func() *block.Header {
			headerCacheLock.RLock()
			defer headerCacheLock.RUnlock()
			return headerByHeightCache[height]
		}(); v == nil {
			if r, err := client.GetDefaultClient().GetBlockHeaderByHeight(height, context.Background()); err == nil {
				headerCacheLock.Lock()
				defer headerCacheLock.Unlock()
				prevHash, _ := types.HashFromString(r.BlockHeader.PrevHash)
				h, _ := types.HashFromString(r.BlockHeader.Hash)
				header := &block.Header{
					MajorVersion: uint8(r.BlockHeader.MajorVersion),
					MinorVersion: uint8(r.BlockHeader.MinorVersion),
					Timestamp:    uint64(r.BlockHeader.Timestamp),
					PreviousId:   prevHash,
					Height:       r.BlockHeader.Height,
					Nonce:        uint32(r.BlockHeader.Nonce),
					Reward:       r.BlockHeader.Reward,
					Id:           h,
					Difficulty:   types.DifficultyFrom64(r.BlockHeader.Difficulty),
				}
				headerByIdCache[header.Id] = header
				headerByHeightCache[header.Height] = header
				return header
			}
			return nil
		} else {
			return v
		}
	}

	getDifficultyByHeight := func(height uint64) types.Difficulty {
		if v := getHeaderByHeight(height); v != nil {
			return v.Difficulty
		}
		return types.ZeroDifficulty
	}

	getSeedByHeight := func(height uint64) (hash types.Hash) {
		seedHeight := randomx.SeedHeight(height)
		if v := getHeaderByHeight(seedHeight); v != nil {
			return v.Id
		}
		return types.ZeroHash
	}

	archiveCache, err := archive.NewCache(*inputArchive, consensus, getDifficultyByHeight)
	if err != nil {
		log.Panic(err)
	}
	defer archiveCache.Close()

	blockCache := lru.New[types.Hash, *sidechain.PoolBlock](int(consensus.ChainWindowSize * 4))

	derivationCache := sidechain.NewDerivationLRUCache()
	getByTemplateIdDirect := func(h types.Hash) *sidechain.PoolBlock {
		if v := blockCache.Get(h); v == nil {
			if bs := archiveCache.LoadByTemplateId(h); len(bs) != 0 {
				blockCache.Set(h, bs[0])
				return bs[0]
			} else {
				return nil
			}
		} else {
			return *v
		}
	}

	processBlock := func(b *sidechain.PoolBlock) error {
		var preAllocatedShares sidechain.Shares
		if len(b.Main.Coinbase.Outputs) == 0 {
			//cannot use SideTemplateId() as it might not be proper to calculate yet. fetch from coinbase only here
			if b2 := getByTemplateIdDirect(types.HashFromBytes(b.CoinbaseExtra(sidechain.SideTemplateId))); b2 != nil && len(b2.Main.Coinbase.Outputs) != 0 {
				b.Main.Coinbase.Outputs = b2.Main.Coinbase.Outputs
			} else {
				preAllocatedShares = sidechain.PreAllocateShares(consensus.ChainWindowSize * 2)
			}
		}

		_, err := b.PreProcessBlock(consensus, derivationCache, preAllocatedShares, getDifficultyByHeight, getByTemplateIdDirect)
		return err
	}

	getByTemplateId := func(h types.Hash) *sidechain.PoolBlock {
		if v := getByTemplateIdDirect(h); v != nil {
			if processBlock(v) != nil {
				return nil
			}
			return v
		} else {
			return nil
		}
	}

	indexDb, err := index.OpenIndex(*connString, consensus, getDifficultyByHeight, getSeedByHeight, getByTemplateIdDirect)
	if err != nil {
		log.Panic(err)
	}
	defer indexDb.Close()

	totalStored := 0

	var lastRangeHeight, rangeStart uint64 = math.MaxUint64, math.MaxUint64
	var lastTipEntries []*sidechain.PoolBlock

	type rangeEntry struct {
		//todo: check time of range
		startHeight uint64
		tipHeight   uint64
		tipEntries  []*sidechain.PoolBlock
	}

	var heightRanges []rangeEntry

	/*id, _ := types.HashFromString("b83a96d30c7db3b15a65fa43ddcf6914bb4e176dea49a94a5263c9adca93d4cc")
	heightRanges = append(heightRanges, rangeEntry{
		startHeight: 3087255,
		tipHeight:   4728558,
		tipEntries:  []*sidechain.PoolBlock{getByTemplateId(id)},
	})*/

	if len(heightRanges) == 0 {
		for blocksAtHeight := range archiveCache.ScanHeights(0, math.MaxUint64) {
			if len(blocksAtHeight) == 0 {
				log.Panicf("no blocks at %d + 1?", lastRangeHeight)
			}
			if lastRangeHeight == math.MaxUint64 {
				rangeStart = blocksAtHeight[0].Side.Height
			} else if blocksAtHeight[0].Side.Height != (lastRangeHeight + 1) { // new range
				heightRanges = append(heightRanges, rangeEntry{
					startHeight: rangeStart,
					tipHeight:   lastRangeHeight,
					tipEntries:  lastTipEntries,
				})
				utils.Logf("range %d -> %d, total of %d height(s)", rangeStart, lastRangeHeight, lastRangeHeight-rangeStart+1)
				utils.Logf("missing %d -> %d, total of %d height(s)", lastRangeHeight+1, blocksAtHeight[0].Side.Height-1, (blocksAtHeight[0].Side.Height-1)-(lastRangeHeight+1)+1)
				rangeStart = blocksAtHeight[0].Side.Height
			}
			lastRangeHeight = blocksAtHeight[0].Side.Height
			totalStored += len(blocksAtHeight)
			lastTipEntries = blocksAtHeight
		}
		utils.Logf("range %d -> %d, total of %d height(s)", rangeStart, lastRangeHeight, lastRangeHeight-rangeStart+1)
		heightRanges = append(heightRanges, rangeEntry{
			startHeight: rangeStart,
			tipHeight:   lastRangeHeight,
			tipEntries:  lastTipEntries,
		})

		utils.Logf("total stored %d", totalStored)
	}

	var lastTime, lastHeight uint64 = math.MaxUint64, math.MaxUint64

	for i := len(heightRanges) - 1; i >= 0; i-- {
		r := heightRanges[i]

		var bestTip *sidechain.PoolBlock
		if len(r.tipEntries) == 0 {
			bestTip = r.tipEntries[0]
		} else {
			for _, b := range r.tipEntries {
				if (b.Main.Timestamp - 60*5) > lastTime { //2m offset of max time drift
					continue
				}

				if bestTip == nil {
					bestTip = b
				} else if bestTip.Main.Coinbase.GenHeight < b.Main.Coinbase.GenHeight {
					bestTip = b
				} else if bestTip.Main.Timestamp < b.Main.Timestamp {
					bestTip = b
				}
			}
		}

		if bestTip == nil {
			utils.Logf("skipped range %d to %d due to: nil tip", r.startHeight, r.tipHeight)
			continue
		} else if bestTip.Main.Coinbase.GenHeight > lastHeight {
			utils.Logf("skipped range %d to %d due to: main height %d > %d", r.startHeight, r.tipHeight, bestTip.Main.Coinbase.GenHeight, lastHeight)
			continue
		} else if (bestTip.Main.Timestamp - 60*5) > lastTime {
			utils.Logf("skipped range %d to %d due to: timestamp %d > %d", r.startHeight, r.tipHeight, bestTip.Main.Timestamp, lastTime)
			continue
		}

		if err := processBlock(bestTip); err != nil {
			utils.Logf("skipped range %d to %d due to: could not process tip: %s", r.startHeight, r.tipHeight, err)
			continue
		}

		lastTime = bestTip.Main.Timestamp
		lastHeight = bestTip.Main.Coinbase.GenHeight

		if r.startHeight-r.tipHeight > consensus.ChainWindowSize*2 &&
			indexDb.GetTipSideBlockByTemplateId(bestTip.SideTemplateId(consensus)) != nil &&
			indexDb.GetTipSideBlockByHeight(r.startHeight+consensus.ChainWindowSize+1) != nil &&
			index.QueryHasResults(indexDb.GetSideBlocksByHeight(r.startHeight)) {
			continue
		}

		// skip inserted heights
		for bestTip != nil && indexDb.GetTipSideBlockByTemplateId(bestTip.SideTemplateId(consensus)) != nil {
			utils.Logf("skip id = %s, template id = %s, height = %d", bestTip.MainId(), bestTip.SideTemplateId(consensus), bestTip.Side.Height)
			bestTip = getByTemplateId(bestTip.Side.Parent)
		}

		for cur := bestTip; cur != nil; cur = getByTemplateId(cur.Side.Parent) {
			utils.Logf("id = %s, template id = %s, height = %d", cur.MainId(), cur.SideTemplateId(consensus), cur.Side.Height)

			if err = indexDb.InsertOrUpdatePoolBlock(cur, index.InclusionInVerifiedChain); err != nil {
				utils.Logf("error inserting %s, %s at %d: %s", cur.SideTemplateId(consensus), cur.MainId(), cur.Side.Height, err)
				break
			}

			lastTime = cur.Main.Timestamp
			lastHeight = cur.Main.Coinbase.GenHeight

			curId := cur.SideTemplateId(consensus)

			for _, e := range archiveCache.LoadBySideChainHeight(cur.Side.Height) {
				if cur.FullId() == e.FullId() {
					continue
				}
				timeDiff := int64(e.Main.Timestamp) - int64(cur.Main.Timestamp)
				if timeDiff < 0 {
					timeDiff = -timeDiff
				}
				if timeDiff > 3600*12 { //More than 12 hours difference, do not include
					continue
				}
				if processBlock(e) != nil {
					utils.Logf("error processing orphan/alternate %s, %s at %d: %s", e.SideTemplateId(consensus), e.MainId(), e.Side.Height, err)
					continue
				}
				if indexDb.GetSideBlockByMainId(e.MainId()) != nil {
					continue
				}
				if curId == e.SideTemplateId(consensus) {
					if err = indexDb.InsertOrUpdatePoolBlock(e, index.InclusionAlternateInVerifiedChain); err != nil {
						log.Panicf("error inserting alternate %s, %s at %d: %s", e.SideTemplateId(consensus), e.MainId(), e.Side.Height, err)
						break
					}
				} else {
					if err = indexDb.InsertOrUpdatePoolBlock(e, index.InclusionOrphan); err != nil {
						log.Panicf("error inserting orphan %s, %s at %d: %s", e.SideTemplateId(consensus), e.MainId(), e.Side.Height, err)
						break
					}
				}
			}
		}

		blockCache = lru.New[types.Hash, *sidechain.PoolBlock](int(consensus.ChainWindowSize * 4))
		headerByIdCache = make(map[types.Hash]*block.Header)
		headerByHeightCache = make(map[uint64]*block.Header)
		derivationCache.Clear()
	}

	var maxHeight, minHeight uint64
	if err := indexDb.Query("SELECT MAX(main_height), MIN(main_height) FROM side_blocks WHERE inclusion = $1;", func(row index.RowScanInterface) error {
		return row.Scan(&maxHeight, &minHeight)
	}, index.InclusionInVerifiedChain); err != nil {
		log.Panic(err)
	}

	heightCount := maxHeight - minHeight + 1

	const strideSize = 1000
	strides := heightCount / strideSize

	ctx := context.Background()

	for stride := uint64(0); stride <= strides; stride++ {
		start := minHeight + stride*strideSize
		end := min(maxHeight, minHeight+stride*strideSize+strideSize)
		utils.Logf("checking %d to %d", start, end)
		if headers, err := client.GetDefaultClient().GetBlockHeadersRangeResult(start, end, ctx); err != nil {
			log.Panic(err)
		} else {
			for _, h := range headers.Headers {
				if err := cmdutils.FindAndInsertMainHeader(h, indexDb, func(b *sidechain.PoolBlock) {
					archiveCache.Store(b)
				}, client.GetDefaultClient(), getDifficultyByHeight, getByTemplateIdDirect, archiveCache.LoadByMainId, archiveCache.LoadByMainChainHeight, processBlock); err != nil {
					log.Panic(err)
					continue
				}
			}
		}
	}

	mainBlocks, _ := indexDb.GetMainBlocksByQuery("WHERE side_template_id IS NOT NULL ORDER BY height DESC;")
	index.QueryIterate(mainBlocks, func(_ int, mb *index.MainBlock) (stop bool) {
		if err := cmdutils.FindAndInsertMainHeaderOutputs(mb, indexDb, client.GetDefaultClient(), getDifficultyByHeight, getByTemplateIdDirect, archiveCache.LoadByMainId, archiveCache.LoadByMainChainHeight, processBlock); err != nil {
			log.Print(err)
		}
		return false
	})
}
