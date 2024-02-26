package main

import (
	"context"
	"git.gammaspectra.live/P2Pool/p2pool-observer/cmd/index"
	cmdutils "git.gammaspectra.live/P2Pool/p2pool-observer/cmd/utils"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/api"
	types2 "git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"log"
	"net/http"
	"nhooyr.io/websocket"
	"slices"
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
	slices.SortFunc(b.buf, func(i, j T) int {
		//keep highest value at 0
		return b.compare(i, j) * -1
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
	slices.SortFunc(b.buf, func(i, j T) int {
		//keep highest value at index 0
		return b.compare(i, j) * -1
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
				utils.Logf("[WS] Client %d detached after %.02f seconds", listenerId, time.Now().Sub(requestTime).Seconds())
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
				utils.Logf("[WS] Client %d attached", listenerId)
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
		index.QueryIterate(indexDb.GetSideBlocksByUncleOfId(cur.TemplateId), func(_ int, u *index.SideBlock) (stop bool) {
			sideBlockBuffer.Insert(u)
			return false
		})
	}

	func() {
		foundBlocks, _ := indexDb.GetFoundBlocks("", 5)
		index.QueryIterate(foundBlocks, func(_ int, b *index.FoundBlock) (stop bool) {
			foundBlockBuffer.Insert(b)
			return false
		})
	}()

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

		index.QueryIterate(indexDb.GetSideBlocksByUncleOfId(sideBlock.TemplateId), func(_ int, u *index.SideBlock) (stop bool) {
			sideBlock.Uncles = append(sideBlock.Uncles, index.SideBlockUncleEntry{
				TemplateId: u.TemplateId,
				Miner:      u.Miner,
				SideHeight: u.SideHeight,
				Difficulty: u.Difficulty,
			})
			return false
		})
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

					index.QueryIterate(indexDb.GetSideBlocksByUncleOfId(cur.TemplateId), func(_ int, u *index.SideBlock) (stop bool) {
						if sideBlockBuffer.Insert(u) {
							//first time seen
							pushedNew = true
							blocksToReport = append(blocksToReport, fillSideBlockResult(mainTip, u))
						}
						return false
					})
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
			slices.SortFunc(blocksToReport, func(a, b *index.SideBlock) int {
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

			func() {
				listenerLock.RLock()
				defer listenerLock.RUnlock()
				for _, b := range blocksToReport {
					buf, err := utils.MarshalJSON(&cmdutils.JSONEvent{
						Type:                cmdutils.JSONEventSideBlock,
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

					ts := time.Now().Unix()

					// Send webhooks
					go func(b *index.SideBlock) {
						q, _ := indexDb.GetMinerWebHooks(b.Miner)
						index.QueryIterate(q, func(_ int, w *index.MinerWebHook) (stop bool) {
							if err := cmdutils.SendSideBlock(w, ts, b.MinerAddress, b); err != nil {
								utils.Logf("[WebHook] Error sending %s webhook to %s: type %s, url %s: %s", cmdutils.JSONEventSideBlock, b.MinerAddress.ToBase58(), w.Type, w.Url, err)
							}
							return false
						})
					}(b)
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

				func() {
					foundBlocks, _ := indexDb.GetFoundBlocks("", 5)
					index.QueryIterate(foundBlocks, func(_ int, b *index.FoundBlock) (stop bool) {
						if foundBlockBuffer.Insert(b) {
							//first time seen
							blocksToReport = append(blocksToReport, fillFoundBlockResult(b))
						}
						return false
					})
				}()
			}()

			//sort for proper order
			slices.SortFunc(blocksToReport, func(a, b *index.FoundBlock) int {
				if a.MainBlock.Height < b.MainBlock.Height {
					return -1
				} else if a.MainBlock.Height > b.MainBlock.Height {
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
				return a.MainBlock.Id.Compare(b.MainBlock.Id)
			})

			//sort for proper order
			slices.SortFunc(unfoundBlocksToReport, func(a, b *index.SideBlock) int {
				if a.MainHeight < b.MainHeight {
					return -1
				} else if a.MainHeight > b.MainHeight {
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

			func() {
				listenerLock.RLock()
				defer listenerLock.RUnlock()
				for _, b := range unfoundBlocksToReport {
					buf, err := utils.MarshalJSON(&cmdutils.JSONEvent{
						Type:                cmdutils.JSONEventOrphanedBlock,
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

					ts := time.Now().Unix()

					// Send webhooks
					go func(b *index.SideBlock) {
						q, _ := indexDb.GetMinerWebHooks(b.Miner)
						index.QueryIterate(q, func(_ int, w *index.MinerWebHook) (stop bool) {
							if err := cmdutils.SendOrphanedBlock(w, ts, b.MinerAddress, b); err != nil {
								utils.Logf("[WebHook] Error sending %s webhook to %s: type %s, url %s: %s", cmdutils.JSONEventOrphanedBlock, b.MinerAddress.ToBase58(), w.Type, w.Url, err)
							}
							return false
						})
					}(b)
				}
				for _, b := range blocksToReport {
					coinbaseOutputs := func() index.MainCoinbaseOutputs {
						mainOutputs, err := indexDb.GetMainCoinbaseOutputs(b.MainBlock.CoinbaseId)
						if err != nil {
							panic(err)
						}
						defer mainOutputs.Close()
						return fillMainCoinbaseOutputs(index.IterateToSliceWithoutPointer[index.MainCoinbaseOutput](mainOutputs))
					}()

					if len(coinbaseOutputs) == 0 {
						//report next time
						foundBlockBuffer.Remove(b)
						continue
					}
					buf, err := utils.MarshalJSON(&cmdutils.JSONEvent{
						Type:                cmdutils.JSONEventFoundBlock,
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

					includingHeight := max(b.EffectiveHeight, b.SideHeight)
					if uint64(b.WindowDepth) > includingHeight {
						includingHeight = 0
					} else {
						includingHeight -= uint64(b.WindowDepth)
					}

					// Send webhooks on outputs
					for _, o := range coinbaseOutputs {
						payout := &index.Payout{
							Miner:             o.Miner,
							TemplateId:        b.MainBlock.SideTemplateId,
							SideHeight:        b.SideHeight,
							UncleOf:           b.UncleOf,
							MainId:            b.MainBlock.Id,
							MainHeight:        b.MainBlock.Height,
							Timestamp:         b.MainBlock.Timestamp,
							CoinbaseId:        b.MainBlock.CoinbaseId,
							Reward:            o.Value,
							PrivateKey:        b.MainBlock.CoinbasePrivateKey,
							Index:             uint64(o.Index),
							GlobalOutputIndex: o.GlobalOutputIndex,
							IncludingHeight:   includingHeight,
						}

						addr := o.MinerAddress

						ts := time.Now().Unix()

						// One goroutine per entry
						go func() {
							q, _ := indexDb.GetMinerWebHooks(payout.Miner)
							index.QueryIterate(q, func(_ int, w *index.MinerWebHook) (stop bool) {
								if err := cmdutils.SendFoundBlock(w, ts, addr, b, coinbaseOutputs); err != nil {
									utils.Logf("[WebHook] Error sending %s webhook to %s: type %s, url %s: %s", cmdutils.JSONEventFoundBlock, addr.ToBase58(), w.Type, w.Url, err)
								}

								if err := cmdutils.SendPayout(w, ts, addr, payout); err != nil {
									utils.Logf("[WebHook] Error sending %s webhook to %s: type %s, url %s: %s", cmdutils.JSONEventPayout, addr.ToBase58(), w.Type, w.Url, err)
								}
								return false
							})
						}()
					}
				}
			}()
		}
	}()
}
