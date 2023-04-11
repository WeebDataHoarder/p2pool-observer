package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/index"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	p2poolapi "git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/api"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"github.com/ake-persson/mapslice-json"
	"github.com/gorilla/mux"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
)

func encodeJson(r *http.Request, d any) ([]byte, error) {
	if strings.Index(strings.ToLower(r.Header.Get("user-agent")), "mozilla") != -1 {
		return json.MarshalIndent(d, "", "    ")
	} else {
		return json.Marshal(d)
	}
}

func main() {
	torHost := os.Getenv("TOR_SERVICE_ADDRESS")
	moneroHost := flag.String("host", "127.0.0.1", "IP address of your Monero node")
	moneroRpcPort := flag.Uint("rpc-port", 18081, "monerod RPC API port number")
	dbString := flag.String("db", "", "")
	p2poolApiHost := flag.String("api-host", "", "Host URL for p2pool go observer consensus")
	flag.Parse()

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

	getBlockWithUncles := func(id types.Hash) (block *sidechain.PoolBlock, uncles []*sidechain.PoolBlock) {
		block = p2api.ByTemplateId(id)
		if block == nil {
			return nil, nil
		}
		for _, uncleId := range block.Side.Uncles {
			if u := p2api.ByTemplateId(uncleId); u == nil {
				return nil, nil
			} else {
				uncles = append(uncles, u)
			}
		}
		return block, uncles
	}
	_ = getBlockWithUncles

	serveMux := mux.NewRouter()
	serveMux.HandleFunc("/api/pool_info", func(writer http.ResponseWriter, request *http.Request) {
		tip := p2api.Tip()

		blockCount := 0
		uncleCount := 0

		miners := make(map[uint64]uint64)

		window, windowUncles := p2api.WindowFromTemplateId(tip.SideTemplateId(p2api.Consensus()))

		if len(window) == 0 {
			//err
			log.Panic("empty window")
		}

		var pplnsWeight types.Difficulty

		for ps := range sidechain.IterateBlocksInPPLNSWindow(tip, p2api.Consensus(), indexDb.GetDifficultyByHeight, func(h types.Hash) *sidechain.PoolBlock {
			if b := window.Get(h); b == nil {
				if b = windowUncles.Get(h); b == nil {
					return p2api.ByTemplateId(h)
				} else {
					return b
				}
			} else {
				return b
			}
		}, func(b *sidechain.PoolBlock, weight types.Difficulty) {
			miners[indexDb.GetOrCreateMinerPackedAddress(*b.GetAddress()).Id()]++
			pplnsWeight = pplnsWeight.Add(weight)
		}, func(err error) {
			log.Panicf("error scanning PPLNS window: %s", err)
		}) {
			blockCount++
			uncleCount += len(ps.Uncles)
		}

		type totalKnownResult struct {
			blocksFound uint64
			minersKnown uint64
		}

		totalKnown := cacheResult(CacheTotalKnownBlocksAndMiners, time.Second*15, func() any {
			result := &totalKnownResult{}
			if err := indexDb.Query("SELECT (SELECT COUNT(*) FROM main_blocks WHERE side_template_id IS NOT NULL) as found, COUNT(*) as miners FROM (SELECT miner FROM side_blocks GROUP BY miner) all_known_miners;", func(row index.RowScanInterface) error {
				return row.Scan(&result.blocksFound, &result.minersKnown)
			}); err != nil {
				return nil
			}

			return result
		}).(*totalKnownResult)

		lastBlocksFound := indexDb.GetBlocksFound("", 150)

		globalDiff := p2api.MainDifficultyByHeight(tip.Main.Coinbase.GenHeight)

		var lastDiff types.Difficulty
		if len(lastBlocksFound) > 0 {
			lastDiff = lastBlocksFound[0].CumulativeDifficulty
		}

		currentEffort := float64(tip.Side.CumulativeDifficulty.Sub(lastDiff).Mul64(100000).Div(globalDiff).Lo) / 1000

		if currentEffort <= 0 || lastDiff.Cmp64(0) == 0 {
			currentEffort = 0
		}

		var blockEfforts mapslice.MapSlice
		for i, b := range lastBlocksFound {
			if i < (len(lastBlocksFound)-1) && b.CumulativeDifficulty.Cmp64(0) > 0 && lastBlocksFound[i+1].CumulativeDifficulty.Cmp64(0) > 0 {
				blockEfforts = append(blockEfforts, mapslice.MapItem{
					Key:   b.MainBlock.Id.String(),
					Value: float64(b.CumulativeDifficulty.Sub(lastBlocksFound[i+1].CumulativeDifficulty).Mul64(100000).Div(globalDiff).Lo) / 1000,
				})
			}
		}

		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		writer.WriteHeader(http.StatusOK)
		if buf, err := encodeJson(request, poolInfoResult{
			SideChain: poolInfoResultSideChain{
				Consensus:            p2api.Consensus(),
				Id:                   tip.SideTemplateId(p2api.Consensus()),
				Height:               tip.Side.Height,
				Difficulty:           tip.Side.Difficulty,
				CumulativeDifficulty: tip.Side.CumulativeDifficulty,
				Timestamp:            tip.Main.Timestamp,
				Effort: poolInfoResultSideChainEffort{
					Current: currentEffort,
					Average: func() (result float64) {
						for _, e := range blockEfforts {
							result += e.Value.(float64)
						}
						return
					}() / float64(len(blockEfforts)),
					Last: blockEfforts,
				},
				Window: poolInfoResultSideChainWindow{
					Miners: len(miners),
					Blocks: blockCount,
					Uncles: uncleCount,
					Weight: pplnsWeight,
				},
				WindowSize:    blockCount,
				MaxWindowSize: int(p2api.Consensus().ChainWindowSize),
				BlockTime:     int(p2api.Consensus().TargetBlockTime),
				UnclePenalty:  int(p2api.Consensus().UnclePenalty),
				Found:         totalKnown.blocksFound,
				Miners:        totalKnown.minersKnown,
			},
			MainChain: poolInfoResultMainChain{
				Id:         tip.Main.PreviousId,
				Height:     tip.Main.Coinbase.GenHeight - 1,
				Difficulty: globalDiff,
				BlockTime:  monero.BlockTime,
			},
			Versions: struct {
				P2Pool versionInfo `json:"p2pool"`
				Monero versionInfo `json:"monero"`
			}{P2Pool: getP2PoolVersion(), Monero: getMoneroVersion()},
		}); err != nil {
			log.Panic(err)
		} else {
			_, _ = writer.Write(buf)
		}

	})

	serveMux.HandleFunc("/api/miner_info/{miner:[^ ]+}", func(writer http.ResponseWriter, request *http.Request) {
		minerId := mux.Vars(request)["miner"]
		var miner *index.Miner
		if len(minerId) > 10 && minerId[0] == '4' {
			miner = indexDb.GetMinerByStringAddress(minerId)
		}

		if miner == nil {
			miner = indexDb.GetMinerByAlias(minerId)
		}

		if miner == nil {
			if i, err := strconv.Atoi(minerId); err == nil {
				miner = indexDb.GetMiner(uint64(i))
			}
		}

		if miner == nil {
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusNotFound)
			buf, _ := json.Marshal(struct {
				Error string `json:"error"`
			}{
				Error: "not_found",
			})
			_, _ = writer.Write(buf)
			return
		}

		var foundBlocksData [index.InclusionAlternateInVerifiedChain + 1]minerInfoBlockData
		_ = indexDb.Query("SELECT COUNT(*) as count, coalesce(MAX(side_height), 0) as last_height, inclusion FROM side_blocks WHERE side_blocks.miner = $1 GROUP BY side_blocks.inclusion ORDER BY inclusion ASC;", func(row index.RowScanInterface) error {
			var d minerInfoBlockData
			var inclusion index.BlockInclusion
			if err := row.Scan(&d.ShareCount, &d.LastShareHeight, &inclusion); err != nil {
				return err
			}
			if inclusion < 3 {
				foundBlocksData[inclusion] = d
			}
			return nil
		}, miner.Id())

		_ = indexDb.Query("SELECT COUNT(*) as count, coalesce(MAX(side_height), 0) as last_height, inclusion FROM side_blocks WHERE side_blocks.miner = $1 AND side_blocks.uncle_of IS NOT NULL GROUP BY side_blocks.inclusion ORDER BY inclusion ASC;", func(row index.RowScanInterface) error {
			var d minerInfoBlockData
			var inclusion index.BlockInclusion
			if err := row.Scan(&d.ShareCount, &d.LastShareHeight, &inclusion); err != nil {
				return err
			}
			if inclusion <= index.InclusionAlternateInVerifiedChain {
				foundBlocksData[inclusion].ShareCount -= d.ShareCount
				foundBlocksData[inclusion].UncleCount = d.ShareCount
			}
			return nil
		}, miner.Id())

		var lastShareHeight uint64
		var lastShareTimestamp uint64
		if foundBlocksData[index.InclusionInVerifiedChain].ShareCount > 0 && foundBlocksData[index.InclusionInVerifiedChain].LastShareHeight > lastShareHeight {
			lastShareHeight = foundBlocksData[index.InclusionInVerifiedChain].LastShareHeight
		}

		if lastShareHeight > 0 {
			lastShareTimestamp = indexDb.GetTipSideBlockByHeight(lastShareHeight).Timestamp
		}

		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		writer.WriteHeader(http.StatusOK)
		buf, _ := encodeJson(request, minerInfoResult{
			Id:                 miner.Id(),
			Address:            miner.Address(),
			Alias:              miner.Alias(),
			Shares:             foundBlocksData,
			LastShareHeight:    lastShareHeight,
			LastShareTimestamp: lastShareTimestamp,
		})
		_, _ = writer.Write(buf)
	})

	serveMux.HandleFunc("/api/miner_alias/{miner:4[123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz]+}", func(writer http.ResponseWriter, request *http.Request) {
		minerId := mux.Vars(request)["miner"]
		miner := indexDb.GetMinerByStringAddress(minerId)

		if miner == nil {
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusNotFound)
			buf, _ := json.Marshal(struct {
				Error string `json:"error"`
			}{
				Error: "not_found",
			})
			_, _ = writer.Write(buf)
			return
		}

		params := request.URL.Query()

		sig := strings.TrimSpace(params.Get("signature"))

		message := strings.TrimSpace(params.Get("message"))

		if len(message) > 20 || len(message) < 3 || !func() bool {
			for _, c := range message {
				if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') && c != '_' && c != '-' && c != '.' {
					return false
				}
			}

			return true
		}() || !unicode.IsLetter(rune(message[0])) {
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusBadRequest)
			buf, _ := json.Marshal(struct {
				Error string `json:"error"`
			}{
				Error: "invalid_message",
			})
			_, _ = writer.Write(buf)
			return
		}

		result := address.VerifyMessage(miner.Address(), []byte(message), sig)
		if result == address.ResultSuccessSpend {
			if message == "REMOVE_MINER_ALIAS" {
				message = ""
			}
			if indexDb.SetMinerAlias(miner.Id(), message) != nil {
				writer.Header().Set("Content-Type", "application/json; charset=utf-8")
				writer.WriteHeader(http.StatusBadRequest)
				buf, _ := json.Marshal(struct {
					Error string `json:"error"`
				}{
					Error: "duplicate_message",
				})
				_, _ = writer.Write(buf)
				return
			} else {
				writer.Header().Set("Content-Type", "application/json; charset=utf-8")
				writer.WriteHeader(http.StatusOK)
				return
			}
		} else if result == address.ResultSuccessView {
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusBadRequest)
			buf, _ := json.Marshal(struct {
				Error string `json:"error"`
			}{
				Error: "view_signature",
			})
			_, _ = writer.Write(buf)
			return
		} else {
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusBadRequest)
			buf, _ := json.Marshal(struct {
				Error string `json:"error"`
			}{
				Error: "invalid_signature",
			})
			_, _ = writer.Write(buf)
			return
		}
	})

	serveMux.HandleFunc("/api/shares_in_window/{miner:[0-9]+|4[123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz]+$}", func(writer http.ResponseWriter, request *http.Request) {
		minerId := mux.Vars(request)["miner"]
		var miner *index.Miner
		if len(minerId) > 10 && minerId[0] == '4' {
			miner = indexDb.GetMinerByStringAddress(minerId)
		}

		if miner == nil {
			if i, err := strconv.Atoi(minerId); err == nil {
				miner = indexDb.GetMiner(uint64(i))
			}
		}

		if miner == nil {
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusNotFound)
			buf, _ := json.Marshal(struct {
				Error string `json:"error"`
			}{
				Error: "not_found",
			})
			_, _ = writer.Write(buf)
			return
		}

		params := request.URL.Query()

		window := p2api.Consensus().ChainWindowSize
		if params.Has("window") {
			if i, err := strconv.Atoi(params.Get("window")); err == nil {
				if i <= int(p2api.Consensus().ChainWindowSize*4) {
					window = uint64(i)
				}
			}
		}

		var from uint64
		if params.Has("from") {
			if i, err := strconv.Atoi(params.Get("from")); err == nil {
				if i >= 0 {
					from = uint64(i)
				}
			}
		}

		result := make([]*sharesInWindowResult, 0)

		for sideBlock := range indexDb.GetSideBlocksByMinerIdInWindow(miner.Id(), from, window) {
			s := &sharesInWindowResult{
				Id:        sideBlock.TemplateId,
				Height:    sideBlock.SideHeight,
				Timestamp: sideBlock.Timestamp,
				Weight:    sideBlock.Difficulty,
			}

			if sideBlock.IsUncle() {
				s.Parent = &sharesInWindowResultParent{
					Id:     sideBlock.UncleOf,
					Height: sideBlock.EffectiveHeight,
				}
				s.Weight = types.DifficultyFrom64(s.Weight).Mul64(100 - p2api.Consensus().UnclePenalty).Div64(100).Lo
			} else {
				for u := range indexDb.GetSideBlocksByUncleId(sideBlock.TemplateId) {
					uncleWeight := types.DifficultyFrom64(u.Difficulty).Mul64(p2api.Consensus().UnclePenalty).Div64(100)
					s.Uncles = append(s.Uncles, sharesInWindowResultUncle{
						Id:     u.TemplateId,
						Height: u.SideHeight,
						Weight: uncleWeight.Lo,
					})
					s.Weight += uncleWeight.Lo
				}
			}

			result = append(result, s)
		}

		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		writer.WriteHeader(http.StatusOK)
		buf, _ := encodeJson(request, result)
		_, _ = writer.Write(buf)
	})

	serveMux.HandleFunc("/api/payouts/{miner:[0-9]+|4[123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz]+$}", func(writer http.ResponseWriter, request *http.Request) {
		minerId := mux.Vars(request)["miner"]
		var miner *index.Miner
		if len(minerId) > 10 && minerId[0] == '4' {
			miner = indexDb.GetMinerByStringAddress(minerId)
		}

		if miner == nil {
			if i, err := strconv.Atoi(minerId); err == nil {
				miner = indexDb.GetMiner(uint64(i))
			}
		}

		if miner == nil {
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusNotFound)
			buf, _ := json.Marshal(struct {
				Error string `json:"error"`
			}{
				Error: "not_found",
			})
			_, _ = writer.Write(buf)
			return
		}

		params := request.URL.Query()

		var limit uint64 = 10

		if params.Has("search_limit") {
			if i, err := strconv.Atoi(params.Get("search_limit")); err == nil {
				limit = uint64(i)
			}
		}

		payouts := make([]*index.Payout, 0)

		for payout := range indexDb.GetPayoutsByMinerId(miner.Id(), limit) {
			payouts = append(payouts, payout)
		}

		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		writer.WriteHeader(http.StatusOK)
		buf, _ := encodeJson(request, payouts)
		_, _ = writer.Write(buf)

	})

	serveMux.HandleFunc("/api/redirect/block/{main_height:[0-9]+|.?[0-9A-Za-z]+$}", func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("host") == torHost {
			http.Redirect(writer, request, fmt.Sprintf("http://yucmgsbw7nknw7oi3bkuwudvc657g2xcqahhbjyewazusyytapqo4xid.onion/explorer/block/%d", utils.DecodeBinaryNumber(mux.Vars(request)["main_height"])), http.StatusFound)
		} else {
			http.Redirect(writer, request, fmt.Sprintf("https://p2pool.io/explorer/block/%d", utils.DecodeBinaryNumber(mux.Vars(request)["main_height"])), http.StatusFound)
		}
	})
	serveMux.HandleFunc("/api/redirect/transaction/{tx_id:.?[0-9A-Za-z]+}", func(writer http.ResponseWriter, request *http.Request) {
		txId := utils.DecodeHexBinaryNumber(mux.Vars(request)["tx_id"])
		if len(txId) != types.HashSize*2 {
			writer.WriteHeader(http.StatusNotFound)
			return
		}

		if request.Header.Get("host") == torHost {
			http.Redirect(writer, request, fmt.Sprintf("http://yucmgsbw7nknw7oi3bkuwudvc657g2xcqahhbjyewazusyytapqo4xid.onion/explorer/tx/%s", txId), http.StatusFound)
		} else {
			http.Redirect(writer, request, fmt.Sprintf("https://p2pool.io/explorer/tx/%s", txId), http.StatusFound)
		}
	})
	serveMux.HandleFunc("/api/redirect/block/{coinbase:[0-9]+|.?[0-9A-Za-z]+$}", func(writer http.ResponseWriter, request *http.Request) {
		foundTargets := indexDb.GetBlocksFound("WHERE side_height = $1", 1, utils.DecodeBinaryNumber(mux.Vars(request)["coinbase"]))
		if len(foundTargets) == 0 {
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusNotFound)
			buf, _ := json.Marshal(struct {
				Error string `json:"error"`
			}{
				Error: "not_found",
			})
			_, _ = writer.Write(buf)
			return
		}

		if request.Header.Get("host") == torHost {
			http.Redirect(writer, request, fmt.Sprintf("http://yucmgsbw7nknw7oi3bkuwudvc657g2xcqahhbjyewazusyytapqo4xid.onion/explorer/tx/%s", foundTargets[0].MainBlock.Id.String()), http.StatusFound)
		} else {
			http.Redirect(writer, request, fmt.Sprintf("https://p2pool.io/explorer/tx/%s", foundTargets[0].MainBlock.Id.String()), http.StatusFound)
		}
	})
	serveMux.HandleFunc("/api/redirect/share/{height:[0-9]+|.?[0-9A-Za-z]+$}", func(writer http.ResponseWriter, request *http.Request) {
		c := utils.DecodeBinaryNumber(mux.Vars(request)["height"])

		blockHeight := c >> 16
		blockIdStart := c & 0xFFFF

		blockStart := []byte{byte((blockIdStart >> 8) & 0xFF), byte(blockIdStart & 0xFF)}
		shares := index.ChanToSlice(indexDb.GetSideBlocksByQuery("WHERE side_height = $1;", 1, blockHeight))
		var b *index.SideBlock
		for _, s := range shares {
			if bytes.Compare(s.TemplateId[:2], blockStart) == 0 {
				b = s
			}
		}

		if b == nil {
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusNotFound)
			buf, _ := json.Marshal(struct {
				Error string `json:"error"`
			}{
				Error: "not_found",
			})
			_, _ = writer.Write(buf)
			return
		}

		http.Redirect(writer, request, fmt.Sprintf("/share/%s", b.TemplateId.String()), http.StatusFound)
	})

	serveMux.HandleFunc("/api/redirect/prove/{height_index:[0-9]+|.[0-9A-Za-z]+}", func(writer http.ResponseWriter, request *http.Request) {
		i := utils.DecodeBinaryNumber(mux.Vars(request)["height_index"])
		n := uint64(math.Ceil(math.Log2(float64(p2api.Consensus().ChainWindowSize * 4))))

		height := i >> n
		outputIndex := i & ((1 << n) - 1)

		b := indexDb.GetBlocksFound("WHERE side_height = $1", 1, height)
		var tx *index.MainCoinbaseOutput
		if len(b) != 0 {
			tx = indexDb.GetMainCoinbaseOutputByIndex(b[0].MainBlock.CoinbaseId, outputIndex)
		}

		if len(b) == 0 || tx == nil {
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusNotFound)
			buf, _ := json.Marshal(struct {
				Error string `json:"error"`
			}{
				Error: "not_found",
			})
			_, _ = writer.Write(buf)
			return
		}

		http.Redirect(writer, request, fmt.Sprintf("/proof/%s/%d", b[0].MainBlock.Id.String(), tx.Index), http.StatusFound)
	})

	serveMux.HandleFunc("/api/redirect/prove/{height:[0-9]+|.[0-9A-Za-z]+}/{miner:[0-9]+|.?[0-9A-Za-z]+}", func(writer http.ResponseWriter, request *http.Request) {
		b := indexDb.GetBlocksFound("WHERE side_height = $1", 1, utils.DecodeBinaryNumber(mux.Vars(request)["height"]))
		miner := indexDb.GetMiner(utils.DecodeBinaryNumber(mux.Vars(request)["miner"]))
		if len(b) == 0 || miner == nil {
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusNotFound)
			buf, _ := json.Marshal(struct {
				Error string `json:"error"`
			}{
				Error: "not_found",
			})
			_, _ = writer.Write(buf)
			return
		}

		tx := indexDb.GetMainCoinbaseOutputByMinerId(b[0].MainBlock.Id, miner.Id())

		if tx == nil {
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusNotFound)
			buf, _ := json.Marshal(struct {
				Error string `json:"error"`
			}{
				Error: "not_found",
			})
			_, _ = writer.Write(buf)
			return
		}

		http.Redirect(writer, request, fmt.Sprintf("/proof/%s/%d", b[0].MainBlock.Id.String(), tx.Index), http.StatusFound)
	})

	serveMux.HandleFunc("/api/redirect/miner/{miner:[0-9]+|.?[0-9A-Za-z]+}", func(writer http.ResponseWriter, request *http.Request) {
		miner := indexDb.GetMiner(utils.DecodeBinaryNumber(mux.Vars(request)["miner"]))
		if miner == nil {
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusNotFound)
			buf, _ := json.Marshal(struct {
				Error string `json:"error"`
			}{
				Error: "not_found",
			})
			_, _ = writer.Write(buf)
			return
		}

		http.Redirect(writer, request, fmt.Sprintf("/miner/%s", miner.Address().ToBase58()), http.StatusFound)
	})

	//other redirects
	serveMux.HandleFunc("/api/redirect/last_found{kind:|/raw|/info}", func(writer http.ResponseWriter, request *http.Request) {
		var lastFoundHash types.Hash
		for _, b := range indexDb.GetBlocksFound("", 1) {
			lastFoundHash = b.MainBlock.SideTemplateId
		}
		http.Redirect(writer, request, fmt.Sprintf("/api/block_by_id/%s%s%s", lastFoundHash.String(), mux.Vars(request)["kind"], request.URL.RequestURI()), http.StatusFound)
	})
	serveMux.HandleFunc("/api/redirect/tip{kind:|/raw|/info}", func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, fmt.Sprintf("/api/block_by_id/%s%s%s", indexDb.GetSideBlockTip().TemplateId.String(), mux.Vars(request)["kind"], request.URL.RequestURI()), http.StatusFound)
	})

	serveMux.HandleFunc("/api/found_blocks", func(writer http.ResponseWriter, request *http.Request) {

		params := request.URL.Query()

		var limit uint64 = 50
		if params.Has("limit") {
			if i, err := strconv.Atoi(params.Get("limit")); err == nil {
				limit = uint64(i)
			}
		}

		var minerId uint64

		if params.Has("miner") {
			id := params.Get("miner")
			var miner *index.Miner

			if len(id) > 10 && id[0] == '4' {
				miner = indexDb.GetMinerByStringAddress(id)
			}

			if miner == nil {
				if i, err := strconv.Atoi(id); err == nil {
					miner = indexDb.GetMiner(uint64(i))
				}
			}

			if miner == nil {
				writer.Header().Set("Content-Type", "application/json; charset=utf-8")
				writer.WriteHeader(http.StatusNotFound)
				buf, _ := json.Marshal(struct {
					Error string `json:"error"`
				}{
					Error: "not_found",
				})
				_, _ = writer.Write(buf)
				return
			}

			minerId = miner.Id()
		}

		if limit > 200 {
			limit = 200
		}

		if limit == 0 {
			limit = 50
		}

		result := make([]*JSONBlock, 0, limit)

		if minerId != 0 {
			for _, foundBlock := range indexDb.GetBlocksFound("WHERE miner = $1", limit, minerId) {
				dbBlock := indexDb.GetSideBlockByMainId(foundBlock.MainBlock.Id)
				if dbBlock == nil {
					continue
				}
				result = append(result, MapJSONBlock(p2api, indexDb, dbBlock, foundBlock, false, params.Has("coinbase")))
			}
		} else {
			for _, foundBlock := range indexDb.GetBlocksFound("", limit) {
				dbBlock := indexDb.GetSideBlockByMainId(foundBlock.MainBlock.Id)
				if dbBlock == nil {
					continue
				}
				result = append(result, MapJSONBlock(p2api, indexDb, dbBlock, foundBlock, false, params.Has("coinbase")))
			}
		}

		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		writer.WriteHeader(http.StatusOK)
		buf, _ := encodeJson(request, result)
		_, _ = writer.Write(buf)
	})

	serveMux.HandleFunc("/api/shares", func(writer http.ResponseWriter, request *http.Request) {

		params := request.URL.Query()

		var limit uint64 = 50
		if params.Has("limit") {
			if i, err := strconv.Atoi(params.Get("limit")); err == nil {
				limit = uint64(i)
			}
		}

		if limit > p2api.Consensus().ChainWindowSize*4*7 {
			limit = p2api.Consensus().ChainWindowSize * 4 * 7
		}
		if limit == 0 {
			limit = 50
		}

		onlyBlocks := params.Has("onlyBlocks")

		var minerId uint64

		if params.Has("miner") {
			id := params.Get("miner")
			var miner *index.Miner

			if len(id) > 10 && id[0] == '4' {
				miner = indexDb.GetMinerByStringAddress(id)
			}

			if miner == nil {
				if i, err := strconv.Atoi(id); err == nil {
					miner = indexDb.GetMiner(uint64(i))
				}
			}

			if miner == nil {
				writer.Header().Set("Content-Type", "application/json; charset=utf-8")
				writer.WriteHeader(http.StatusNotFound)
				buf, _ := json.Marshal(struct {
					Error string `json:"error"`
				}{
					Error: "not_found",
				})
				_, _ = writer.Write(buf)
				return
			}

			minerId = miner.Id()
		}

		result := make([]*JSONBlock, 0, limit)

		for b := range indexDb.GetShares(limit, minerId, onlyBlocks) {
			result = append(result, MapJSONBlock(p2api, indexDb, b, nil, true, params.Has("coinbase")))
		}

		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		writer.WriteHeader(http.StatusOK)
		buf, _ := encodeJson(request, result)
		_, _ = writer.Write(buf)
	})

	serveMux.HandleFunc("/api/block_by_{by:id|height}/{block:[0-9a-f]+|[0-9]+}{kind:|/raw|/info|/payouts}", func(writer http.ResponseWriter, request *http.Request) {

		params := request.URL.Query()

		var block *index.SideBlock
		if mux.Vars(request)["by"] == "id" {
			if id, err := types.HashFromString(mux.Vars(request)["block"]); err == nil {
				if b := indexDb.GetTipSideBlockByTemplateId(id); b != nil {
					block = b
				} else if b = indexDb.GetSideBlockByMainId(id); b != nil {
					block = b
				}
			}
		} else if mux.Vars(request)["by"] == "height" {
			if height, err := strconv.ParseUint(mux.Vars(request)["block"], 10, 0); err == nil {
				if b := indexDb.GetTipSideBlockByHeight(height); b != nil {
					block = b
				}
			}
		}

		if block == nil {
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusNotFound)
			buf, _ := json.Marshal(struct {
				Error string `json:"error"`
			}{
				Error: "not_found",
			})
			_, _ = writer.Write(buf)
			return
		}

		isOrphan := block.Inclusion == index.InclusionOrphan
		isInvalid := false

		switch mux.Vars(request)["kind"] {
		case "/raw":
			raw := p2api.ByMainId(block.MainId)

			if raw == nil {
				writer.Header().Set("Content-Type", "application/json; charset=utf-8")
				writer.WriteHeader(http.StatusNotFound)
				buf, _ := json.Marshal(struct {
					Error string `json:"error"`
				}{
					Error: "not_found",
				})
				_, _ = writer.Write(buf)
				return
			}

			writer.Header().Set("Content-Type", "text/plain")
			writer.WriteHeader(http.StatusOK)
			buf, _ := raw.MarshalBinary()
			_, _ = writer.Write(buf)
		case "/payouts":
			result := make([]*index.Payout, 0)
			if !isOrphan && !isInvalid {
				for payout := range indexDb.GetPayoutsBySideBlock(block) {
					result = append(result, payout)
				}
			}
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusOK)
			buf, _ := encodeJson(request, result)
			_, _ = writer.Write(buf)
		default:
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusOK)
			buf, _ := encodeJson(request, MapJSONBlock(p2api, indexDb, block, nil, true, params.Has("coinbase")))
			_, _ = writer.Write(buf)
		}
	})

	type difficultyStatResult struct {
		Height                    uint64 `json:"height"`
		Timestamp                 uint64 `json:"timestamp"`
		Difficulty                uint64 `json:"difficulty"`
		CumulativeDifficultyTop64 uint64 `json:"cumulative_difficulty_top64"`
		CumulativeDifficulty      uint64 `json:"cumulative_difficulty"`
	}

	type minerSeenResult struct {
		Height    uint64 `json:"height"`
		Timestamp uint64 `json:"timestamp"`
		Miners    uint64 `json:"miners"`
	}

	serveMux.HandleFunc("/api/stats/{kind:difficulty|miner_seen|miner_seen_window$}", func(writer http.ResponseWriter, request *http.Request) {
		switch mux.Vars(request)["kind"] {
		case "difficulty":
			result := make([]*difficultyStatResult, 0)

			_ = indexDb.Query("SELECT side_height,timestamp,difficulty,cumulative_difficulty FROM side_blocks WHERE side_height % $1 = 0 ORDER BY side_height DESC;", func(row index.RowScanInterface) error {
				r := &difficultyStatResult{}

				var cumDiff types.Difficulty
				if err := row.Scan(&r.Height, &r.Timestamp, &r.Difficulty, &cumDiff); err != nil {
					return err
				}
				r.CumulativeDifficulty = cumDiff.Lo
				r.CumulativeDifficultyTop64 = cumDiff.Hi

				result = append(result, r)
				return nil
			}, 3600/p2api.Consensus().TargetBlockTime)

			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusOK)
			buf, _ := encodeJson(request, result)
			_, _ = writer.Write(buf)

		case "miner_seen":
			result := make([]*minerSeenResult, 0)

			_ = indexDb.Query("SELECT side_height,timestamp,(SELECT COUNT(DISTINCT(b.miner)) FROM side_blocks b WHERE b.side_height <= side_blocks.side_height AND b.side_height > (side_blocks.side_height - $2)) as count FROM side_blocks WHERE side_height % $1 = 0 ORDER BY side_height DESC;", func(row index.RowScanInterface) error {
				r := &minerSeenResult{}

				if err := row.Scan(&r.Height, &r.Timestamp, &r.Miners); err != nil {
					return err
				}

				result = append(result, r)
				return nil
			}, (3600*24)/p2api.Consensus().TargetBlockTime, (3600*24*7)/p2api.Consensus().TargetBlockTime)

			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusOK)
			buf, _ := encodeJson(request, result)
			_, _ = writer.Write(buf)

		case "miner_seen_window":
			result := make([]*minerSeenResult, 0)

			_ = indexDb.Query("SELECT side_height,timestamp,(SELECT COUNT(DISTINCT(b.miner)) FROM side_blocks b WHERE b.side_height <= side_blocks.side_height AND b.side_height > (side_blocks.side_height - $2)) as count FROM side_blocks WHERE side_height % $1 = 0 ORDER BY side_height DESC;", func(row index.RowScanInterface) error {
				r := &minerSeenResult{}

				if err := row.Scan(&r.Height, &r.Timestamp, &r.Miners); err != nil {
					return err
				}

				result = append(result, r)
				return nil
			}, p2api.Consensus().ChainWindowSize, p2api.Consensus().ChainWindowSize)

			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusOK)
			buf, _ := encodeJson(request, result)
			_, _ = writer.Write(buf)
		}
	})

	server := &http.Server{
		Addr:        "0.0.0.0:8080",
		ReadTimeout: time.Second * 2,
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.Method != "GET" && request.Method != "HEAD" {
				writer.WriteHeader(http.StatusForbidden)
				return
			}

			serveMux.ServeHTTP(writer, request)
		}),
	}

	if err := server.ListenAndServe(); err != nil {
		log.Panic(err)
	}
}
