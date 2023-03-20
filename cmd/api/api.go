package main

import (
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/database"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/randomx"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool"
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
	client.SetDefaultClientSettings(os.Getenv("MONEROD_RPC_URL"))
	dbString := flag.String("db", "", "")
	p2poolApiHost := flag.String("api-host", "", "Host URL for p2pool go observer consensus")
	flag.Parse()
	db, err := database.NewDatabase(*dbString)
	if err != nil {
		log.Panic(err)
	}
	defer db.Close()

	p2api := p2poolapi.NewP2PoolApi(*p2poolApiHost)
	api, err := p2poolapi.New(db, p2api)
	if err != nil {
		log.Panic(err)
	}

	for status := p2api.Status(); !p2api.Status().Synchronized; status = p2api.Status() {
		log.Printf("[API] Not synchronized (height %d, id %s), waiting five seconds", status.Height, status.Id)
		time.Sleep(time.Second * 5)
	}

	log.Printf("[CHAIN] Consensus id = %s\n", p2api.Consensus().Id())

	getSeedByHeight := func(height uint64) (hash types.Hash) {
		seedHeight := randomx.SeedHeight(height)
		if h := p2api.MainHeaderByHeight(seedHeight); h == nil {
			return types.ZeroHash
		} else {
			return h.Id
		}
	}

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
		tip := api.GetDatabase().GetChainTip()

		blockCount := 0
		uncleCount := 0

		var windowDifficulty types.Difficulty

		miners := make(map[uint64]uint64)

		for b := range api.GetDatabase().GetBlocksInWindow(&tip.Height, 0) {
			blockCount++
			if _, ok := miners[b.MinerId]; !ok {
				miners[b.MinerId] = 0
			}
			miners[b.MinerId]++

			windowDifficulty = windowDifficulty.Add(b.Difficulty)
			for u := range api.GetDatabase().GetUnclesByParentId(b.Id) {
				//TODO: check this check is correct :)
				if (tip.Height - u.Block.Height) > p2api.Consensus().ChainWindowSize {
					continue
				}

				uncleCount++
				if _, ok := miners[u.Block.MinerId]; !ok {
					miners[u.Block.MinerId] = 0
				}
				miners[u.Block.MinerId]++

				windowDifficulty = windowDifficulty.Add(u.Block.Difficulty)
			}
		}

		type totalKnownResult struct {
			blocksFound uint64
			minersKnown uint64
		}

		totalKnown := cacheResult(CacheTotalKnownBlocksAndMiners, time.Second*15, func() any {
			result := &totalKnownResult{}
			if err := api.GetDatabase().Query("SELECT (SELECT COUNT(*) FROM blocks WHERE main_found IS TRUE ) + (SELECT COUNT(*) FROM uncles WHERE main_found IS TRUE) as found, COUNT(*) as miners FROM (SELECT miner FROM blocks GROUP BY miner UNION DISTINCT SELECT miner FROM uncles GROUP BY miner) all_known_miners;", func(row database.RowScanInterface) error {
				return row.Scan(&result.blocksFound, &result.minersKnown)
			}); err != nil {
				return nil
			}

			return result
		}).(*totalKnownResult)

		/*
			poolBlocks, _ := api.GetPoolBlocks()

			poolStats, _ := api.GetPoolStats()

			globalDiff := tip.Template.Difficulty

			currentEffort := float64(types.DifficultyFrom64(poolStats.PoolStatistics.TotalHashes-poolBlocks[0].TotalHashes).Mul64(100000).Div(globalDiff).Lo) / 1000

			if currentEffort <= 0 || poolBlocks[0].TotalHashes == 0 {
				currentEffort = 0
			}

			var blockEfforts mapslice.MapSlice
			for i, b := range poolBlocks {
				if i < (len(poolBlocks)-1) && b.TotalHashes > 0 && poolBlocks[i+1].TotalHashes > 0 {
					blockEfforts = append(blockEfforts, mapslice.MapItem{
						Key:   b.Hash.String(),
						Value: float64((b.TotalHashes-poolBlocks[i+1].TotalHashes)*100) / float64(b.Difficulty),
					})
				}
			}

		*/
		currentEffort := float64(1)
		var blockEfforts mapslice.MapSlice
		blockEfforts = append(blockEfforts, mapslice.MapItem{
			Key:   types.ZeroHash.String(),
			Value: float64(1),
		})

		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		writer.WriteHeader(http.StatusOK)
		if buf, err := encodeJson(request, poolInfoResult{
			SideChain: poolInfoResultSideChain{
				Id:         tip.Id,
				Height:     tip.Height,
				Difficulty: tip.Difficulty,
				Timestamp:  tip.Timestamp,
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
					Weight: windowDifficulty,
				},
				WindowSize:   int(p2api.Consensus().ChainWindowSize),
				BlockTime:    int(p2api.Consensus().TargetBlockTime),
				UnclePenalty: int(p2api.Consensus().UnclePenalty),
				Found:        totalKnown.blocksFound,
				Miners:       totalKnown.minersKnown,
			},
			MainChain: poolInfoResultMainChain{
				Id:         tip.Template.Id,
				Height:     tip.Main.Height - 1,
				Difficulty: tip.Template.Difficulty,
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
		var miner *database.Miner
		if len(minerId) > 10 && minerId[0] == '4' {
			miner = api.GetDatabase().GetMinerByAddress(minerId)
		}

		if miner == nil {
			miner = api.GetDatabase().GetMinerByAlias(minerId)
		}

		if miner == nil {
			if i, err := strconv.Atoi(minerId); err == nil {
				miner = api.GetDatabase().GetMiner(uint64(i))
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

		var blockData struct {
			Count      uint64
			LastHeight uint64
		}
		_ = api.GetDatabase().Query("SELECT COUNT(*) as count, coalesce(MAX(height), 0) as last_height FROM blocks WHERE blocks.miner = $1;", func(row database.RowScanInterface) error {
			return row.Scan(&blockData.Count, &blockData.LastHeight)
		}, miner.Id())
		var uncleData struct {
			Count      uint64
			LastHeight uint64
		}
		_ = api.GetDatabase().Query("SELECT COUNT(*) as count, coalesce(MAX(parent_height), 0) as last_height FROM uncles WHERE uncles.miner = $1;", func(row database.RowScanInterface) error {
			return row.Scan(&uncleData.Count, &uncleData.LastHeight)
		}, miner.Id())

		var lastShareHeight uint64
		var lastShareTimestamp uint64
		if blockData.Count > 0 && blockData.LastHeight > lastShareHeight {
			lastShareHeight = blockData.LastHeight
		}
		if uncleData.Count > 0 && uncleData.LastHeight > lastShareHeight {
			lastShareHeight = uncleData.LastHeight
		}

		if lastShareHeight > 0 {
			lastShareTimestamp = api.GetDatabase().GetBlockByHeight(lastShareHeight).Timestamp
		}

		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		writer.WriteHeader(http.StatusOK)
		buf, _ := encodeJson(request, minerInfoResult{
			Id:      miner.Id(),
			Address: miner.MoneroAddress(),
			Alias:   miner.Alias(),
			Shares: struct {
				Blocks uint64 `json:"blocks"`
				Uncles uint64 `json:"uncles"`
			}{Blocks: blockData.Count, Uncles: uncleData.Count},
			LastShareHeight:    lastShareHeight,
			LastShareTimestamp: lastShareTimestamp,
		})
		_, _ = writer.Write(buf)
	})

	serveMux.HandleFunc("/api/miner_alias/{miner:4[123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz]+}", func(writer http.ResponseWriter, request *http.Request) {
		minerId := mux.Vars(request)["miner"]
		miner := api.GetDatabase().GetMinerByAddress(minerId)

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

		result := address.VerifyMessage(miner.MoneroAddress(), []byte(message), sig)
		if result == address.ResultSuccessSpend {
			if message == "REMOVE_MINER_ALIAS" {
				message = ""
			}
			if api.GetDatabase().SetMinerAlias(miner.Id(), message) != nil {
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
		var miner *database.Miner
		if len(minerId) > 10 && minerId[0] == '4' {
			miner = api.GetDatabase().GetMinerByAddress(minerId)
		}

		if miner == nil {
			if i, err := strconv.Atoi(minerId); err == nil {
				miner = api.GetDatabase().GetMiner(uint64(i))
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

		var from *uint64
		if params.Has("from") {
			if i, err := strconv.Atoi(params.Get("from")); err == nil {
				if i >= 0 {
					i := uint64(i)
					from = &i
				}
			}
		}

		result := make([]*sharesInWindowResult, 0)

		for block := range api.GetDatabase().GetBlocksByMinerIdInWindow(miner.Id(), from, window) {
			s := &sharesInWindowResult{
				Id:        block.Id,
				Height:    block.Height,
				Timestamp: block.Timestamp,
				Weight:    block.Difficulty.Lo,
			}

			for u := range api.GetDatabase().GetUnclesByParentId(block.Id) {
				uncleWeight := u.Block.Difficulty.Mul64(p2pool.UnclePenalty).Div64(100)
				s.Uncles = append(s.Uncles, sharesInWindowResultUncle{
					Id:     u.Block.Id,
					Height: u.Block.Height,
					Weight: uncleWeight.Lo,
				})
				s.Weight += uncleWeight.Lo
			}

			result = append(result, s)
		}

		for uncle := range api.GetDatabase().GetUnclesByMinerIdInWindow(miner.Id(), from, window) {
			s := &sharesInWindowResult{
				Parent: &sharesInWindowResultParent{
					Id:     uncle.ParentId,
					Height: uncle.ParentHeight,
				},
				Id:        uncle.Block.Id,
				Height:    uncle.Block.Height,
				Timestamp: uncle.Block.Timestamp,
				Weight:    uncle.Block.Difficulty.Mul64(100 - p2pool.UnclePenalty).Div64(100).Lo,
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
		var miner *database.Miner
		if len(minerId) > 10 && minerId[0] == '4' {
			miner = api.GetDatabase().GetMinerByAddress(minerId)
		}

		if miner == nil {
			if i, err := strconv.Atoi(minerId); err == nil {
				miner = api.GetDatabase().GetMiner(uint64(i))
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

		payouts := make([]*database.Payout, 0)

		for payout := range api.GetDatabase().GetPayoutsByMinerId(miner.Id(), limit) {
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
		c := api.GetDatabase().GetBlocksByQuery("WHERE height = $1 AND main_found IS TRUE", utils.DecodeBinaryNumber(mux.Vars(request)["coinbase"]))
		defer func() {
			for range c {

			}
		}()
		b := <-c
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

		if request.Header.Get("host") == torHost {
			http.Redirect(writer, request, fmt.Sprintf("http://yucmgsbw7nknw7oi3bkuwudvc657g2xcqahhbjyewazusyytapqo4xid.onion/explorer/tx/%s", b.Coinbase.Id.String()), http.StatusFound)
		} else {
			http.Redirect(writer, request, fmt.Sprintf("https://p2pool.io/explorer/tx/%s", b.Coinbase.Id.String()), http.StatusFound)
		}
	})
	serveMux.HandleFunc("/api/redirect/share/{height:[0-9]+|.?[0-9A-Za-z]+$}", func(writer http.ResponseWriter, request *http.Request) {
		c := utils.DecodeBinaryNumber(mux.Vars(request)["height"])

		blockHeight := c >> 16
		blockIdStart := c & 0xFFFF

		var b database.BlockInterface
		var b1 = <-api.GetDatabase().GetBlocksByQuery("WHERE height = $1 AND id ILIKE $2 LIMIT 1;", blockHeight, hex.EncodeToString([]byte{byte((blockIdStart >> 8) & 0xFF), byte(blockIdStart & 0xFF)})+"%")
		if b1 == nil {
			b2 := <-api.GetDatabase().GetUncleBlocksByQuery("WHERE height = $1 AND id ILIKE $2 LIMIT 1;", blockHeight, hex.EncodeToString([]byte{byte((blockIdStart >> 8) & 0xFF), byte(blockIdStart & 0xFF)})+"%")
			if b2 != nil {
				b = b2
			}
		} else {
			b = b1
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

		http.Redirect(writer, request, fmt.Sprintf("/share/%s", b.GetBlock().Id.String()), http.StatusFound)
	})

	serveMux.HandleFunc("/api/redirect/prove/{height_index:[0-9]+|.[0-9A-Za-z]+}", func(writer http.ResponseWriter, request *http.Request) {
		i := utils.DecodeBinaryNumber(mux.Vars(request)["height_index"])
		n := uint64(math.Ceil(math.Log2(float64(p2api.Consensus().ChainWindowSize * 4))))

		height := i >> n
		index := i & ((1 << n) - 1)

		b := api.GetDatabase().GetBlockByHeight(height)
		var tx *database.CoinbaseTransactionOutput
		if b != nil {
			tx = api.GetDatabase().GetCoinbaseTransactionOutputByIndex(b.Coinbase.Id, index)
		}

		if b == nil || tx == nil {
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

		http.Redirect(writer, request, fmt.Sprintf("/proof/%s/%d", b.Id.String(), tx.Index()), http.StatusFound)
	})

	serveMux.HandleFunc("/api/redirect/prove/{height:[0-9]+|.[0-9A-Za-z]+}/{miner:[0-9]+|.?[0-9A-Za-z]+}", func(writer http.ResponseWriter, request *http.Request) {
		b := api.GetDatabase().GetBlockByHeight(utils.DecodeBinaryNumber(mux.Vars(request)["height"]))
		miner := api.GetDatabase().GetMiner(utils.DecodeBinaryNumber(mux.Vars(request)["miner"]))
		if b == nil || miner == nil {
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

		tx := api.GetDatabase().GetCoinbaseTransactionOutputByMinerId(b.Coinbase.Id, miner.Id())

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

		http.Redirect(writer, request, fmt.Sprintf("/proof/%s/%d", b.Id.String(), tx.Index()), http.StatusFound)
	})

	serveMux.HandleFunc("/api/redirect/miner/{miner:[0-9]+|.?[0-9A-Za-z]+}", func(writer http.ResponseWriter, request *http.Request) {
		miner := api.GetDatabase().GetMiner(utils.DecodeBinaryNumber(mux.Vars(request)["miner"]))
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

		http.Redirect(writer, request, fmt.Sprintf("/miner/%s", miner.Address()), http.StatusFound)
	})

	//other redirects
	serveMux.HandleFunc("/api/redirect/last_found{kind:|/raw|/info}", func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, fmt.Sprintf("/api/block_by_id/%s%s%s", api.GetDatabase().GetLastFound().GetBlock().Id.String(), mux.Vars(request)["kind"], request.URL.RequestURI()), http.StatusFound)
	})
	serveMux.HandleFunc("/api/redirect/tip{kind:|/raw|/info}", func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, fmt.Sprintf("/api/block_by_id/%s%s%s", api.GetDatabase().GetChainTip().Id.String(), mux.Vars(request)["kind"], request.URL.RequestURI()), http.StatusFound)
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
			var miner *database.Miner

			if len(id) > 10 && id[0] == '4' {
				miner = api.GetDatabase().GetMinerByAddress(id)
			}

			if miner == nil {
				if i, err := strconv.Atoi(id); err == nil {
					miner = api.GetDatabase().GetMiner(uint64(i))
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

		result := make([]*database.Block, 0, limit)

		for block := range api.GetDatabase().GetAllFound(limit, minerId) {
			MapJSONBlock(api, block, false, params.Has("coinbase"))
			result = append(result, block.GetBlock())
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
			var miner *database.Miner

			if len(id) > 10 && id[0] == '4' {
				miner = api.GetDatabase().GetMinerByAddress(id)
			}

			if miner == nil {
				if i, err := strconv.Atoi(id); err == nil {
					miner = api.GetDatabase().GetMiner(uint64(i))
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

		result := make([]*database.Block, 0, limit)

		for b := range api.GetDatabase().GetShares(limit, minerId, onlyBlocks) {
			MapJSONBlock(api, b, true, params.Has("coinbase"))
			result = append(result, b.GetBlock())
		}

		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		writer.WriteHeader(http.StatusOK)
		buf, _ := encodeJson(request, result)
		_, _ = writer.Write(buf)
	})

	serveMux.HandleFunc("/api/block_by_{by:id|height}/{block:[0-9a-f]+|[0-9]+}{kind:|/raw|/info|/payouts}", func(writer http.ResponseWriter, request *http.Request) {

		params := request.URL.Query()

		var block database.BlockInterface
		if mux.Vars(request)["by"] == "id" {
			if id, err := types.HashFromString(mux.Vars(request)["block"]); err == nil {
				if b := api.GetDatabase().GetBlockById(id); b != nil {
					block = b
				}
			}
		} else if mux.Vars(request)["by"] == "height" {
			if height, err := strconv.ParseUint(mux.Vars(request)["block"], 10, 0); err == nil {
				if b := api.GetDatabase().GetBlockByHeight(height); b != nil {
					block = b
				}
			}
		}

		isOrphan := false
		isInvalid := false

		if block == nil && mux.Vars(request)["by"] == "id" {
			if id, err := types.HashFromString(mux.Vars(request)["block"]); err == nil {
				if b := api.GetDatabase().GetUncleById(id); b != nil {
					block = b
				} else {
					isOrphan = true

					bblock := p2api.ByTemplateId(id)

					if b, _, _ := database.NewBlockFromBinaryBlock(getSeedByHeight, p2api.MainDifficultyByHeight, db, bblock, nil, false); b != nil {
						block = b
					} else {
						isInvalid = true
						/*if b, _ = api.GetShareFromFailedRawEntry(id); b != nil {
							block = b
						}*/
					}
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

		switch mux.Vars(request)["kind"] {
		case "/raw":
			raw := p2api.ByTemplateId(block.GetBlock().Id)

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
			result := make([]*database.Payout, 0)
			if !isOrphan && !isInvalid {
				blockHeight := block.GetBlock().Height
				if uncle, ok := block.(*database.UncleBlock); ok {
					blockHeight = uncle.ParentHeight
				}

				if err := db.Query("SELECT decode(b.id, 'hex') AS id, decode(b.main_id, 'hex') AS main_id, b.height AS height, b.main_height AS main_height, b.timestamp AS timestamp, decode(b.coinbase_privkey, 'hex') AS coinbase_privkey, b.uncle AS uncle, decode(o.id, 'hex') AS coinbase_id, o.amount AS amount, o.index AS index FROM (SELECT id, amount, index FROM coinbase_outputs WHERE miner = $1 ) o LEFT JOIN LATERAL (SELECT id, coinbase_id, coinbase_privkey, height, main_height, main_id, timestamp, FALSE AS uncle FROM blocks WHERE coinbase_id = o.id UNION SELECT id, coinbase_id, coinbase_privkey, height, main_height, main_id, timestamp, TRUE AS uncle FROM uncles WHERE coinbase_id = o.id) b ON b.coinbase_id = o.id WHERE height >= $2 AND height < $3 ORDER BY main_height ASC;", func(row database.RowScanInterface) error {
					var blockId, mainId, privKey, coinbaseId []byte
					var height, mainHeight, timestamp, amount, index uint64
					var uncle bool

					if err := row.Scan(&blockId, &mainId, &height, &mainHeight, &timestamp, &privKey, &uncle, &coinbaseId, &amount, &index); err != nil {
						return err
					}

					result = append(result, &database.Payout{
						Id:        types.HashFromBytes(blockId),
						Height:    height,
						Timestamp: timestamp,
						Main: struct {
							Id     types.Hash `json:"id"`
							Height uint64     `json:"height"`
						}{Id: types.HashFromBytes(mainId), Height: mainHeight},
						Uncle: uncle,
						Coinbase: struct {
							Id         types.Hash             `json:"id"`
							Reward     uint64                 `json:"reward"`
							PrivateKey crypto.PrivateKeyBytes `json:"private_key"`
							Index      uint64                 `json:"index"`
						}{Id: types.HashFromBytes(coinbaseId), Reward: amount, PrivateKey: crypto.PrivateKeyBytes(types.HashFromBytes(privKey)), Index: index},
					})
					return nil
				}, block.GetBlock().MinerId, blockHeight, blockHeight+p2api.Consensus().ChainWindowSize); err != nil {
					return
				}
			}
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusOK)
			buf, _ := encodeJson(request, result)
			_, _ = writer.Write(buf)
		default:
			MapJSONBlock(api, block, true, params.Has("coinbase"))

			func() {
				block.GetBlock().Lock.Lock()
				defer block.GetBlock().Lock.Unlock()
				block.GetBlock().Orphan = isOrphan
				if isInvalid {
					block.GetBlock().Invalid = &isInvalid
				}
			}()

			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusOK)
			buf, _ := encodeJson(request, block.GetBlock())
			_, _ = writer.Write(buf)
		}
	})

	type difficultyStatResult struct {
		Height     uint64 `json:"height"`
		Timestamp  uint64 `json:"timestamp"`
		Difficulty uint64 `json:"difficulty"`
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

			_ = api.GetDatabase().Query("SELECT height,timestamp,difficulty FROM blocks WHERE height % $1 = 0 ORDER BY height DESC;", func(row database.RowScanInterface) error {
				r := &difficultyStatResult{}

				var difficultyHex string
				if err := row.Scan(&r.Height, &r.Timestamp, &difficultyHex); err != nil {
					return err
				}
				d, _ := types.DifficultyFromString(difficultyHex)
				r.Difficulty = d.Lo

				result = append(result, r)
				return nil
			}, 3600/p2api.Consensus().TargetBlockTime)

			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusOK)
			buf, _ := encodeJson(request, result)
			_, _ = writer.Write(buf)

		case "miner_seen":
			result := make([]*minerSeenResult, 0)

			_ = api.GetDatabase().Query("SELECT height,timestamp,(SELECT COUNT(DISTINCT(b.miner)) FROM blocks b WHERE b.height <= blocks.height AND b.height > (blocks.height - $2)) as count FROM blocks WHERE height % $1 = 0 ORDER BY height DESC;", func(row database.RowScanInterface) error {
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

			_ = api.GetDatabase().Query("SELECT height,timestamp,(SELECT COUNT(DISTINCT(b.miner)) FROM blocks b WHERE b.height <= blocks.height AND b.height > (blocks.height - $2)) as count FROM blocks WHERE height % $1 = 0 ORDER BY height DESC;", func(row database.RowScanInterface) error {
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
