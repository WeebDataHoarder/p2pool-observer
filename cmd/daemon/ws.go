package main

import (
	"context"
	"encoding/json"
	utils2 "git.gammaspectra.live/P2Pool/p2pool-observer/cmd/utils"
	"git.gammaspectra.live/P2Pool/p2pool-observer/index"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/api"
	types2 "git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/types"
	"golang.org/x/exp/slices"
	"log"
	"net/http"
	"nhooyr.io/websocket"
	"sync"
	"sync/atomic"
	"time"
)

type listener struct {
	ListenerId uint64
	Write      func(buf []byte)
	Context    context.Context
	Cancel     func()
}

type SortedBuf[T any] struct {
	buf     []T
	compare func(a, b T) int
}

func NewSortedBuf[T any](size int, compare func(a, b T) int) *SortedBuf[T] {
	return &SortedBuf[T]{
		buf:     make([]T, size, size+1),
		compare: compare,
	}
}

func (b *SortedBuf[T]) Insert(value T) bool {
	if b.Has(value) {
		return false
	}
	b.buf = append(b.buf, value)
	slices.SortFunc(b.buf, func(i, j T) bool {
		//keep highest value at 0
		return b.compare(i, j) > 0
	})
	b.buf = b.buf[:len(b.buf)-1]
	return b.Has(value)
}

func (b *SortedBuf[T]) Remove(value T) {
	var zeroValue T
	for i, v := range b.buf {
		if b.compare(value, v) == 0 {
			b.buf[i] = zeroValue
		}
	}
	slices.SortFunc(b.buf, func(i, j T) bool {
		//keep highest value at index 0
		return b.compare(i, j) > 0
	})
}

func (b *SortedBuf[T]) Has(value T) bool {
	for _, v := range b.buf {
		if b.compare(value, v) == 0 {
			//value inserted
			return true
		}
	}
	return false
}

var listenerLock sync.RWMutex
var listenerIdCounter atomic.Uint64
var listeners []*listener

func setupEventHandler(p2api *api.P2PoolApi, indexDb *index.Index) {

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
					Write: func(buf []byte) {
						ctx2, cancel2 := context.WithTimeout(ctx, time.Second*5)
						defer cancel2()
						if c.Write(ctx2, websocket.MessageText, buf) != nil {
							cancel()
						}
					},
					Context: ctx,
					Cancel:  cancel,
				})
				log.Printf("[WS] Client %d attached", listenerId)
			}()
			defer c.Close(websocket.StatusInternalError, "closing")

			c.CloseRead(context.Background())
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

	//remember last few template for events
	sideBlockBuffer := NewSortedBuf[*index.SideBlock](int(p2api.Consensus().ChainWindowSize*2), func(a, b *index.SideBlock) int {
		if a == b {
			return 0
		}
		if a == nil {
			return -1
		} else if b == nil {
			return 1
		}

		if a.EffectiveHeight < b.EffectiveHeight {
			return -1
		} else if a.EffectiveHeight > b.EffectiveHeight {
			return 1
		}
		if a.SideHeight < b.SideHeight {
			return -1
		} else if a.SideHeight > b.SideHeight {
			return 1
		}
		//same height, sort by main id
		return a.MainId.Compare(b.MainId)
	})

	//remember last few found main for events
	foundBlockBuffer := NewSortedBuf[*index.FoundBlock](10, func(a, b *index.FoundBlock) int {
		if a == b {
			return 0
		}
		if a == nil {
			return -1
		} else if b == nil {
			return 1
		}

		if a.MainBlock.Height < b.MainBlock.Height {
			return -1
		} else if a.MainBlock.Height > b.MainBlock.Height {
			return 1
		}

		if a.SideHeight < b.SideHeight {
			return -1
		} else if a.SideHeight > b.SideHeight {
			return 1
		}
		//same height, sort by main id
		return a.MainBlock.Id.Compare(b.MainBlock.Id)
	})

	//initialize
	tip := indexDb.GetSideBlockTip()
	for cur := tip; cur != nil; cur = indexDb.GetTipSideBlockByTemplateId(cur.ParentTemplateId) {
		if (cur.EffectiveHeight + p2api.Consensus().ChainWindowSize) < tip.EffectiveHeight {
			break
		}
		sideBlockBuffer.Insert(cur)
		for u := range indexDb.GetSideBlocksByUncleOfId(cur.TemplateId) {
			sideBlockBuffer.Insert(u)
		}
	}

	for _, b := range indexDb.GetFoundBlocks("", 5) {
		foundBlockBuffer.Insert(b)
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

	var minerData *types2.MinerData
	var minerDataLock sync.Mutex
	getMinerData := func(expectedHeight uint64) *types2.MinerData {
		minerDataLock.Lock()
		defer minerDataLock.Unlock()
		if minerData == nil || minerData.Height < expectedHeight {
			minerData = p2api.MinerData()
		}
		return minerData
	}

	fillSideBlockResult := func(mainTip *index.MainBlock, sideBlock *index.SideBlock) *index.SideBlock {

		miner := indexDb.GetMiner(sideBlock.Miner)
		sideBlock.MinerAddress = miner.Address()
		sideBlock.MinerAlias = miner.Alias()

		mainTipAtHeight := indexDb.GetMainBlockByHeight(sideBlock.MainHeight)
		if mainTipAtHeight != nil {
			sideBlock.MinedMainAtHeight = mainTipAtHeight.Id == sideBlock.MainId
			sideBlock.MainDifficulty = mainTipAtHeight.Difficulty
		} else {
			minerData := getMinerData(sideBlock.MainHeight)
			if minerData.Height == sideBlock.MainHeight {
				sideBlock.MainDifficulty = minerData.Difficulty.Lo
			} else if mainTip.Height == sideBlock.MainHeight {
				sideBlock.MainDifficulty = mainTip.Difficulty
			}
		}

		for u := range indexDb.GetSideBlocksByUncleOfId(sideBlock.TemplateId) {
			sideBlock.Uncles = append(sideBlock.Uncles, index.SideBlockUncleEntry{
				TemplateId: u.TemplateId,
				Miner:      u.Miner,
				SideHeight: u.SideHeight,
				Difficulty: u.Difficulty,
			})
		}
		return sideBlock

	}

	go func() {
		var blocksToReport []*index.SideBlock
		for range time.Tick(time.Second * 1) {
			//reuse
			blocksToReport = blocksToReport[:0]

			func() {
				sideBlocksLock.RLock()
				defer sideBlocksLock.RUnlock()
				mainTip := indexDb.GetMainBlockTip()
				tip := indexDb.GetSideBlockTip()
				for cur := tip; cur != nil; cur = indexDb.GetTipSideBlockByTemplateId(cur.ParentTemplateId) {
					if (cur.EffectiveHeight + p2api.Consensus().ChainWindowSize) < tip.EffectiveHeight {
						break
					}
					var pushedNew bool
					for u := range indexDb.GetSideBlocksByUncleOfId(cur.TemplateId) {
						if sideBlockBuffer.Insert(u) {
							//first time seen
							pushedNew = true
							blocksToReport = append(blocksToReport, fillSideBlockResult(mainTip, u))
						}
					}
					if sideBlockBuffer.Insert(cur) {
						//first time seen
						pushedNew = true
						blocksToReport = append(blocksToReport, fillSideBlockResult(mainTip, cur))
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
						select {
						case <-l.Context.Done():
						default:
							l.Write(buf)
						}
					}
				}
			}()
		}
	}()

	go func() {
		var blocksToReport []*index.FoundBlock
		var unfoundBlocksToReport []*index.SideBlock
		for range time.Tick(time.Second * 1) {
			//reuse
			blocksToReport = blocksToReport[:0]
			unfoundBlocksToReport = unfoundBlocksToReport[:0]

			func() {
				sideBlocksLock.RLock()
				defer sideBlocksLock.RUnlock()
				mainTip := indexDb.GetMainBlockTip()
				for _, m := range foundBlockBuffer.buf {
					if m != nil && indexDb.GetMainBlockById(m.MainBlock.Id) == nil {
						//unfound
						if b := indexDb.GetSideBlockByMainId(m.MainBlock.Id); b != nil {
							unfoundBlocksToReport = append(unfoundBlocksToReport, fillSideBlockResult(mainTip, indexDb.GetSideBlockByMainId(m.MainBlock.Id)))
							foundBlockBuffer.Remove(m)
						}
					}
				}
				for _, b := range indexDb.GetFoundBlocks("", 5) {
					if foundBlockBuffer.Insert(b) {
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
						select {
						case <-l.Context.Done():
						default:
							l.Write(buf)
						}
					}
				}
				for _, b := range blocksToReport {
					coinbaseOutputs := fillMainCoinbaseOutputs(indexDb.GetMainCoinbaseOutputs(b.MainBlock.CoinbaseId))
					if len(coinbaseOutputs) == 0 {
						//report next time
						foundBlockBuffer.Remove(b)
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
						select {
						case <-l.Context.Done():
						default:
							l.Write(buf)
						}
					}
				}
			}()
		}
	}()
}
