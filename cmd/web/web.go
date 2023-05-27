package main

import (
	"bytes"
	"flag"
	"fmt"
	cmdutils "git.gammaspectra.live/P2Pool/p2pool-observer/cmd/utils"
	"git.gammaspectra.live/P2Pool/p2pool-observer/cmd/web/views"
	"git.gammaspectra.live/P2Pool/p2pool-observer/index"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero"
	address2 "git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	types2 "git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"github.com/gorilla/mux"
	"github.com/valyala/quicktemplate"
	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"
	"io"
	"log"
	"math"
	"net/http"
	_ "net/http/pprof"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

func toUint64(t any) uint64 {
	if x, ok := t.(uint64); ok {
		return x
	} else if x, ok := t.(int64); ok {
		return uint64(x)
	} else if x, ok := t.(uint); ok {
		return uint64(x)
	} else if x, ok := t.(int); ok {
		return uint64(x)
	} else if x, ok := t.(uint32); ok {
		return uint64(x)
	} else if x, ok := t.(types2.SoftwareId); ok {
		return uint64(x)
	} else if x, ok := t.(types2.SoftwareVersion); ok {
		return uint64(x)
	} else if x, ok := t.(int32); ok {
		return uint64(x)
	} else if x, ok := t.(float64); ok {
		return uint64(x)
	} else if x, ok := t.(float32); ok {
		return uint64(x)
	} else if x, ok := t.(string); ok {
		if n, err := strconv.ParseUint(x, 10, 0); err == nil {
			return n
		}
	}

	return 0
}

func toString(t any) string {

	if s, ok := t.(string); ok {
		return s
	} else if h, ok := t.(types.Hash); ok {
		return h.String()
	}

	return ""
}

func toInt64(t any) int64 {
	if x, ok := t.(uint64); ok {
		return int64(x)
	} else if x, ok := t.(int64); ok {
		return x
	} else if x, ok := t.(uint); ok {
		return int64(x)
	} else if x, ok := t.(uint32); ok {
		return int64(x)
	} else if x, ok := t.(int32); ok {
		return int64(x)
	} else if x, ok := t.(int); ok {
		return int64(x)
	} else if x, ok := t.(float64); ok {
		return int64(x)
	} else if x, ok := t.(float32); ok {
		return int64(x)
	} else if x, ok := t.(string); ok {
		if n, err := strconv.ParseInt(x, 10, 0); err == nil {
			return n
		}
	}

	return 0
}

func toFloat64(t any) float64 {
	if x, ok := t.(float64); ok {
		return x
	} else if x, ok := t.(float32); ok {
		return float64(x)
	} else if x, ok := t.(uint64); ok {
		return float64(x)
	} else if x, ok := t.(int64); ok {
		return float64(x)
	} else if x, ok := t.(uint); ok {
		return float64(x)
	} else if x, ok := t.(int); ok {
		return float64(x)
	} else if x, ok := t.(string); ok {
		if n, err := strconv.ParseFloat(x, 0); err == nil {
			return n
		}
	}

	return 0
}

//go:generate go run github.com/valyala/quicktemplate/qtc@v1.7.0 -dir=views
func main() {

	var responseBufferPool sync.Pool
	responseBufferPool.New = func() any {
		return make([]byte, 0, 1024*1024) //1 MiB allocations
	}

	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	//monerod related
	moneroHost := flag.String("host", "127.0.0.1", "IP address of your Monero node")
	moneroRpcPort := flag.Uint("rpc-port", 18081, "monerod RPC API port number")
	debugListen := flag.String("debug-listen", "", "Provide a bind address and port to expose a pprof HTTP API on it.")
	flag.Parse()

	client.SetDefaultClientSettings(fmt.Sprintf("http://%s:%d", *moneroHost, *moneroRpcPort))

	var ircLinkTitle, ircLink, webchatLink, matrixLink string
	ircUrl, err := url.Parse(os.Getenv("SITE_IRC_URL"))
	if err == nil && ircUrl.Host != "" {
		ircLink = ircUrl.String()
		humanHost := ircUrl.Host
		switch strings.Split(humanHost, ":")[0] {
		case "irc.libera.chat":
			humanHost = "libera.chat"
			matrixLink = fmt.Sprintf("https://matrix.to/#/#%s:%s", ircUrl.Fragment, humanHost)
			webchatLink = fmt.Sprintf("https://web.libera.chat/?nick=Guest?#%s", ircUrl.Fragment)
		case "irc.hackint.org":
			humanHost = "hackint.org"
			matrixLink = fmt.Sprintf("https://matrix.to/#/#%s:%s", ircUrl.Fragment, humanHost)
		}
		ircLinkTitle = fmt.Sprintf("#%s@%s", ircUrl.Fragment, humanHost)
	}

	var basePoolInfo *cmdutils.PoolInfoResult

	for {
		t := getTypeFromAPI[cmdutils.PoolInfoResult]("pool_info")
		if t == nil {
			time.Sleep(1)
			continue
		}
		if t.SideChain.Id != types.ZeroHash {
			basePoolInfo = t
			break
		}
		time.Sleep(1)
	}

	consensusData, _ := utils.MarshalJSON(basePoolInfo.SideChain.Consensus)
	consensus, err := sidechain.NewConsensusFromJSON(consensusData)
	if err != nil {
		log.Panic(err)
	}

	log.Printf("Consensus id = %s", consensus.Id())

	var lastPoolInfo atomic.Pointer[cmdutils.PoolInfoResult]

	baseContext := views.GlobalRequestContext{
		DonationAddress: types.DonationAddress,
		SiteTitle:       os.Getenv("SITE_TITLE"),
		//TODO change to args
		NetServiceAddress: os.Getenv("NET_SERVICE_ADDRESS"),
		TorServiceAddress: os.Getenv("TOR_SERVICE_ADDRESS"),
		Consensus:         consensus,
		Pool:              nil,
	}

	baseContext.Socials.Irc.Link = ircLink
	baseContext.Socials.Irc.Title = ircLinkTitle
	baseContext.Socials.Irc.WebChat = webchatLink
	baseContext.Socials.Matrix.Link = matrixLink

	renderPage := func(request *http.Request, writer http.ResponseWriter, page views.ContextSetterPage, pool ...*cmdutils.PoolInfoResult) {
		w := bytes.NewBuffer(responseBufferPool.Get().([]byte))
		defer func() {
			defer responseBufferPool.Put(w.Bytes()[:0])
			_, _ = writer.Write(w.Bytes())
		}()

		ctx := baseContext
		ctx.IsOnion = request.Host == ctx.TorServiceAddress
		if len(pool) == 0 || pool[0] == nil {
			ctx.Pool = lastPoolInfo.Load()
		} else {
			ctx.Pool = pool[0]
		}

		defer func() {
			if err := recover(); err != nil {

				defer func() {
					// error page error'd
					if err := recover(); err != nil {
						w = bytes.NewBuffer(nil)
						writer.Header().Set("content-type", "text/plain")
						_, _ = w.Write([]byte(fmt.Sprintf("%s", err)))
					}
				}()
				w = bytes.NewBuffer(nil)
				writer.WriteHeader(http.StatusInternalServerError)
				errorPage := views.NewErrorPage(http.StatusInternalServerError, "Internal Server Error", err)
				errorPage.SetContext(&ctx)

				views.WritePageTemplate(w, errorPage)
			}
		}()

		page.SetContext(&ctx)

		bufferedWriter := quicktemplate.AcquireWriter(w)
		defer quicktemplate.ReleaseWriter(bufferedWriter)
		views.StreamPageTemplate(bufferedWriter, page)
	}

	serveMux := mux.NewRouter()

	serveMux.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		params := request.URL.Query()
		refresh := 0
		if params.Has("refresh") {
			writer.Header().Set("refresh", "120")
			refresh = 100
		}

		poolInfo := getTypeFromAPI[cmdutils.PoolInfoResult]("pool_info", 5)
		lastPoolInfo.Store(poolInfo)

		secondsPerBlock := float64(poolInfo.MainChain.Difficulty.Lo) / float64(poolInfo.SideChain.Difficulty.Div64(consensus.TargetBlockTime).Lo)

		blocksToFetch := uint64(math.Ceil((((time.Hour*24).Seconds()/secondsPerBlock)*2)/100) * 100)

		blocks := getSliceFromAPI[*index.FoundBlock](fmt.Sprintf("found_blocks?limit=%d", blocksToFetch), 5)
		shares := getSideBlocksFromAPI("side_blocks?limit=50", 5)

		blocksFound := cmdutils.NewPositionChart(30*4, consensus.ChainWindowSize*4)

		tip := int64(poolInfo.SideChain.Height)
		for _, b := range blocks {
			blocksFound.Add(int(tip-int64(b.SideHeight)), 1)
		}

		if len(blocks) > 20 {
			blocks = blocks[:20]
		}

		renderPage(request, writer, &views.IndexPage{
			Refresh: refresh,
			Positions: struct {
				BlocksFound *cmdutils.PositionChart
			}{
				BlocksFound: blocksFound,
			},
			Shares:      shares,
			FoundBlocks: blocks,
		}, poolInfo)
	})

	serveMux.HandleFunc("/api", func(writer http.ResponseWriter, request *http.Request) {
		renderPage(request, writer, &views.ApiPage{})
	})

	serveMux.HandleFunc("/calculate-share-time", func(writer http.ResponseWriter, request *http.Request) {
		poolInfo := getTypeFromAPI[cmdutils.PoolInfoResult]("pool_info", 5)
		lastPoolInfo.Store(poolInfo)
		hashRate := float64(0)
		magnitude := float64(1000)

		params := request.URL.Query()
		if params.Has("hashrate") {
			hashRate = toFloat64(params.Get("hashrate"))
		}
		if params.Has("magnitude") {
			magnitude = toFloat64(params.Get("magnitude"))
		}

		currentHashRate := magnitude * hashRate

		calculatePage := &views.CalculateShareTimePage{
			Hashrate:              hashRate,
			Magnitude:             magnitude,
			Efforts:               nil,
			EstimatedRewardPerDay: 0,
		}

		if currentHashRate > 0 {
			var efforts []views.CalculateShareTimePageEffortEntry

			for _, v := range []float64{25, 50, 75, 100, 150, 200, 300, 400, 500, 600, 700, 800, 900, 1000} {
				efforts = append(efforts, views.CalculateShareTimePageEffortEntry{
					Effort:      v,
					Probability: (1 - math.Exp(-(v / 100))) * 100,
					Between:     (float64(poolInfo.SideChain.Difficulty.Lo) * (v / 100)) / currentHashRate,
					BetweenSolo: (float64(poolInfo.MainChain.Difficulty.Lo) * (v / 100)) / currentHashRate,
				})
			}
			calculatePage.Efforts = efforts

			longWeight := types.DifficultyFrom64(uint64(currentHashRate)).Mul64(3600 * 24)
			calculatePage.EstimatedRewardPerDay = longWeight.Mul64(poolInfo.MainChain.BaseReward).Div(poolInfo.MainChain.NextDifficulty).Lo
		}

		renderPage(request, writer, calculatePage, poolInfo)
	})

	serveMux.HandleFunc("/connectivity-check", func(writer http.ResponseWriter, request *http.Request) {

		params := request.URL.Query()

		var addressPort netip.AddrPort
		var err error
		if params.Has("address") {
			addressPort, err = netip.ParseAddrPort(params.Get("address"))
			if err != nil {
				addr, err := netip.ParseAddr(params.Get("address"))
				if err == nil {
					addressPort = netip.AddrPortFrom(addr, consensus.DefaultPort())
				}
			}
		}

		if addressPort.IsValid() && !addressPort.Addr().IsUnspecified() {
			checkInformation := getTypeFromAPI[types2.P2PoolConnectionCheckInformation]("consensus/connection_check/" + addressPort.String())
			var rawTip *sidechain.PoolBlock
			ourTip := getTypeFromAPI[index.SideBlock]("redirect/tip")
			var theirTip *index.SideBlock
			if checkInformation != nil {
				if buf, err := utils.MarshalJSON(checkInformation.Tip); err == nil && checkInformation.Tip != nil {
					b := sidechain.PoolBlock{}
					if utils.UnmarshalJSON(buf, &b) == nil {
						rawTip = &b
						theirTip = getTypeFromAPI[index.SideBlock](fmt.Sprintf("block_by_id/%s", types.HashFromBytes(b.CoinbaseExtra(sidechain.SideTemplateId))))
					}
				}
			}
			renderPage(request, writer, &views.ConnectivityCheckPage{
				Address:    addressPort,
				YourTip:    theirTip,
				YourTipRaw: rawTip,
				OurTip:     ourTip,
				Check:      checkInformation,
			})
		} else {

			renderPage(request, writer, &views.ConnectivityCheckPage{
				Address:    netip.AddrPort{},
				YourTip:    nil,
				YourTipRaw: nil,
				OurTip:     nil,
				Check:      nil,
			})
		}
	})

	serveMux.HandleFunc("/transaction-lookup", func(writer http.ResponseWriter, request *http.Request) {

		params := request.URL.Query()

		var txId types.Hash
		if params.Has("txid") {
			txId, _ = types.HashFromString(params.Get("txid"))
		}

		if txId != types.ZeroHash {
			fullResult := getTypeFromAPI[cmdutils.TransactionLookupResult](fmt.Sprintf("transaction_lookup/%s", txId.String()))

			if fullResult != nil && fullResult.Id == txId {
				var topMiner *index.TransactionInputQueryResultsMatch
				for i, m := range fullResult.Match {
					if m.Address == nil {
						continue
					} else if topMiner == nil {
						topMiner = &fullResult.Match[i]
					} else {
						if topMiner.Count <= 2 && topMiner.Count == m.Count {
							//if count is not greater
							topMiner = nil
						}
						break
					}
				}

				var topTimestamp, bottomTimestamp uint64 = 0, math.MaxUint64

				getOut := func(outputIndex uint64) *client.Output {
					if i := slices.IndexFunc(fullResult.Outs, func(output client.Output) bool {
						return output.GlobalOutputIndex == outputIndex
					}); i != -1 {
						return &fullResult.Outs[i]
					}
					return nil
				}

				for _, i := range fullResult.Inputs {
					for j, o := range i.MatchedOutputs {
						if o == nil {
							if oi := getOut(i.Input.KeyOffsets[j]); oi != nil {
								if oi.Timestamp != 0 {
									if topTimestamp < oi.Timestamp {
										topTimestamp = oi.Timestamp
									}
									if bottomTimestamp > oi.Timestamp {
										bottomTimestamp = oi.Timestamp
									}
								}
							}
							continue
						}
						if o.Timestamp != 0 {
							if topTimestamp < o.Timestamp {
								topTimestamp = o.Timestamp
							}
							if bottomTimestamp > o.Timestamp {
								bottomTimestamp = o.Timestamp
							}
						}
					}
				}

				timeScaleItems := topTimestamp - bottomTimestamp
				if bottomTimestamp == math.MaxUint64 {
					timeScaleItems = 1
				}
				minerCoinbaseChart := cmdutils.NewPositionChart(170, timeScaleItems)
				minerCoinbaseChart.SetIdle('_')
				minerSweepChart := cmdutils.NewPositionChart(170, timeScaleItems)
				minerSweepChart.SetIdle('_')
				otherCoinbaseMinerChart := cmdutils.NewPositionChart(170, timeScaleItems)
				otherCoinbaseMinerChart.SetIdle('_')
				otherSweepMinerChart := cmdutils.NewPositionChart(170, timeScaleItems)
				otherSweepMinerChart.SetIdle('_')
				noMinerChart := cmdutils.NewPositionChart(170, timeScaleItems)
				noMinerChart.SetIdle('_')

				if topMiner != nil {
					var noMinerCount, minerCount, otherMinerCount uint64
					for _, i := range fullResult.Inputs {
						var isNoMiner, isMiner, isOtherMiner bool
						for j, o := range i.MatchedOutputs {
							if o == nil {
								if oi := getOut(i.Input.KeyOffsets[j]); oi != nil {
									if oi.Timestamp != 0 {
										noMinerChart.Add(int(topTimestamp-oi.Timestamp), 1)
									}
								}
								isNoMiner = true
							} else if topMiner.Address.Compare(o.Address) == 0 {
								isMiner = true
								if o.Timestamp != 0 {
									if o.Coinbase != nil {
										minerCoinbaseChart.Add(int(topTimestamp-o.Timestamp), 1)
									} else if o.Sweep != nil {
										minerSweepChart.Add(int(topTimestamp-o.Timestamp), 1)
									}
								}
							} else {
								isOtherMiner = true
								if o.Timestamp != 0 {
									if o.Coinbase != nil {
										otherCoinbaseMinerChart.Add(int(topTimestamp-o.Timestamp), 1)
									} else if o.Sweep != nil {
										otherSweepMinerChart.Add(int(topTimestamp-o.Timestamp), 1)
									}
								}
							}
						}

						if isMiner {
							minerCount++
						} else if isOtherMiner {
							otherMinerCount++
						} else if isNoMiner {
							noMinerCount++
						}
					}

					minerRatio := float64(minerCount) / float64(len(fullResult.Inputs))
					noMinerRatio := float64(noMinerCount) / float64(len(fullResult.Inputs))
					otherMinerRatio := float64(otherMinerCount) / float64(len(fullResult.Inputs))
					var likelyMiner bool
					if (len(fullResult.Inputs) > 8 && minerRatio >= noMinerRatio && minerRatio > otherMinerRatio) || (len(fullResult.Inputs) > 8 && minerRatio > 0.35 && minerRatio > otherMinerRatio) || (len(fullResult.Inputs) >= 4 && minerRatio > 0.75) {
						likelyMiner = true
					}

					renderPage(request, writer, &views.TransactionLookupPage{
						TransactionId:   txId,
						Result:          fullResult,
						Miner:           topMiner,
						LikelyMiner:     likelyMiner,
						MinerCount:      minerCount,
						NoMinerCount:    noMinerCount,
						OtherMinerCount: otherMinerCount,
						MinerRatio:      minerRatio * 100,
						NoMinerRatio:    noMinerRatio * 100,
						OtherMinerRatio: otherMinerRatio * 100,
						TopTimestamp:    topTimestamp,
						BottomTimestamp: bottomTimestamp,
						Positions: struct {
							MinerCoinbase      *cmdutils.PositionChart
							OtherMinerCoinbase *cmdutils.PositionChart
							MinerSweep         *cmdutils.PositionChart
							OtherMinerSweep    *cmdutils.PositionChart
							NoMiner            *cmdutils.PositionChart
						}{
							MinerCoinbase:      minerCoinbaseChart,
							OtherMinerCoinbase: otherCoinbaseMinerChart,
							MinerSweep:         minerSweepChart,
							OtherMinerSweep:    otherSweepMinerChart,
							NoMiner:            noMinerChart,
						},
					})
					return
				}
			}
		}

		renderPage(request, writer, &views.TransactionLookupPage{
			TransactionId: txId,
		})
	})

	serveMux.HandleFunc("/sweeps", func(writer http.ResponseWriter, request *http.Request) {
		params := request.URL.Query()
		refresh := 0
		if params.Has("refresh") {
			writer.Header().Set("refresh", "600")
			refresh = 600
		}

		var miner *cmdutils.MinerInfoResult
		if params.Has("miner") {
			miner = getTypeFromAPI[cmdutils.MinerInfoResult](fmt.Sprintf("miner_info/%s", params.Get("miner")))
			if miner == nil || miner.Address == nil {
				renderPage(request, writer, views.NewErrorPage(http.StatusNotFound, "Address Not Found", "You need to have mined at least one share in the past. Come back later :)"))
				return
			}
		}

		poolInfo := getTypeFromAPI[cmdutils.PoolInfoResult]("pool_info", 5)
		lastPoolInfo.Store(poolInfo)

		if miner != nil {
			renderPage(request, writer, &views.SweepsPage{
				Refresh: refresh,
				Sweeps:  getSliceFromAPI[*index.MainLikelySweepTransaction](fmt.Sprintf("sweeps/%d?limit=100", miner.Id)),
				Miner:   miner.Address,
			}, poolInfo)
		} else {
			renderPage(request, writer, &views.SweepsPage{
				Refresh: refresh,
				Sweeps:  getSliceFromAPI[*index.MainLikelySweepTransaction]("sweeps?limit=100", 30),
				Miner:   nil,
			}, poolInfo)
		}
	})

	serveMux.HandleFunc("/blocks", func(writer http.ResponseWriter, request *http.Request) {
		params := request.URL.Query()
		refresh := 0
		if params.Has("refresh") {
			writer.Header().Set("refresh", "600")
			refresh = 600
		}

		var miner *cmdutils.MinerInfoResult
		if params.Has("miner") {
			miner = getTypeFromAPI[cmdutils.MinerInfoResult](fmt.Sprintf("miner_info/%s", params.Get("miner")))
			if miner == nil || miner.Address == nil {
				renderPage(request, writer, views.NewErrorPage(http.StatusNotFound, "Address Not Found", "You need to have mined at least one share in the past. Come back later :)"))
				return
			}
		}

		poolInfo := getTypeFromAPI[cmdutils.PoolInfoResult]("pool_info", 5)
		lastPoolInfo.Store(poolInfo)

		if miner != nil {
			renderPage(request, writer, &views.BlocksPage{
				Refresh:     refresh,
				FoundBlocks: getSliceFromAPI[*index.FoundBlock](fmt.Sprintf("found_blocks?&limit=100&miner=%d", miner.Id)),
				Miner:       miner.Address,
			}, poolInfo)
		} else {
			renderPage(request, writer, &views.BlocksPage{
				Refresh:     refresh,
				FoundBlocks: getSliceFromAPI[*index.FoundBlock]("found_blocks?limit=100", 30),
				Miner:       nil,
			}, poolInfo)
		}
	})

	serveMux.HandleFunc("/miners", func(writer http.ResponseWriter, request *http.Request) {
		params := request.URL.Query()
		if params.Has("refresh") {
			writer.Header().Set("refresh", "600")
		}

		poolInfo := getTypeFromAPI[cmdutils.PoolInfoResult]("pool_info", 5)
		lastPoolInfo.Store(poolInfo)

		currentWindowSize := uint64(poolInfo.SideChain.WindowSize)
		windowSize := currentWindowSize
		if poolInfo.SideChain.Height <= windowSize {
			windowSize = consensus.ChainWindowSize
		}
		size := uint64(30)
		cacheTime := 30
		if params.Has("weekly") {
			windowSize = consensus.ChainWindowSize * 4 * 7
			size *= 2
			if params.Has("refresh") {
				writer.Header().Set("refresh", "3600")
			}
			cacheTime = 60
		}

		shares := getSideBlocksFromAPI(fmt.Sprintf("side_blocks_in_window?window=%d&noMainStatus&noUncles", windowSize), cacheTime)

		miners := make(map[uint64]*views.MinersPageMinerEntry)

		tipHeight := poolInfo.SideChain.Height
		wend := tipHeight - windowSize

		tip := shares[0]

		createMiner := func(miner uint64, share *index.SideBlock) {
			if _, ok := miners[miner]; !ok {
				miners[miner] = &views.MinersPageMinerEntry{
					Address:         share.MinerAddress,
					Alias:           share.MinerAlias,
					SoftwareId:      share.SoftwareId,
					SoftwareVersion: share.SoftwareVersion,
					Shares:          cmdutils.NewPositionChart(size, windowSize),
					Uncles:          cmdutils.NewPositionChart(size, windowSize),
				}
			}
		}

		var totalWeight types.Difficulty
		var uncleShareIndex int
		for i, share := range shares {
			miner := share.Miner

			if share.IsUncle() {
				if share.SideHeight <= wend {
					continue
				}
				createMiner(share.Miner, share)
				miners[miner].Uncles.Add(int(int64(tip.SideHeight)-int64(share.SideHeight)), 1)

				unclePenalty := types.DifficultyFrom64(share.Difficulty).Mul64(consensus.UnclePenalty).Div64(100)
				uncleWeight := share.Difficulty - unclePenalty.Lo

				if shares[uncleShareIndex].TemplateId == share.UncleOf {
					parent := shares[uncleShareIndex]
					createMiner(parent.Miner, parent)
					miners[parent.Miner].Weight = miners[parent.Miner].Weight.Add64(unclePenalty.Lo)
				}
				miners[miner].Weight = miners[miner].Weight.Add64(uncleWeight)

				totalWeight = totalWeight.Add64(share.Difficulty)
			} else {
				uncleShareIndex = i
				createMiner(share.Miner, share)
				miners[miner].Shares.Add(int(int64(tip.SideHeight)-int64(share.SideHeight)), 1)
				miners[miner].Weight = miners[miner].Weight.Add64(share.Difficulty)
				totalWeight = totalWeight.Add64(share.Difficulty)
			}
		}

		minerKeys := maps.Keys(miners)
		slices.SortFunc(minerKeys, func(a uint64, b uint64) bool {
			return miners[a].Weight.Cmp(miners[b].Weight) > 0
		})

		sortedMiners := make([]*views.MinersPageMinerEntry, len(minerKeys))

		for i, k := range minerKeys {
			sortedMiners[i] = miners[k]
		}

		renderPage(request, writer, &views.MinersPage{
			Refresh:      0,
			Weekly:       params.Has("weekly"),
			Miners:       sortedMiners,
			WindowWeight: totalWeight,
		}, poolInfo)
	})

	serveMux.HandleFunc("/share/{block:[0-9a-f]+|[0-9]+}", func(writer http.ResponseWriter, request *http.Request) {
		identifier := mux.Vars(request)["block"]
		params := request.URL.Query()

		var block *index.SideBlock
		var coinbase index.MainCoinbaseOutputs
		var rawBlock []byte
		if len(identifier) == 64 {
			block = getTypeFromAPI[index.SideBlock](fmt.Sprintf("block_by_id/%s", identifier))
		} else {
			block = getTypeFromAPI[index.SideBlock](fmt.Sprintf("block_by_height/%s", identifier))
		}

		if block == nil {
			renderPage(request, writer, views.NewErrorPage(http.StatusNotFound, "Share Not Found", nil))
			return
		}
		rawBlock = getFromAPIRaw(fmt.Sprintf("block_by_id/%s/raw", block.MainId))

		coinbase = getSliceFromAPI[index.MainCoinbaseOutput](fmt.Sprintf("block_by_id/%s/coinbase", block.MainId))

		poolInfo := getTypeFromAPI[cmdutils.PoolInfoResult]("pool_info", 5)
		lastPoolInfo.Store(poolInfo)

		var raw *sidechain.PoolBlock
		b := &sidechain.PoolBlock{}
		if b.UnmarshalBinary(consensus, &sidechain.NilDerivationCache{}, rawBlock) == nil {
			raw = b
		}

		payouts := getSliceFromAPI[*index.Payout](fmt.Sprintf("block_by_id/%s/payouts", block.MainId))

		sweepsCount := 0

		var likelySweeps [][]*index.MainLikelySweepTransaction
		if params.Has("sweeps") && block.MinedMainAtHeight && ((int64(poolInfo.MainChain.Height)-int64(block.MainHeight))+1) >= monero.MinerRewardUnlockTime {
			indices := make([]uint64, len(coinbase))
			for i, o := range coinbase {
				indices[i] = o.GlobalOutputIndex
			}
			data, _ := utils.MarshalJSON(indices)
			uri, _ := url.Parse(os.Getenv("API_URL") + "sweeps_by_spending_global_output_indices")
			if response, err := http.DefaultClient.Do(&http.Request{
				Method: "POST",
				URL:    uri,
				Body:   io.NopCloser(bytes.NewReader(data)),
			}); err == nil {
				func() {
					defer response.Body.Close()
					if response.StatusCode == http.StatusOK {
						if data, err := io.ReadAll(response.Body); err == nil {
							r := make([][]*index.MainLikelySweepTransaction, 0, len(indices))
							if utils.UnmarshalJSON(data, &r) == nil && len(r) == len(indices) {
								likelySweeps = r
							}
						}
					}
				}()
			}

			//remove not likely matching outputs
			for oi, sweeps := range likelySweeps {
				likelySweeps[oi] = likelySweeps[oi][:0]
				for _, s := range sweeps {
					if s == nil {
						continue
					}
					if s.Address.Compare(coinbase[oi].MinerAddress) == 0 {
						likelySweeps[oi] = append(likelySweeps[oi], s)
						sweepsCount++
					}
				}
			}

		}

		if block.Timestamp < uint64(time.Now().Unix()-60) {
			writer.Header().Set("cache-control", "public; max-age=604800")
		} else {
			writer.Header().Set("cache-control", "public; max-age=60")
		}

		renderPage(request, writer, &views.SharePage{
			Block:           block,
			PoolBlock:       raw,
			Payouts:         payouts,
			CoinbaseOutputs: coinbase,
			SweepsCount:     sweepsCount,
			Sweeps:          likelySweeps,
		}, poolInfo)
	})

	serveMux.HandleFunc("/miner/{miner:[^ ]+}", func(writer http.ResponseWriter, request *http.Request) {
		params := request.URL.Query()
		refresh := 0
		if params.Has("refresh") {
			writer.Header().Set("refresh", "300")
			refresh = 300
		}
		address := mux.Vars(request)["miner"]
		miner := getTypeFromAPI[cmdutils.MinerInfoResult](fmt.Sprintf("miner_info/%s", address))
		if miner == nil || miner.Address == nil {
			if addr := address2.FromBase58(address); addr != nil {
				miner = &cmdutils.MinerInfoResult{
					Id:                 0,
					Address:            addr,
					LastShareHeight:    0,
					LastShareTimestamp: 0,
				}
			} else {
				renderPage(request, writer, views.NewErrorPage(http.StatusNotFound, "Invalid Address", nil))
				return
			}
		}

		poolInfo := getTypeFromAPI[cmdutils.PoolInfoResult]("pool_info", 5)
		lastPoolInfo.Store(poolInfo)

		const totalWindows = 4
		wsize := consensus.ChainWindowSize * totalWindows

		currentWindowSize := uint64(poolInfo.SideChain.WindowSize)

		tipHeight := poolInfo.SideChain.Height

		var shares, lastShares, lastOrphanedShares []*index.SideBlock

		var lastFound []*index.FoundBlock
		var payouts []*index.Payout
		var sweeps []*index.MainLikelySweepTransaction
		if miner.Id != 0 {
			shares = getSideBlocksFromAPI(fmt.Sprintf("side_blocks_in_window/%d?from=%d&window=%d&noMiner&noMainStatus&noUncles", miner.Id, tipHeight, wsize))
			payouts = getSliceFromAPI[*index.Payout](fmt.Sprintf("payouts/%d?search_limit=1000", miner.Id))
			lastShares = getSideBlocksFromAPI(fmt.Sprintf("side_blocks?limit=50&miner=%d", miner.Id))
			lastOrphanedShares = getSideBlocksFromAPI(fmt.Sprintf("side_blocks?limit=10&miner=%d&inclusion=%d", miner.Id, index.InclusionOrphan))
			lastFound = getSliceFromAPI[*index.FoundBlock](fmt.Sprintf("found_blocks?limit=10&miner=%d", miner.Id))
			sweeps = getSliceFromAPI[*index.MainLikelySweepTransaction](fmt.Sprintf("sweeps/%d?limit=5", miner.Id))
		}

		sharesFound := cmdutils.NewPositionChart(30*totalWindows, consensus.ChainWindowSize*totalWindows)
		unclesFound := cmdutils.NewPositionChart(30*totalWindows, consensus.ChainWindowSize*totalWindows)

		var sharesInWindow, unclesInWindow uint64
		var longDiff, windowDiff types.Difficulty

		wend := tipHeight - currentWindowSize

		foundPayout := cmdutils.NewPositionChart(30*totalWindows, consensus.ChainWindowSize*totalWindows)
		for _, p := range payouts {
			foundPayout.Add(int(int64(tipHeight)-int64(p.SideHeight)), 1)
		}

		var raw *sidechain.PoolBlock

		if len(lastShares) > 0 {
			raw = getTypeFromAPI[sidechain.PoolBlock](fmt.Sprintf("block_by_id/%s/light", lastShares[0].MainId))
			if raw == nil || raw.ShareVersion() == sidechain.ShareVersion_None {
				raw = nil
			}
		}

		for _, share := range shares {
			if share.IsUncle() {

				unclesFound.Add(int(int64(tipHeight)-int64(share.SideHeight)), 1)

				unclePenalty := types.DifficultyFrom64(share.Difficulty).Mul64(consensus.UnclePenalty).Div64(100)
				uncleWeight := share.Difficulty - unclePenalty.Lo

				if i := slices.IndexFunc(shares, func(block *index.SideBlock) bool {
					return block.TemplateId == share.UncleOf
				}); i != -1 {
					if shares[i].SideHeight > wend {
						windowDiff = windowDiff.Add64(unclePenalty.Lo)
					}
					longDiff = longDiff.Add64(unclePenalty.Lo)
				}
				if share.SideHeight > wend {
					windowDiff = windowDiff.Add64(uncleWeight)
					unclesInWindow++
				}
				longDiff = longDiff.Add64(uncleWeight)
			} else {
				sharesFound.Add(int(int64(tipHeight)-toInt64(share.SideHeight)), 1)
				if share.SideHeight > wend {
					windowDiff = windowDiff.Add64(share.Difficulty)
					sharesInWindow++
				}
				longDiff = longDiff.Add64(share.Difficulty)
			}
		}

		if len(payouts) > 10 {
			payouts = payouts[:10]
		}

		minerPage := &views.MinerPage{
			Refresh: refresh,
			Positions: struct {
				Resolution     int
				SeparatorIndex int
				Blocks         *cmdutils.PositionChart
				Uncles         *cmdutils.PositionChart
				Payouts        *cmdutils.PositionChart
			}{
				Resolution:     int(foundPayout.Resolution()),
				SeparatorIndex: int(consensus.ChainWindowSize*totalWindows - currentWindowSize),
				Blocks:         sharesFound,
				Uncles:         unclesFound,
				Payouts:        foundPayout,
			},
			SharesInWindow:     int(sharesInWindow),
			UnclesInWindow:     int(unclesInWindow),
			Weight:             longDiff.Lo,
			WindowWeight:       windowDiff.Lo,
			Miner:              miner,
			LastPoolBlock:      raw,
			LastShares:         lastShares,
			LastOrphanedShares: lastOrphanedShares,
			LastFound:          lastFound,
			LastPayouts:        payouts,
			LastSweeps:         sweeps,
		}

		if windowDiff.Cmp64(0) > 0 {
			longWindowWeight := poolInfo.SideChain.Window.Weight.Mul64(4).Mul64(poolInfo.SideChain.Consensus.ChainWindowSize).Div64(uint64(poolInfo.SideChain.WindowSize))
			averageRewardPerBlock := longDiff.Mul64(poolInfo.MainChain.BaseReward).Div(longWindowWeight).Lo
			minerPage.ExpectedRewardPerDay = longWindowWeight.Mul64(averageRewardPerBlock).Div(poolInfo.MainChain.NextDifficulty).Lo

			expectedRewardNextBlock := windowDiff.Mul64(poolInfo.MainChain.BaseReward).Div(poolInfo.SideChain.Window.Weight).Lo
			minerPage.ExpectedRewardPerWindow = poolInfo.SideChain.Window.Weight.Mul64(expectedRewardNextBlock).Div(poolInfo.MainChain.NextDifficulty).Lo
		}

		totalWeight := poolInfo.SideChain.Window.Weight.Mul64(4).Mul64(poolInfo.SideChain.Consensus.ChainWindowSize).Div64(uint64(poolInfo.SideChain.WindowSize))
		dailyHashRate := poolInfo.SideChain.Difficulty.Mul(longDiff).Div(totalWeight).Div64(consensus.TargetBlockTime).Lo

		hashRate := float64(0)
		magnitude := float64(1000)

		if dailyHashRate >= 1000000000 {
			hashRate = float64(dailyHashRate) / 1000000000
			magnitude = 1000000000
		} else if dailyHashRate >= 1000000 {
			hashRate = float64(dailyHashRate) / 1000000
			magnitude = 1000000
		} else if dailyHashRate >= 1000 {
			hashRate = float64(dailyHashRate) / 1000
			magnitude = 1000
		}

		if params.Has("magnitude") {
			magnitude = toFloat64(params.Get("magnitude"))
		}
		if params.Has("hashrate") {
			hashRate = toFloat64(params.Get("hashrate"))

			if hashRate > 0 && magnitude > 0 {
				dailyHashRate = uint64(hashRate * magnitude)
			}
		}

		minerPage.HashrateLocal = hashRate
		minerPage.MagnitudeLocal = magnitude

		efforts := make([]float64, len(lastShares))
		for i := len(lastShares) - 1; i >= 0; i-- {
			s := lastShares[i]
			if i == (len(lastShares) - 1) {
				efforts[i] = -1
				continue
			}
			previous := lastShares[i+1]

			timeDelta := uint64(utils.Max(int64(s.Timestamp)-int64(previous.Timestamp), 0))

			expectedCumDiff := types.DifficultyFrom64(dailyHashRate).Mul64(timeDelta)

			efforts[i] = float64(expectedCumDiff.Mul64(100).Lo) / float64(s.Difficulty)
		}
		minerPage.LastSharesEfforts = efforts

		renderPage(request, writer, minerPage, poolInfo)
	})

	serveMux.HandleFunc("/miner", func(writer http.ResponseWriter, request *http.Request) {
		params := request.URL.Query()
		if params.Get("address") == "" {
			http.Redirect(writer, request, "/", http.StatusMovedPermanently)
			return
		}
		http.Redirect(writer, request, fmt.Sprintf("/miner/%s", params.Get("address")), http.StatusMovedPermanently)
	})

	serveMux.HandleFunc("/proof/{block:[0-9a-f]+|[0-9]+}/{index:[0-9]+}", func(writer http.ResponseWriter, request *http.Request) {
		identifier := utils.DecodeHexBinaryNumber(mux.Vars(request)["block"])
		requestIndex := toUint64(mux.Vars(request)["index"])

		block := getTypeFromAPI[index.SideBlock](fmt.Sprintf("block_by_id/%s", identifier))

		if block == nil || !block.MinedMainAtHeight {
			renderPage(request, writer, views.NewErrorPage(http.StatusNotFound, "Share Was Not Found", nil))
			return
		}

		raw := getTypeFromAPI[sidechain.PoolBlock](fmt.Sprintf("block_by_id/%s/light", block.MainId))
		if raw == nil || raw.ShareVersion() == sidechain.ShareVersion_None {
			raw = nil
		}

		payouts := getSliceFromAPI[index.MainCoinbaseOutput](fmt.Sprintf("block_by_id/%s/coinbase", block.MainId))

		if raw == nil {
			renderPage(request, writer, views.NewErrorPage(http.StatusNotFound, "Coinbase Was Not Found", nil))
			return
		}

		if uint64(len(payouts)) <= requestIndex {
			renderPage(request, writer, views.NewErrorPage(http.StatusNotFound, "Payout Was Not Found", nil))
			return
		}

		poolInfo := getTypeFromAPI[cmdutils.PoolInfoResult]("pool_info", 5)
		lastPoolInfo.Store(poolInfo)

		renderPage(request, writer, &views.ProofPage{
			Output: &payouts[requestIndex],
			Block:  block,
			Raw:    raw,
		}, poolInfo)
	})

	serveMux.HandleFunc("/payouts/{miner:[0-9]+|4[123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz]+}", func(writer http.ResponseWriter, request *http.Request) {
		params := request.URL.Query()
		refresh := 0
		if params.Has("refresh") {
			writer.Header().Set("refresh", "600")
			refresh = 600
		}

		address := mux.Vars(request)["miner"]
		if params.Has("address") {
			address = params.Get("address")
		}
		miner := getTypeFromAPI[cmdutils.MinerInfoResult](fmt.Sprintf("miner_info/%s", address))

		if miner == nil || miner.Address == nil {
			renderPage(request, writer, views.NewErrorPage(http.StatusNotFound, "Address Not Found", "You need to have mined at least one share in the past. Come back later :)"))
			return
		}

		payouts := getSliceFromAPI[*index.Payout](fmt.Sprintf("payouts/%d?search_limit=0", miner.Id))
		if len(payouts) == 0 {
			renderPage(request, writer, views.NewErrorPage(http.StatusNotFound, "Address Not Found", "You need to have mined at least one share in the past, and a main block found during that period. Come back later :)"))
			return
		}
		renderPage(request, writer, &views.PayoutsPage{
			Total: func() (result uint64) {
				for _, p := range payouts {
					result += p.Reward
				}
				return
			}(),
			Miner:   miner.Address,
			Payouts: payouts,
			Refresh: refresh,
		})
	})

	server := &http.Server{
		Addr:        "0.0.0.0:8444",
		ReadTimeout: time.Second * 2,
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.Method != "GET" && request.Method != "HEAD" {
				writer.WriteHeader(http.StatusForbidden)
				return
			}

			writer.Header().Set("content-type", "text/html; charset=utf-8")

			serveMux.ServeHTTP(writer, request)
		}),
	}

	if *debugListen != "" {
		go func() {
			if err := http.ListenAndServe(*debugListen, nil); err != nil {
				log.Panic(err)
			}
		}()
	}

	if err := server.ListenAndServe(); err != nil {
		log.Panic(err)
	}
}
