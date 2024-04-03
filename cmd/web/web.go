package main

import (
	"bytes"
	"compress/gzip"
	"embed"
	"flag"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/cmd/httputils"
	"git.gammaspectra.live/P2Pool/p2pool-observer/cmd/index"
	cmdutils "git.gammaspectra.live/P2Pool/p2pool-observer/cmd/utils"
	"git.gammaspectra.live/P2Pool/p2pool-observer/cmd/web/views"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero"
	address2 "git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	types2 "git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"github.com/andybalholm/brotli"
	"github.com/goccy/go-json"
	"github.com/gorilla/mux"
	"github.com/klauspost/compress/zstd"
	"github.com/valyala/quicktemplate"
	"io"
	"math"
	"mime"
	"net/http"
	_ "net/http/pprof"
	"net/netip"
	"net/url"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

//go:embed assets
var assets embed.FS

const (
	compressionNone = iota
	compressionGzip
	compressionBrotli
	compressionZstd
)

type compressedAsset struct {
	Data            []byte
	ContentEncoding httputils.ContentEncoding
	ETag            string
}

var compressedAssets = make(map[string][]compressedAsset)

func init() {
	fmt.Printf("Compressing assets")
	start := time.Now()
	dirList, err := assets.ReadDir("assets")
	if err != nil {
		panic(err)
	}
	for _, e := range dirList {
		filePath := "assets/" + e.Name()
		file, err := assets.ReadFile(filePath)
		if err != nil {
			panic(err)
		}
		compressedAssets[filePath] = append(compressedAssets[filePath], compressedAsset{
			Data:            file,
			ContentEncoding: "",
			ETag:            fmt.Sprintf("\"%s\"", crypto.Keccak256Single(file).String()),
		})

		{
			buf := bytes.NewBuffer(nil)
			gzipWriter, err := gzip.NewWriterLevel(buf, gzip.BestCompression)
			if err != nil {
				panic(err)
			}
			_, err = gzipWriter.Write(file)
			if err != nil {
				panic(err)
			}
			err = gzipWriter.Flush()
			if err != nil {
				panic(err)
			}
			gzipWriter.Close()
			compressedAssets[filePath] = append(compressedAssets[filePath], compressedAsset{
				Data:            buf.Bytes(),
				ContentEncoding: httputils.ContentEncodingGzip,
				ETag:            fmt.Sprintf("\"%s\"", crypto.Keccak256Single(buf.Bytes()).String()),
			})
		}

		{
			buf := bytes.NewBuffer(nil)
			brotliWriter := brotli.NewWriterLevel(buf, brotli.BestCompression)

			_, err = brotliWriter.Write(file)
			if err != nil {
				panic(err)
			}
			err = brotliWriter.Flush()
			if err != nil {
				panic(err)
			}
			brotliWriter.Close()
			compressedAssets[filePath] = append(compressedAssets[filePath], compressedAsset{
				Data:            buf.Bytes(),
				ContentEncoding: httputils.ContentEncodingBrotli,
				ETag:            fmt.Sprintf("\"%s\"", crypto.Keccak256Single(buf.Bytes()).String()),
			})
		}

		{

			zstdWriter, err := zstd.NewWriter(nil,
				zstd.WithEncoderLevel(zstd.SpeedBestCompression),
				//zstd.WithEncoderConcurrency(runtime.NumCPU()),
				//zstd.WithEncoderCRC(false),
				//zstd.WithWindowSize(zstd.MaxWindowSize),
				//zstd.WithZeroFrames(true),
			)
			if err != nil {
				panic(err)
			}

			buf := zstdWriter.EncodeAll(file, nil)
			zstdWriter.Close()
			compressedAssets[filePath] = append(compressedAssets[filePath], compressedAsset{
				Data:            buf,
				ContentEncoding: httputils.ContentEncodingZstd,
				ETag:            fmt.Sprintf("\"%s\"", crypto.Keccak256Single(buf).String()),
			})
		}

	}

	fmt.Printf(" DONE in %s\n", time.Now().Sub(start).String())
}

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
		splitChan := strings.Split(ircUrl.Fragment, "/")

		if len(splitChan) > 1 {
			ircLink = strings.ReplaceAll(ircLink, "/"+splitChan[1], "")
		}

		switch strings.Split(humanHost, ":")[0] {
		case "irc.libera.chat":
			if len(splitChan) > 1 {
				matrixLink = fmt.Sprintf("https://matrix.to/#/#%s:%s?via=matrix.org&via=%s", splitChan[0], splitChan[1], splitChan[1])
				webchatLink = fmt.Sprintf("https://web.libera.chat/?nick=Guest?#%s", splitChan[0])
			} else {
				humanHost = "libera.chat"
				matrixLink = fmt.Sprintf("https://matrix.to/#/#%s:%s?via=matrix.org", ircUrl.Fragment, humanHost)
				webchatLink = fmt.Sprintf("https://web.libera.chat/?nick=Guest%%3F#%s", ircUrl.Fragment)
			}
		case "irc.hackint.org":
			if len(splitChan) > 1 {
				matrixLink = fmt.Sprintf("https://matrix.to/#/#%s:%s?via=matrix.org&via=%s", splitChan[0], splitChan[1], splitChan[1])
			} else {
				humanHost = "hackint.org"
				matrixLink = fmt.Sprintf("https://matrix.to/#/#%s:%s?via=matrix.org", ircUrl.Fragment, humanHost)
			}
		default:
			if len(splitChan) > 1 {
				matrixLink = fmt.Sprintf("https://matrix.to/#/#%s:%s?via=matrix.org&via=%s", splitChan[0], splitChan[1], splitChan[1])
			}
		}
		ircLinkTitle = fmt.Sprintf("#%s@%s", splitChan[0], humanHost)
	}

	var basePoolInfo *cmdutils.PoolInfoResult

	for {
		t := getTypeFromAPI[cmdutils.PoolInfoResult]("pool_info")
		if t == nil {
			time.Sleep(1)
			continue
		}
		if t.SideChain.LastBlock != nil {
			basePoolInfo = t
			break
		}
		time.Sleep(1)
	}

	consensusData, _ := utils.MarshalJSON(basePoolInfo.SideChain.Consensus)
	consensus, err := sidechain.NewConsensusFromJSON(consensusData)
	if err != nil {
		utils.Panic(err)
	}

	utils.Logf("Consensus", "Consensus id = %s", consensus.Id)

	var lastPoolInfo atomic.Pointer[cmdutils.PoolInfoResult]

	ensureGetLastPoolInfo := func() *cmdutils.PoolInfoResult {
		poolInfo := lastPoolInfo.Load()
		if poolInfo == nil {
			poolInfo = getTypeFromAPI[cmdutils.PoolInfoResult]("pool_info", 5)
			lastPoolInfo.Store(poolInfo)
		}
		return poolInfo
	}

	baseContext := views.GlobalRequestContext{
		DonationAddress: types.DonationAddress,
		SiteTitle:       os.Getenv("SITE_TITLE"),
		//TODO change to args
		NetServiceAddress: os.Getenv("NET_SERVICE_ADDRESS"),
		TorServiceAddress: os.Getenv("TOR_SERVICE_ADDRESS"),
		Consensus:         consensus,
		Pool:              nil,
		IsRefresh:         nil,
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

		if refreshProvider, ok := page.(views.RefreshProviderPage); ok {
			ctx.IsRefresh = refreshProvider.IsRefresh
		}

		page.SetContext(&ctx)

		bufferedWriter := quicktemplate.AcquireWriter(w)
		defer quicktemplate.ReleaseWriter(bufferedWriter)
		views.StreamPageTemplate(bufferedWriter, page)
	}

	serveMux := mux.NewRouter()

	serveMux.HandleFunc("/assets/{asset:.*}", func(writer http.ResponseWriter, request *http.Request) {
		mimeType := mime.TypeByExtension(path.Ext(request.URL.Path))
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}

		assetPath := "assets/" + mux.Vars(request)["asset"]

		pref := httputils.SelectEncodingServerPreference(request.Header.Get("Accept-Encoding"))

		encodings := strings.Split(request.Header.Get("Accept-Encoding"), ",")
		for i := range encodings {
			e := strings.Split(encodings[i], ";")
			encodings[i] = strings.TrimSpace(e[0])
		}

		if entries, ok := compressedAssets[assetPath]; ok {
			var e compressedAsset

			switch pref {
			case httputils.ContentEncodingZstd:
				e = entries[compressionZstd]
			case httputils.ContentEncodingBrotli:
				e = entries[compressionBrotli]
			case httputils.ContentEncodingGzip:
				e = entries[compressionGzip]
			default:
				e = entries[compressionNone]
			}
			writer.Header().Set("ETag", e.ETag)

			if request.Header.Get("If-None-Match") == e.ETag {
				writer.WriteHeader(http.StatusNotModified)
				return
			}

			if e.ContentEncoding != "" {
				writer.Header().Set("Content-Encoding", string(e.ContentEncoding))
			}

			writer.Header().Set("Content-Type", mimeType)
			writer.Header().Set("Content-Length", strconv.FormatUint(uint64(len(e.Data)), 10))
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write(e.Data)
			return
		} else {
			writer.WriteHeader(http.StatusNotFound)
			return
		}
	})

	serveMux.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		params := request.URL.Query()
		refresh := 0
		if params.Has("refresh") {
			writer.Header().Set("refresh", "120")
			refresh = 100
		}

		poolInfo := getTypeFromAPI[cmdutils.PoolInfoResult]("pool_info", 5)
		lastPoolInfo.Store(poolInfo)

		secondsPerBlock := float64(poolInfo.MainChain.Difficulty.Lo) / float64(poolInfo.SideChain.LastBlock.Difficulty/consensus.TargetBlockTime)

		blocksToFetch := uint64(math.Ceil((((time.Hour*24).Seconds()/secondsPerBlock)*2)/100) * 100)

		blocks := getSliceFromAPI[*index.FoundBlock](fmt.Sprintf("found_blocks?limit=%d", blocksToFetch), 5)
		shares := getSideBlocksFromAPI("side_blocks?limit=50", 5)

		blocksFound := cmdutils.NewPositionChart(30*4, consensus.ChainWindowSize*4)

		tip := int64(poolInfo.SideChain.LastBlock.SideHeight)
		for _, b := range blocks {
			blocksFound.Add(int(tip-int64(b.SideHeight)), 1)
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
		poolInfo := *ensureGetLastPoolInfo()
		if len(poolInfo.SideChain.Effort.Last) > 5 {
			poolInfo.SideChain.Effort.Last = poolInfo.SideChain.Effort.Last[:5]
		}

		p := &views.ApiPage{
			PoolInfoExample: poolInfo,
		}
		renderPage(request, writer, p)
	})

	serveMux.HandleFunc("/calculate-share-time", func(writer http.ResponseWriter, request *http.Request) {
		poolInfo := getTypeFromAPI[cmdutils.PoolInfoResult]("pool_info", 5)
		lastPoolInfo.Store(poolInfo)
		hashRate := float64(0)
		magnitude := float64(1000)

		params := request.URL.Query()
		if params.Has("hashrate") {
			hashRate = toFloat64(strings.ReplaceAll(params.Get("hashrate"), ",", "."))
		}
		if params.Has("magnitude") {
			magnitude = toFloat64(params.Get("magnitude"))
		}

		currentHashRate := magnitude * hashRate

		var effortSteps = []float64{10, 25, 50, 75, 100, 150, 200, 300, 400, 500, 600, 700, 800, 900, 1000}
		const shareSteps = 15

		calculatePage := &views.CalculateShareTimePage{
			Hashrate:              hashRate,
			Magnitude:             magnitude,
			Efforts:               nil,
			EstimatedRewardPerDay: 0,
			EstimatedSharesPerDay: 0,
			EstimatedBlocksPerDay: 0,
		}

		if currentHashRate > 0 {
			var efforts []views.CalculateShareTimePageEffortEntry

			for _, effort := range effortSteps {
				e := views.CalculateShareTimePageEffortEntry{
					Effort:             effort,
					Probability:        utils.ProbabilityEffort(effort) * 100,
					Between:            (float64(poolInfo.SideChain.LastBlock.Difficulty) * (effort / 100)) / currentHashRate,
					BetweenSolo:        (float64(poolInfo.MainChain.Difficulty.Lo) * (effort / 100)) / currentHashRate,
					ShareProbabilities: make([]float64, shareSteps+1),
				}

				for i := uint64(0); i <= shareSteps; i++ {
					e.ShareProbabilities[i] = utils.ProbabilityNShares(i, effort)
				}

				efforts = append(efforts, e)
			}
			calculatePage.Efforts = efforts

			longWeight := types.DifficultyFrom64(uint64(currentHashRate)).Mul64(3600 * 24)
			calculatePage.EstimatedSharesPerDay = float64(longWeight.Mul64(1000).Div64(poolInfo.SideChain.LastBlock.Difficulty).Lo) / 1000
			calculatePage.EstimatedBlocksPerDay = float64(longWeight.Mul64(1000).Div(poolInfo.MainChain.NextDifficulty).Lo) / 1000
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
			checkInformation := getTypeFromAPI[types2.P2PoolConnectionCheckInformation[*sidechain.PoolBlock]]("consensus/connection_check/" + addressPort.String())
			var rawTip *sidechain.PoolBlock
			ourTip := getTypeFromAPI[index.SideBlock]("redirect/tip")
			var theirTip *index.SideBlock
			if checkInformation != nil {
				if checkInformation.Tip != nil {
					rawTip = checkInformation.Tip
					theirTip = getTypeFromAPI[index.SideBlock](fmt.Sprintf("block_by_id/%s", types.HashFromBytes(rawTip.CoinbaseExtra(sidechain.SideTemplateId))))
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
			miner = getTypeFromAPI[cmdutils.MinerInfoResult](fmt.Sprintf("miner_info/%s?noShares", params.Get("miner")))
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
				Sweeps:  getStreamFromAPI[*index.MainLikelySweepTransaction](fmt.Sprintf("sweeps/%d?limit=100", miner.Id)),
				Miner:   miner.Address,
			}, poolInfo)
		} else {
			renderPage(request, writer, &views.SweepsPage{
				Refresh: refresh,
				Sweeps:  getStreamFromAPI[*index.MainLikelySweepTransaction]("sweeps?limit=100"),
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
			miner = getTypeFromAPI[cmdutils.MinerInfoResult](fmt.Sprintf("miner_info/%s?noShares", params.Get("miner")))
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

		currentWindowSize := uint64(poolInfo.SideChain.Window.Blocks)
		windowSize := currentWindowSize
		if poolInfo.SideChain.LastBlock.SideHeight <= windowSize {
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

		tipHeight := poolInfo.SideChain.LastBlock.SideHeight
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

				uncleWeight, unclePenalty := consensus.ApplyUnclePenalty(types.DifficultyFrom64(share.Difficulty))

				if shares[uncleShareIndex].TemplateId == share.UncleOf {
					parent := shares[uncleShareIndex]
					createMiner(parent.Miner, parent)
					miners[parent.Miner].Weight = miners[parent.Miner].Weight.Add64(unclePenalty.Lo)
				}
				miners[miner].Weight = miners[miner].Weight.Add64(uncleWeight.Lo)

				totalWeight = totalWeight.Add64(share.Difficulty)
			} else {
				uncleShareIndex = i
				createMiner(share.Miner, share)
				miners[miner].Shares.Add(int(int64(tip.SideHeight)-int64(share.SideHeight)), 1)
				miners[miner].Weight = miners[miner].Weight.Add64(share.Difficulty)
				totalWeight = totalWeight.Add64(share.Difficulty)
			}
		}

		minerKeys := utils.Keys(miners)
		slices.SortFunc(minerKeys, func(a uint64, b uint64) int {
			return miners[a].Weight.Cmp(miners[b].Weight) * -1
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

		payouts := getStreamFromAPI[*index.Payout](fmt.Sprintf("block_by_id/%s/payouts", block.MainId))

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
		miner := getTypeFromAPI[cmdutils.MinerInfoResult](fmt.Sprintf("miner_info/%s?shareEstimates", address))
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

		currentWindowSize := uint64(poolInfo.SideChain.Window.Blocks)

		tipHeight := poolInfo.SideChain.LastBlock.SideHeight

		var dayShares, lastShares, lastOrphanedShares []*index.SideBlock

		var lastFound []*index.FoundBlock
		var payouts []*index.Payout
		var sweeps <-chan *index.MainLikelySweepTransaction

		var raw *sidechain.PoolBlock

		if miner.Id != 0 {
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				dayShares = getSideBlocksFromAPI(fmt.Sprintf("side_blocks_in_window/%d?from=%d&window=%d&noMiner&noMainStatus&noUncles", miner.Id, tipHeight, wsize))
			}()
			wg.Add(1)
			go func() {
				defer wg.Done()
				lastShares = getSideBlocksFromAPI(fmt.Sprintf("side_blocks?limit=200&miner=%d", miner.Id))

				if len(lastShares) > 0 {
					raw = getTypeFromAPI[sidechain.PoolBlock](fmt.Sprintf("block_by_id/%s/light", lastShares[0].MainId))
					if raw == nil || raw.ShareVersion() == sidechain.ShareVersion_None {
						raw = nil
					}
				}
			}()
			wg.Add(1)
			go func() {
				defer wg.Done()
				lastOrphanedShares = getSideBlocksFromAPI(fmt.Sprintf("side_blocks?limit=12&miner=%d&inclusion=%d", miner.Id, index.InclusionOrphan))
			}()
			wg.Add(1)
			go func() {
				defer wg.Done()
				lastFound = getSliceFromAPI[*index.FoundBlock](fmt.Sprintf("found_blocks?limit=12&miner=%d", miner.Id))
			}()
			sweeps = getStreamFromAPI[*index.MainLikelySweepTransaction](fmt.Sprintf("sweeps/%d?limit=12", miner.Id))
			wg.Add(1)
			go func() {
				defer wg.Done()
				//get a bit over the expected required
				payouts = getSliceFromAPI[*index.Payout](fmt.Sprintf("payouts/%d?from_timestamp=%d", miner.Id, uint64(time.Now().Unix())-(consensus.ChainWindowSize*consensus.TargetBlockTime*(totalWindows+1))))
			}()
			wg.Wait()
		} else {
			sweepC := make(chan *index.MainLikelySweepTransaction)
			sweeps = sweepC
			close(sweepC)
		}

		sharesInWindow := cmdutils.NewPositionChart(30, uint64(poolInfo.SideChain.Window.Blocks))
		unclesInWindow := cmdutils.NewPositionChart(30, uint64(poolInfo.SideChain.Window.Blocks))

		var windowShares []*index.SideBlock

		var windowWeight types.Difficulty

		for _, share := range dayShares {
			weight, _ := share.Weight(tipHeight, currentWindowSize, consensus.UnclePenalty)
			if weight > 0 {
				if share.IsUncle() {
					unclesInWindow.Add(int(int64(tipHeight)-int64(share.SideHeight)), 1)
				} else {
					sharesInWindow.Add(int(int64(tipHeight)-toInt64(share.SideHeight)), 1)
				}
				windowShares = append(windowShares, share)
				windowWeight = windowWeight.Add64(weight)
			}
		}

		dayHashrate, dayRatio, dayWeight, dayTotal := index.HashRatioSideBlocks(dayShares, poolInfo.SideChain.LastBlock, consensus.UnclePenalty)

		var latestShareForLast = poolInfo.SideChain.LastBlock
		if len(lastShares) > 0 && (poolInfo.SideChain.LastBlock.Timestamp-lastShares[0].Timestamp) > 3600*24*7 {
			//too long since last share
			latestShareForLast = lastShares[0]
		}
		lastHashrate, lastRatio, lastWeight, lastTotal := index.HashRatioSideBlocks(lastShares, latestShareForLast, consensus.UnclePenalty)

		minerPage := &views.MinerPage{
			Refresh: refresh,
			Positions: struct {
				BlocksInWindow *cmdutils.PositionChart
				UnclesInWindow *cmdutils.PositionChart
			}{
				BlocksInWindow: sharesInWindow,
				UnclesInWindow: unclesInWindow,
			},
			Window: views.MinerPageSectionData{
				Shares:         windowShares,
				Hashrate:       uint64((windowWeight.Float64() / poolInfo.SideChain.Window.Weight.Float64()) * float64(poolInfo.SideChain.LastBlock.Difficulty/consensus.TargetBlockTime)),
				Ratio:          windowWeight.Float64() / poolInfo.SideChain.Window.Weight.Float64(),
				ExpectedReward: windowWeight.Mul64(poolInfo.MainChain.BaseReward).Div(poolInfo.MainChain.NextDifficulty).Lo,
				Weight:         windowWeight,
				Total:          poolInfo.SideChain.Window.Weight,
			},
			Day: views.MinerPageSectionData{
				Shares:         dayShares,
				Hashrate:       dayHashrate,
				Ratio:          dayRatio,
				ExpectedReward: dayWeight.Mul64(poolInfo.MainChain.BaseReward).Div(poolInfo.MainChain.NextDifficulty).Lo,
				Weight:         dayWeight,
				Total:          dayTotal,
			},
			Last: views.MinerPageSectionData{
				Shares:         lastShares,
				Hashrate:       lastHashrate,
				Ratio:          lastRatio,
				ExpectedReward: lastWeight.Mul64(poolInfo.MainChain.BaseReward).Div(poolInfo.MainChain.NextDifficulty).Lo,
				Total:          lastTotal,
			},
			Miner:              miner,
			LastPoolBlock:      raw,
			LastOrphanedShares: lastOrphanedShares,
			LastFound:          lastFound,
			LastPayouts:        payouts,
			LastSweeps:         sweeps,
		}

		if len(dayShares) > len(lastShares) {
			minerPage.Last = minerPage.Day
		}

		calculatedHashrate := minerPage.Last.Hashrate

		if len(minerPage.Last.Shares) < 2 {
			calculatedHashrate = 0
		}

		hashRate := float64(0)
		magnitude := float64(1000)

		if calculatedHashrate >= 1000000000 {
			hashRate = float64(calculatedHashrate) / 1000000000
			magnitude = 1000000000
		} else if calculatedHashrate >= 1000000 {
			hashRate = float64(calculatedHashrate) / 1000000
			magnitude = 1000000
		} else if calculatedHashrate >= 1000 {
			hashRate = float64(calculatedHashrate) / 1000
			magnitude = 1000
		}

		if params.Has("magnitude") {
			magnitude = toFloat64(params.Get("magnitude"))
		}
		if params.Has("hashrate") {
			hashRate = toFloat64(strings.ReplaceAll(params.Get("hashrate"), ",", "."))

			if hashRate > 0 && magnitude > 0 {
				calculatedHashrate = uint64(hashRate * magnitude)
			}
			minerPage.HashrateSubmit = true
		}

		minerPage.HashrateLocal = hashRate
		minerPage.MagnitudeLocal = magnitude

		calculateEfforts := func(shares []*index.SideBlock) []float64 {
			if calculatedHashrate == 0 {
				return nil
			}
			efforts := make([]float64, len(shares))
			for i := len(shares) - 1; i >= 0; i-- {
				s := shares[i]
				if i == (len(shares) - 1) {
					efforts[i] = -1
					continue
				}
				previous := shares[i+1]

				timeDelta := uint64(max(int64(s.Timestamp)-int64(previous.Timestamp), 0))

				expectedCumDiff := types.DifficultyFrom64(calculatedHashrate).Mul64(timeDelta)

				efforts[i] = float64(expectedCumDiff.Mul64(100).Lo) / float64(s.Difficulty)
			}
			return efforts
		}

		minerPage.Day.Efforts = calculateEfforts(minerPage.Day.Shares)
		minerPage.Last.Efforts = calculateEfforts(minerPage.Last.Shares)

		renderPage(request, writer, minerPage, poolInfo)
	})

	serveMux.HandleFunc("/miner-options/{miner:[^ ]+}/signed_action", func(writer http.ResponseWriter, request *http.Request) {
		params := request.URL.Query()

		address := mux.Vars(request)["miner"]
		miner := getTypeFromAPI[cmdutils.MinerInfoResult](fmt.Sprintf("miner_info/%s?noShares", address))
		if miner == nil || miner.Address == nil {
			renderPage(request, writer, views.NewErrorPage(http.StatusNotFound, "Address Not Found", "You need to have mined at least one share in the past. Come back later :)"))
			return
		}

		var signedAction *cmdutils.SignedAction

		if err := json.Unmarshal([]byte(params.Get("message")), &signedAction); err != nil || signedAction == nil {
			renderPage(request, writer, views.NewErrorPage(http.StatusBadRequest, "Invalid Message", nil))
			return
		}

		values := make(url.Values)
		values.Set("signature", params.Get("signature"))
		values.Set("message", signedAction.String())

		var jsonErr struct {
			Error string `json:"error"`
		}
		statusCode, buf := getFromAPI("miner_signed_action/" + string(miner.Address.ToBase58()) + "?" + values.Encode())
		_ = json.Unmarshal(buf, &jsonErr)
		if statusCode != http.StatusOK {
			renderPage(request, writer, views.NewErrorPage(http.StatusBadRequest, "Could not verify message", jsonErr.Error))
			return
		}
	})

	serveMux.HandleFunc("/miner-options/{miner:[^ ]+}/{action:set_miner_alias|unset_miner_alias|add_webhook|remove_webhook}", func(writer http.ResponseWriter, request *http.Request) {
		params := request.URL.Query()

		address := mux.Vars(request)["miner"]
		miner := getTypeFromAPI[cmdutils.MinerInfoResult](fmt.Sprintf("miner_info/%s?noShares", address))
		if miner == nil || miner.Address == nil {
			renderPage(request, writer, views.NewErrorPage(http.StatusNotFound, "Address Not Found", "You need to have mined at least one share in the past. Come back later :)"))
			return
		}

		var signedAction *cmdutils.SignedAction

		action := mux.Vars(request)["action"]
		switch action {
		case "set_miner_alias":
			signedAction = cmdutils.SignedActionSetMinerAlias(baseContext.NetServiceAddress, params.Get("alias"))
		case "unset_miner_alias":
			signedAction = cmdutils.SignedActionUnsetMinerAlias(baseContext.NetServiceAddress)
		case "add_webhook":
			signedAction = cmdutils.SignedActionAddWebHook(baseContext.NetServiceAddress, params.Get("type"), params.Get("url"))
			for _, s := range []string{"side_blocks", "payouts", "found_blocks", "orphaned_blocks", "other"} {
				value := "false"
				if params.Get(s) == "on" {
					value = "true"
				}
				signedAction.Data = append(signedAction.Data, cmdutils.SignedActionEntry{
					Key:   "send_" + s,
					Value: value,
				})
			}
		case "remove_webhook":
			signedAction = cmdutils.SignedActionRemoveWebHook(baseContext.NetServiceAddress, params.Get("type"), params.Get("url_hash"))
		default:
			renderPage(request, writer, views.NewErrorPage(http.StatusNotFound, "Invalid Action", nil))
			return
		}

		renderPage(request, writer, &views.MinerOptionsPage{
			Miner:        miner,
			SignedAction: signedAction,
			WebHooks:     getSliceFromAPI[*index.MinerWebHook](fmt.Sprintf("miner_webhooks/%s", address)),
		})
	})

	serveMux.HandleFunc("/miner-options/{miner:[^ ]+}", func(writer http.ResponseWriter, request *http.Request) {
		//params := request.URL.Query()

		address := mux.Vars(request)["miner"]
		miner := getTypeFromAPI[cmdutils.MinerInfoResult](fmt.Sprintf("miner_info/%s?noShares", address))
		if miner == nil || miner.Address == nil {
			renderPage(request, writer, views.NewErrorPage(http.StatusNotFound, "Address Not Found", "You need to have mined at least one share in the past. Come back later :)"))
			return
		}

		renderPage(request, writer, &views.MinerOptionsPage{
			Miner:        miner,
			SignedAction: nil,
			WebHooks:     getSliceFromAPI[*index.MinerWebHook](fmt.Sprintf("miner_webhooks/%s", address)),
		})
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
		miner := getTypeFromAPI[cmdutils.MinerInfoResult](fmt.Sprintf("miner_info/%s?noShares", address))

		if miner == nil || miner.Address == nil {
			renderPage(request, writer, views.NewErrorPage(http.StatusNotFound, "Address Not Found", "You need to have mined at least one share in the past. Come back later :)"))
			return
		}

		payouts := getStreamFromAPI[*index.Payout](fmt.Sprintf("payouts/%d?search_limit=0", miner.Id))
		renderPage(request, writer, &views.PayoutsPage{
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
				utils.Panic(err)
			}
		}()
	}

	if err := server.ListenAndServe(); err != nil {
		utils.Panic(err)
	}
}
