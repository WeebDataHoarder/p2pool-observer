package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	utils2 "git.gammaspectra.live/P2Pool/p2pool-observer/cmd/utils"
	"git.gammaspectra.live/P2Pool/p2pool-observer/index"
	address2 "git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	types2 "git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"github.com/ake-persson/mapslice-json"
	"github.com/gorilla/mux"
	"github.com/tyler-sommer/stick"
	"github.com/tyler-sommer/stick/twig"
	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"
	"golang.org/x/net/html"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

func toUint64(t any) uint64 {
	if x, ok := t.(json.Number); ok {
		if n, err := x.Int64(); err == nil {
			return uint64(n)
		}
	}

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

func toInt64(t any) int64 {
	if x, ok := t.(json.Number); ok {
		if n, err := x.Int64(); err == nil {
			return n
		}
	}

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
	if x, ok := t.(json.Number); ok {
		if n, err := x.Float64(); err == nil {
			return n
		}
	}

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

func main() {
	client.SetDefaultClientSettings(os.Getenv("MONEROD_RPC_URL"))
	torHost := os.Getenv("TOR_SERVICE_ADDRESS")
	env := twig.New(&loader{})

	basePoolInfo := getFromAPI("pool_info", 5).(map[string]any)

	consensusData, _ := json.Marshal(basePoolInfo["sidechain"].(map[string]any)["consensus"].(map[string]any))
	consensus, err := sidechain.NewConsensusFromJSON(consensusData)
	if err != nil {
		log.Panic(err)
	}

	render := func(request *http.Request, writer http.ResponseWriter, template string, ctx map[string]stick.Value) {
		w := bytes.NewBuffer(nil)
		defer func() {
			_, _ = writer.Write(w.Bytes())
		}()
		defer func() {
			if err := recover(); err != nil {
				w = bytes.NewBuffer(nil)
				writer.WriteHeader(http.StatusInternalServerError)
				opts := make(map[string]stick.Value)
				e := make(map[string]stick.Value)
				opts["error"] = e
				e["code"] = http.StatusInternalServerError
				e["message"] = "Internal Server Error"
				e["content"] = "<pre>" + html.EscapeString(fmt.Sprintf("%s", err)) + "</pre>"
				if err = env.Execute("error.html", w, opts); err != nil {
					w = bytes.NewBuffer(nil)
					writer.Header().Set("content-type", "text/plain")
					_, _ = w.Write([]byte(fmt.Sprintf("%s", err)))
				}
			}
		}()

		if ctx == nil {
			ctx = make(map[string]stick.Value)
		}
		ctx["is_onion"] = request.Host == torHost
		ctx["consensus"] = consensus

		if err := env.Execute(template, w, ctx); err != nil {
			w = bytes.NewBuffer(nil)
			writer.WriteHeader(http.StatusInternalServerError)
			opts := make(map[string]stick.Value)
			e := make(map[string]stick.Value)
			opts["error"] = e
			e["code"] = http.StatusInternalServerError
			e["message"] = "Internal Server Error"
			e["content"] = "<pre>" + html.EscapeString(err.Error()) + "</pre>"
			if err = env.Execute("error.html", w, opts); err != nil {
				w = bytes.NewBuffer(nil)
				writer.Header().Set("content-type", "text/plain")
				_, _ = w.Write([]byte(err.Error()))
			}
		}
	}

	env.Functions["getenv"] = func(ctx stick.Context, args ...stick.Value) stick.Value {
		if len(args) == 0 {
			return ""
		}
		return os.Getenv(args[0].(string))
	}
	env.Functions["diff_hashrate"] = func(ctx stick.Context, args ...stick.Value) stick.Value {
		if d, err := types.DifficultyFromString(args[0].(string)); err == nil {
			return d.Div64(toUint64(args[1])).Lo
		}
		return 0
	}
	env.Functions["diff_uint"] = func(ctx stick.Context, args ...stick.Value) stick.Value {
		if d, err := types.DifficultyFromString(args[0].(string)); err == nil {
			return d.Lo
		}
		return 0
	}
	env.Functions["monero_to_xmr"] = func(ctx stick.Context, args ...stick.Value) stick.Value {
		v := toUint64(args[0])
		return fmt.Sprintf("%d.%012d", v/1000000000000, v%1000000000000)
	}
	env.Functions["utc_date"] = func(ctx stick.Context, args ...stick.Value) stick.Value {
		return time.Unix(int64(toUint64(args[0])), 0).UTC().Format("02-01-2006 15:04:05 MST")
	}
	env.Functions["time_elapsed"] = func(ctx stick.Context, args ...stick.Value) stick.Value {
		diff := time.Since(time.Unix(int64(toUint64(args[0])), 0).UTC())

		days := int64(diff.Hours() / 24)
		hours := int64(diff.Hours()) % 24
		minutes := int64(diff.Minutes()) % 60
		seconds := int64(diff.Seconds()) % 60

		var result []string
		if days > 0 {
			result = append(result, strconv.FormatInt(days, 10)+"d")
		}
		if hours > 0 {
			result = append(result, strconv.FormatInt(hours, 10)+"h")
		}
		if minutes > 0 {
			result = append(result, strconv.FormatInt(minutes, 10)+"m")
		}
		if seconds > 0 {
			result = append(result, strconv.FormatInt(seconds, 10)+"s")
		}

		if toUint64(args[0]) == 0 {
			return "never"
		} else if len(result) == 0 {
			return "just now"
		} else {
			return strings.Join(result, " ") + " ago"
		}
	}
	env.Functions["time_elapsed_short"] = func(ctx stick.Context, args ...stick.Value) stick.Value {
		diff := time.Since(time.Unix(int64(toUint64(args[0])), 0).UTC())

		days := int64(diff.Hours() / 24)
		hours := int64(diff.Hours()) % 24
		minutes := int64(diff.Minutes()) % 60
		seconds := int64(diff.Seconds()) % 60

		var result []string
		if days > 0 {
			result = append(result, strconv.FormatInt(days, 10)+"d")
		}
		if hours > 0 {
			result = append(result, strconv.FormatInt(hours, 10)+"h")
		}
		if minutes > 0 {
			result = append(result, strconv.FormatInt(minutes, 10)+"m")
		}
		if seconds > 0 {
			result = append(result, strconv.FormatInt(seconds, 10)+"s")
		}

		if toUint64(args[0]) == 0 {
			return "never"
		} else if len(result) == 0 {
			return "just now"
		} else {
			return strings.Join(result[0:1], " ") + " ago"
		}
	}
	env.Functions["time_duration_long"] = func(ctx stick.Context, args ...stick.Value) stick.Value {

		diff := time.Second * time.Duration(toUint64(args[0]))
		diff += time.Microsecond * time.Duration((toFloat64(toUint64(args[0]))-toFloat64(args[0]))*1000000)
		days := int64(diff.Hours() / 24)
		hours := int64(diff.Hours()) % 24
		minutes := int64(diff.Minutes()) % 60
		seconds := int64(diff.Seconds()) % 60
		ms := int64(diff.Milliseconds()) % 1000

		var result []string
		if days > 0 {
			result = append(result, strconv.FormatInt(days, 10)+"d")
		}
		if hours > 0 {
			result = append(result, strconv.FormatInt(hours, 10)+"h")
		}
		if minutes > 0 {
			result = append(result, strconv.FormatInt(minutes, 10)+"m")
		}
		if seconds > 0 {
			result = append(result, strconv.FormatInt(seconds, 10)+"s")
		}

		if len(result) == 0 || (len(result) == 1 && seconds > 0) {
			result = append(result, strconv.FormatInt(ms, 10)+"ms")
		}

		return strings.Join(result, " ")
	}

	env.Functions["add_uint"] = func(ctx stick.Context, args ...stick.Value) stick.Value {
		var result uint64
		for _, v := range args {
			result += toUint64(v)
		}

		return result
	}

	env.Functions["sub_int"] = func(ctx stick.Context, args ...stick.Value) stick.Value {
		result := toInt64(args[0])
		for _, v := range args[1:] {
			result -= toInt64(v)
		}

		return result
	}

	env.Functions["date_diff_short"] = func(ctx stick.Context, args ...stick.Value) stick.Value {
		diff := time.Since(time.Unix(int64(toUint64(args[0])), 0).UTC())
		s := fmt.Sprintf("%02d:%02d:%02d", int64(diff.Hours())%24, int64(diff.Minutes())%60, int64(diff.Seconds())%60)

		days := int64(diff.Hours() / 24)
		if days > 0 {
			return strconv.FormatInt(days, 10) + ":" + s
		}

		return s
	}

	env.Functions["prove_output_number"] = func(ctx stick.Context, args ...stick.Value) stick.Value {
		if len(args) != 2 {
			return nil
		}

		n := uint64(math.Ceil(math.Log2(float64(consensus.ChainWindowSize * 4))))

		//height | index

		return (toUint64(args[0]) << n) | toUint64(args[1])
	}

	env.Functions["get_tx_proof"] = func(ctx stick.Context, args ...stick.Value) stick.Value {
		if len(args) != 3 {
			return nil
		}
		h, _ := types.HashFromString(args[1].(string))
		k, _ := types.HashFromString(args[2].(string))
		keyBytes := crypto.PrivateKeyBytes(k)
		return address2.GetTxProofV2(address2.FromBase58(args[0].(string)), h, &keyBytes, "")
	}

	env.Functions["get_tx_proof_v1"] = func(ctx stick.Context, args ...stick.Value) stick.Value {
		if len(args) != 3 {
			return nil
		}
		h, _ := types.HashFromString(args[1].(string))
		k, _ := types.HashFromString(args[2].(string))
		keyBytes := crypto.PrivateKeyBytes(k)
		return address2.GetTxProofV1(address2.FromBase58(args[0].(string)), h, &keyBytes, "")
	}

	env.Functions["get_ephemeral_pubkey"] = func(ctx stick.Context, args ...stick.Value) stick.Value {
		if len(args) != 3 {
			return nil
		}
		k, _ := types.HashFromString(args[1].(string))
		keyBytes := crypto.PrivateKeyBytes(k)
		return address2.GetEphemeralPublicKey(address2.FromBase58(args[0].(string)), &keyBytes, toUint64(args[2]))
	}

	env.Functions["coinbase_extra"] = func(ctx stick.Context, args ...stick.Value) stick.Value {
		if len(args) != 1 {
			return nil
		}
		b, _ := args[0].(*sidechain.PoolBlock).Main.Coinbase.Extra.MarshalBinary()
		return b
	}

	env.Functions["extra_nonce"] = func(ctx stick.Context, args ...stick.Value) stick.Value {
		if len(args) != 1 {
			return nil
		}
		return args[0].(*sidechain.PoolBlock).CoinbaseExtra(sidechain.SideExtraNonce)
	}

	env.Functions["software_info"] = func(ctx stick.Context, args ...stick.Value) stick.Value {
		if len(args) != 2 {
			return nil
		}
		return fmt.Sprintf("%s %s", types2.SoftwareId(toUint64(args[0])).String(), types2.SoftwareVersion(toUint64(args[1])).String())
	}

	env.Functions["get_url"] = func(ctx stick.Context, args ...stick.Value) stick.Value {
		if len(args) != 1 {
			return nil
		}

		if k, ok := args[0].(string); ok {
			isOnion, _ := ctx.Scope().Get("is_onion")
			return utils2.GetSiteUrlByHost(k, isOnion != nil && isOnion.(bool))
		}
		return ""
	}

	env.Functions["side_block_weight"] = func(ctx stick.Context, args ...stick.Value) stick.Value {
		if len(args) != 3 {
			return nil
		}

		if sideBlock, ok := args[0].(*index.SideBlock); ok {
			w, _ := sideBlock.Weight(toUint64(args[1]), toUint64(args[2]), consensus.UnclePenalty)
			return w
		}
		return 0
	}

	env.Functions["attribute"] = func(ctx stick.Context, args ...stick.Value) stick.Value {
		if len(args) != 2 {
			return nil
		}

		if s, ok := args[0].([]any); ok {
			return s[toUint64(args[1])]
		} else if m, ok := args[0].(map[string]any); ok {
			return m[args[1].(string)]
		}
		return nil
	}
	env.Filters["slice_sum"] = func(ctx stick.Context, val stick.Value, args ...stick.Value) stick.Value {
		var result uint64

		stick.Iterate(val, func(k, v stick.Value, l stick.Loop) (brk bool, err error) {
			result += toUint64(v)
			return false, nil
		})

		return result
	}
	env.Filters["diff_div"] = func(ctx stick.Context, val stick.Value, args ...stick.Value) stick.Value {
		if d, ok := val.(types.Difficulty); ok {
			return d.Div64(toUint64(args[0])).String()
		} else if d, err := types.DifficultyFromString(val.(string)); err == nil {
			return d.Div64(toUint64(args[0])).String()
		}
		log.Panic()
		return val
	}
	env.Filters["diff_int"] = func(ctx stick.Context, val stick.Value, args ...stick.Value) stick.Value {
		if d, ok := val.(types.Difficulty); ok {
			return d.Lo
		} else if d, err := types.DifficultyFromString(val.(string)); err == nil {
			return d.Lo
		}
		log.Panic()
		return 0
	}
	env.Filters["benc"] = func(ctx stick.Context, val stick.Value, args ...stick.Value) stick.Value {
		if s, ok := val.(string); ok {
			if n, err := strconv.ParseUint(s, 10, 0); err == nil {
				return utils.EncodeBinaryNumber(n)
			}
		} else {
			return utils.EncodeBinaryNumber(toUint64(val))
		}

		//TODO: remove this
		log.Panic()
		return ""
	}

	env.Filters["hex"] = func(ctx stick.Context, val stick.Value, args ...stick.Value) stick.Value {
		if s, ok := val.(string); ok {
			return s
		} else if s, ok := val.(types.Difficulty); ok {
			return s.String()
		} else if s, ok := val.(crypto.PrivateKey); ok {
			return s.String()
		} else if s, ok := val.(crypto.PublicKey); ok {
			return s.String()
		} else if s, ok := val.(crypto.PrivateKeyBytes); ok {
			return s.String()
		} else if s, ok := val.(crypto.PublicKeyBytes); ok {
			return s.String()
		} else if s, ok := val.(types.Hash); ok {
			return s.String()
		} else if s, ok := val.([]byte); ok {
			return hex.EncodeToString(s)
		} else if s, ok := val.(uint32); ok {
			var buf [4]byte
			binary.BigEndian.PutUint32(buf[:], s)
			return hex.EncodeToString(buf[:])
		} else if s, ok := val.(uint64); ok {
			var buf [8]byte
			binary.BigEndian.PutUint64(buf[:], s)
			return hex.EncodeToString(buf[:])
		}

		return val
	}

	env.Filters["henc"] = func(ctx stick.Context, val stick.Value, args ...stick.Value) stick.Value {

		if h, ok := val.(types.Hash); ok {
			return utils.EncodeHexBinaryNumber(h.String())
		} else if s, ok := val.(string); ok {
			return utils.EncodeHexBinaryNumber(s)
		}

		//TODO: remove this
		log.Panic()
		return ""
	}
	env.Filters["effort_color"] = func(ctx stick.Context, val stick.Value, args ...stick.Value) stick.Value {
		if effort, ok := val.(float64); ok {
			if effort < 100 {
				return "#00C000"
			} else if effort < 200 {
				return "#E0E000"
			} else {
				return "#FF0000"
			}
		}

		return "#000000"
	}

	env.Filters["si_units"] = func(ctx stick.Context, val stick.Value, args ...stick.Value) stick.Value {
		v := toFloat64(val)
		if len(args) > 0 {
			return utils.SiUnits(v, int(toUint64(args[0])))
		} else {
			return utils.SiUnits(v, 3)
		}
	}
	env.Filters["effort_color"] = func(ctx stick.Context, val stick.Value, args ...stick.Value) stick.Value {

		effort := toFloat64(val)
		if effort < 100 {
			return "#00C000"
		} else if effort < 200 {
			return "#E0E000"
		} else {
			return "#FF0000"
		}
	}
	env.Filters["shorten"] = func(ctx stick.Context, val stick.Value, args ...stick.Value) stick.Value {
		var value string
		if s, ok := val.(string); ok {
			value = s
		} else {
			value = fmt.Sprintf("%s", value)
		}

		n := int(toUint64(args[0]))
		if len(value) <= n*2+3 {
			return value
		} else {
			return value[:n] + "..." + value[len(value)-n:]
		}
	}
	env.Filters["intstr"] = func(ctx stick.Context, val stick.Value, args ...stick.Value) stick.Value {
		return strconv.FormatUint(toUint64(val), 10)
	}
	env.Filters["str"] = func(ctx stick.Context, val stick.Value, args ...stick.Value) stick.Value {
		if strVal, ok := val.(fmt.Stringer); ok {
			return strVal.String()
		}
		return strconv.FormatUint(toUint64(val), 10)
	}
	env.Tests["defined"] = func(ctx stick.Context, val stick.Value, args ...stick.Value) bool {
		return val != nil
	}

	serveMux := mux.NewRouter()

	serveMux.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		params := request.URL.Query()
		if params.Has("refresh") {
			writer.Header().Set("refresh", "120")
		}

		poolInfo := getFromAPI("pool_info", 5).(map[string]any)

		d1, _ := types.DifficultyFromString(poolInfo["mainchain"].(map[string]any)["difficulty"].(string))
		d2, _ := types.DifficultyFromString(poolInfo["sidechain"].(map[string]any)["difficulty"].(string))
		secondsPerBlock := float64(d1.Lo) / float64(d2.Div64(toUint64(poolInfo["sidechain"].(map[string]any)["block_time"])).Lo)

		blocksToFetch := uint64(math.Ceil((((time.Hour*24).Seconds()/secondsPerBlock)*2)/100) * 100)

		blocks := getFromAPI(fmt.Sprintf("found_blocks?coinbase&limit=%d", blocksToFetch), 5).([]any)
		shares := getSideBlocksFromAPI("side_blocks?limit=50", 5)

		ctx := make(map[string]stick.Value)
		ctx["refresh"] = writer.Header().Get("refresh")

		blocksFound := NewPositionChart(30*4, consensus.ChainWindowSize*4)

		tip := toInt64(poolInfo["sidechain"].(map[string]any)["height"])
		for _, b := range blocks {
			blocksFound.Add(int(tip-toInt64(b.(map[string]any)["height"])), 1)
		}

		if len(blocks) > 20 {
			blocks = blocks[:20]
		}

		ctx["blocks_found"] = blocks
		ctx["blocks_found_position"] = blocksFound.String()
		ctx["shares"] = shares
		ctx["pool"] = poolInfo

		render(request, writer, "index.html", ctx)
	})

	serveMux.HandleFunc("/api", func(writer http.ResponseWriter, request *http.Request) {
		render(request, writer, "api.html", nil)
	})

	serveMux.HandleFunc("/calculate-share-time", func(writer http.ResponseWriter, request *http.Request) {
		poolInfo := getFromAPI("pool_info", 5)
		hashRate := float64(0)
		magnitude := float64(1000)

		params := request.URL.Query()
		if params.Has("hashrate") {
			hashRate = toFloat64(params.Get("hashrate"))
		}
		if params.Has("magnitude") {
			magnitude = toFloat64(params.Get("magnitude"))
		}

		ctx := make(map[string]stick.Value)
		ctx["hashrate"] = hashRate
		ctx["magnitude"] = magnitude
		ctx["pool"] = poolInfo

		render(request, writer, "calculate-share-time.html", ctx)
	})

	serveMux.HandleFunc("/blocks", func(writer http.ResponseWriter, request *http.Request) {
		params := request.URL.Query()
		if params.Has("refresh") {
			writer.Header().Set("refresh", "600")
		}

		var miner map[string]any
		if params.Has("miner") {
			m := getFromAPI(fmt.Sprintf("miner_info/%s", params.Get("miner")))
			if m == nil || m.(map[string]any)["address"] == nil {
				ctx := make(map[string]stick.Value)
				error := make(map[string]stick.Value)
				ctx["error"] = error
				ctx["code"] = http.StatusNotFound
				error["message"] = "Address Not Found"
				error["content"] = "<div class=\"center\" style=\"text-align: center\">You need to have mined at least one share in the past. Come back later :)</div>"
				render(request, writer, "error.html", ctx)
				return
			}
			miner = m.(map[string]any)
		}

		poolInfo := getFromAPI("pool_info", 5).(map[string]any)

		ctx := make(map[string]stick.Value)
		ctx["refresh"] = writer.Header().Get("refresh")
		ctx["pool"] = poolInfo

		if miner != nil {
			blocks := getFromAPI(fmt.Sprintf("found_blocks?&limit=100&miner=%d&coinbase", toUint64(miner["id"])))
			ctx["blocks_found"] = blocks
			ctx["miner"] = miner

			render(request, writer, "blocks_miner.html", ctx)
		} else {
			blocks := getFromAPI("found_blocks?limit=100&coinbase", 30)
			ctx["blocks_found"] = blocks

			render(request, writer, "blocks.html", ctx)
		}
	})

	serveMux.HandleFunc("/miners", func(writer http.ResponseWriter, request *http.Request) {
		params := request.URL.Query()
		if params.Has("refresh") {
			writer.Header().Set("refresh", "600")
		}

		poolInfo := getFromAPI("pool_info", 5).(map[string]any)

		currentWindowSize := toUint64(poolInfo["sidechain"].(map[string]any)["window_size"])
		windowSize := currentWindowSize
		if toUint64(poolInfo["sidechain"].(map[string]any)["height"]) <= windowSize {
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

		miners := make(map[uint64]map[string]any, 0)

		tipHeight := toUint64(poolInfo["sidechain"].(map[string]any)["height"])
		wend := tipHeight - windowSize

		tip := shares[0]

		createMiner := func(miner uint64, share *index.SideBlock) {
			if _, ok := miners[miner]; !ok {
				miners[miner] = make(map[string]any)
				miners[miner]["address"] = share.MinerAddress.ToBase58()
				miners[miner]["software_id"] = share.SoftwareId
				miners[miner]["software_version"] = share.SoftwareVersion
				miners[miner]["weight"] = types.ZeroDifficulty
				miners[miner]["shares"] = NewPositionChart(size, windowSize)
				miners[miner]["uncles"] = NewPositionChart(size, windowSize)
				if share.MinerAlias != "" {
					miners[miner]["alias"] = share.MinerAlias
				}
			}
		}

		var totalWeight types.Difficulty
		for _, share := range shares {
			miner := share.Miner

			if share.IsUncle() {
				if share.SideHeight <= wend {
					continue
				}
				createMiner(share.Miner, share)
				miners[miner]["uncles"].(*PositionChart).Add(int(int64(tip.SideHeight)-int64(share.SideHeight)), 1)

				unclePenalty := types.DifficultyFrom64(share.Difficulty).Mul64(consensus.UnclePenalty).Div64(100)
				uncleWeight := share.Difficulty - unclePenalty.Lo

				if i := slices.IndexFunc(shares, func(block *index.SideBlock) bool {
					return block.TemplateId == share.UncleOf
				}); i != -1 {
					parent := shares[i]
					createMiner(parent.Miner, parent)
					miners[parent.Miner]["weight"] = miners[parent.Miner]["weight"].(types.Difficulty).Add64(unclePenalty.Lo)
				}
				miners[miner]["weight"] = miners[miner]["weight"].(types.Difficulty).Add64(uncleWeight)

				totalWeight = totalWeight.Add64(share.Difficulty)
			} else {
				createMiner(share.Miner, share)
				miners[miner]["shares"].(*PositionChart).Add(int(int64(tip.SideHeight)-int64(share.SideHeight)), 1)
				miners[miner]["weight"] = miners[miner]["weight"].(types.Difficulty).Add64(share.Difficulty)
				totalWeight = totalWeight.Add64(share.Difficulty)
			}
		}

		minerKeys := maps.Keys(miners)
		slices.SortFunc(minerKeys, func(a uint64, b uint64) bool {
			return miners[a]["weight"].(types.Difficulty).Cmp(miners[b]["weight"].(types.Difficulty)) > 0
		})

		sortedMiners := make(mapslice.MapSlice, len(minerKeys))

		for i, k := range minerKeys {
			sortedMiners[i].Key = miners[k]["address"].(string)
			sortedMiners[i].Value = miners[k]
		}

		ctx := make(map[string]stick.Value)
		ctx["refresh"] = writer.Header().Get("refresh")
		ctx["miners"] = sortedMiners
		ctx["tip"] = tip
		ctx["pool"] = poolInfo
		ctx["window_weight"] = totalWeight

		if params.Has("weekly") {
			render(request, writer, "miners_week.html", ctx)
		} else {
			render(request, writer, "miners.html", ctx)
		}
	})

	serveMux.HandleFunc("/share/{block:[0-9a-f]+|[0-9]+}", func(writer http.ResponseWriter, request *http.Request) {
		identifier := mux.Vars(request)["block"]

		var block any
		var rawBlock any
		if len(identifier) == 64 {
			block = getFromAPI(fmt.Sprintf("block_by_id/%s?coinbase", identifier))
			rawBlock = getFromAPI(fmt.Sprintf("block_by_id/%s/raw", identifier))
		} else {
			block = getFromAPI(fmt.Sprintf("block_by_height/%s?coinbase", identifier))
			rawBlock = getFromAPI(fmt.Sprintf("block_by_height/%s/raw", identifier))
		}

		if block == nil {
			ctx := make(map[string]stick.Value)
			error := make(map[string]stick.Value)
			ctx["error"] = error
			ctx["code"] = http.StatusNotFound
			error["message"] = "Share Not Found"
			render(request, writer, "error.html", ctx)
			return
		}

		poolInfo := getFromAPI("pool_info", 5)

		var raw *sidechain.PoolBlock
		if s, ok := rawBlock.([]byte); ok && rawBlock != nil {
			b := &sidechain.PoolBlock{
				NetworkType: sidechain.NetworkMainnet,
			}

			if b.UnmarshalBinary(&sidechain.NilDerivationCache{}, s) == nil {
				raw = b
			}
		}

		payouts := getFromAPI(fmt.Sprintf("block_by_id/%s/payouts", block.(map[string]any)["id"].(string)))

		ctx := make(map[string]stick.Value)
		ctx["block"] = block
		ctx["raw"] = raw
		ctx["pool"] = poolInfo
		ctx["payouts"] = payouts

		render(request, writer, "share.html", ctx)
	})

	serveMux.HandleFunc("/miner/{miner:[^ ]+}", func(writer http.ResponseWriter, request *http.Request) {
		params := request.URL.Query()
		if params.Has("refresh") {
			writer.Header().Set("refresh", "300")
		}
		address := mux.Vars(request)["miner"]
		m := getFromAPI(fmt.Sprintf("miner_info/%s", address))
		if m == nil || m.(map[string]any)["address"] == nil {
			if addr := address2.FromBase58(address); addr != nil {
				miner := make(map[string]any)
				m = miner
				miner["id"] = uint64(0)
				miner["address"] = addr.ToBase58()
				miner["last_share_height"] = uint64(0)
				miner["last_share_timestamp"] = uint64(0)
			} else {
				ctx := make(map[string]stick.Value)
				error := make(map[string]stick.Value)
				ctx["error"] = error
				ctx["code"] = http.StatusNotFound
				error["message"] = "Invalid Address"
				render(request, writer, "error.html", ctx)
				return
			}
		}

		miner := m.(map[string]any)

		poolInfo := getFromAPI("pool_info", 5).(map[string]any)

		const totalWindows = 4
		wsize := consensus.ChainWindowSize * totalWindows

		currentWindowSize := toUint64(poolInfo["sidechain"].(map[string]any)["window_size"])

		tipHeight := toUint64(poolInfo["sidechain"].(map[string]any)["height"])

		var shares, lastShares []*index.SideBlock

		var payouts, lastFound []any
		if toUint64(miner["id"]) != 0 {
			shares = getSideBlocksFromAPI(fmt.Sprintf("side_blocks_in_window/%d?from=%d&window=%d&noMiner&noMainStatus&noUncles", toUint64(miner["id"]), tipHeight, wsize))
			payouts = getFromAPI(fmt.Sprintf("payouts/%d?search_limit=1000", toUint64(miner["id"]))).([]any)
			lastShares = getSideBlocksFromAPI(fmt.Sprintf("side_blocks?limit=50&miner=%d", toUint64(miner["id"])))
			lastFound = getFromAPI(fmt.Sprintf("found_blocks?limit=5&miner=%d&coinbase", toUint64(miner["id"]))).([]any)
		}

		sharesFound := NewPositionChart(30*totalWindows, consensus.ChainWindowSize*totalWindows)
		unclesFound := NewPositionChart(30*totalWindows, consensus.ChainWindowSize*totalWindows)

		var sharesInWindow, unclesInWindow uint64
		var longDiff, windowDiff types.Difficulty

		wend := tipHeight - currentWindowSize

		foundPayout := NewPositionChart(30*totalWindows, consensus.ChainWindowSize*totalWindows)
		for _, p := range payouts {
			foundPayout.Add(int(int64(tipHeight)-toInt64(p.(map[string]any)["height"])), 1)
		}

		var raw *sidechain.PoolBlock

		if len(lastShares) > 0 {
			rawBlock := getFromAPIRaw(fmt.Sprintf("block_by_id/%s/light", lastShares[0].MainId))
			b := &sidechain.PoolBlock{}
			if json.Unmarshal(rawBlock, b) == nil && b.NetworkType != sidechain.NetworkInvalid {
				raw = b
			}
		}

		for _, share := range shares {
			if share.IsUncle() {
				if share.SideHeight <= wend {
					continue
				}

				unclesFound.Add(int(int64(tipHeight)-toInt64(share.SideHeight)), 1)

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

		ctx := make(map[string]stick.Value)
		ctx["refresh"] = writer.Header().Get("refresh")
		ctx["pool"] = poolInfo
		ctx["miner"] = miner
		if raw != nil {
			ctx["last_raw_share"] = raw
		}
		ctx["last_shares"] = lastShares
		ctx["last_found"] = lastFound
		ctx["last_payouts"] = payouts
		ctx["window_weight"] = windowDiff.Lo
		ctx["weight"] = longDiff.Lo
		ctx["window_count_blocks"] = sharesInWindow
		ctx["window_count_uncles"] = unclesInWindow
		ctx["count_blocks"] = sharesFound.Total()
		ctx["count_uncles"] = unclesFound.Total()
		ctx["count_payouts"] = foundPayout.Total()
		ctx["position_resolution"] = foundPayout.Resolution()
		ctx["position_blocks"] = sharesFound.StringWithSeparator(int(consensus.ChainWindowSize*totalWindows - currentWindowSize))
		ctx["position_uncles"] = unclesFound.StringWithSeparator(int(consensus.ChainWindowSize*totalWindows - currentWindowSize))
		ctx["position_payouts"] = foundPayout.StringWithSeparator(int(consensus.ChainWindowSize*totalWindows - currentWindowSize))
		render(request, writer, "miner.html", ctx)
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
		index := toUint64(mux.Vars(request)["index"])

		block := getFromAPI(fmt.Sprintf("block_by_id/%s?coinbase", identifier)).(map[string]any)

		if block == nil || block["main"].(map[string]any)["found"] == false || block["coinbase"].(map[string]any)["payouts"] == nil {
			ctx := make(map[string]stick.Value)
			error := make(map[string]stick.Value)
			ctx["error"] = error
			ctx["code"] = http.StatusNotFound
			error["message"] = "Share Was Not Found"
			render(request, writer, "error.html", ctx)
			return
		}

		payouts := block["coinbase"].(map[string]any)["payouts"].([]any)
		if uint64(len(payouts)) <= index {
			ctx := make(map[string]stick.Value)
			error := make(map[string]stick.Value)
			ctx["error"] = error
			ctx["code"] = http.StatusNotFound
			error["message"] = "Output Not Found"
			render(request, writer, "error.html", ctx)
			return
		}

		poolInfo := getFromAPI("pool_info", 5).(map[string]any)

		ctx := make(map[string]stick.Value)
		ctx["block"] = block
		ctx["payout"] = payouts[index]
		ctx["pool"] = poolInfo

		render(request, writer, "proof.html", ctx)
	})

	serveMux.HandleFunc("/payouts/{miner:[0-9]+|4[123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz]+}", func(writer http.ResponseWriter, request *http.Request) {
		params := request.URL.Query()
		if params.Has("refresh") {
			writer.Header().Set("refresh", "600")
		}

		address := mux.Vars(request)["miner"]
		if params.Has("address") {
			address = params.Get("address")
		}
		m := getFromAPI(fmt.Sprintf("miner_info/%s", address))

		if m == nil {
			ctx := make(map[string]stick.Value)
			error := make(map[string]stick.Value)
			ctx["error"] = error
			ctx["code"] = http.StatusNotFound
			error["message"] = "Address Not Found"
			error["content"] = "<div class=\"center\" style=\"text-align: center\">You need to have mined at least one share in the past. Come back later :)</div>"
			render(request, writer, "error.html", ctx)
			return
		}

		miner := m.(map[string]any)

		payouts := getFromAPI(fmt.Sprintf("payouts/%d?search_limit=0", toUint64(miner["id"]))).([]any)
		if len(payouts) == 0 {
			ctx := make(map[string]stick.Value)
			error := make(map[string]stick.Value)
			ctx["error"] = error
			ctx["code"] = http.StatusNotFound
			error["message"] = "No payout for address found"
			error["content"] = "<div class=\"center\" style=\"text-align: center\">You need to have mined at least one share in the past, and a main block found during that period. Come back later :)</div>"
			render(request, writer, "error.html", ctx)
			return
		}

		ctx := make(map[string]stick.Value)
		ctx["refresh"] = writer.Header().Get("refresh")
		ctx["miner"] = miner
		ctx["payouts"] = payouts
		ctx["total"] = func() (result uint64) {
			for _, p := range payouts {
				result += toUint64(p.(map[string]any)["coinbase"].(map[string]any)["reward"])
			}
			return
		}()
		render(request, writer, "payouts.html", ctx)
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

	if err := server.ListenAndServe(); err != nil {
		log.Panic(err)
	}
}
