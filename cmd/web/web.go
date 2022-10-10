package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool"
	block2 "git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/block"
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
	}

	return 0
}

func main() {
	env := twig.New(&loader{})

	render := func(writer http.ResponseWriter, template string, ctx map[string]stick.Value) {
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
		hours := int64(diff.Hours())
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

		if len(result) == 0 {
			return "just now"
		} else {
			return strings.Join(result, " ") + " ago"
		}
	}
	env.Functions["time_elapsed_short"] = func(ctx stick.Context, args ...stick.Value) stick.Value {
		diff := time.Since(time.Unix(int64(toUint64(args[0])), 0).UTC())

		days := int64(diff.Hours() / 24)
		hours := int64(diff.Hours())
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

		if len(result) == 0 {
			return "just now"
		} else {
			return strings.Join(result[0:1], " ") + " ago"
		}
	}
	env.Functions["time_duration_long"] = func(ctx stick.Context, args ...stick.Value) stick.Value {

		diff := time.Second * time.Duration(toUint64(args[0]))
		diff += time.Microsecond * time.Duration((toFloat64(toUint64(args[0]))-toFloat64(args[0]))*1000000)
		days := int64(diff.Hours() / 24)
		hours := int64(diff.Hours())
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

	env.Functions["date_diff_short"] = func(ctx stick.Context, args ...stick.Value) stick.Value {
		diff := time.Since(time.Unix(int64(toUint64(args[0])), 0).UTC())
		s := fmt.Sprintf("%02d:%02d:%02d", int64(diff.Hours())%24, int64(diff.Minutes())%60, int64(diff.Seconds())%60)

		days := int64(diff.Hours() / 24)
		if days > 0 {
			return strconv.FormatInt(days, 10) + ":" + s
		}

		return s
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

	env.Filters["gmp_init"] = func(ctx stick.Context, val stick.Value, args ...stick.Value) stick.Value {
		return "TODO gmp_init"
	}
	env.Filters["gmp_intval"] = func(ctx stick.Context, val stick.Value, args ...stick.Value) stick.Value {
		return "TODO gmp_intval"
	}
	env.Filters["gmp_div"] = func(ctx stick.Context, val stick.Value, args ...stick.Value) stick.Value {
		return "TODO gmp_div"
	}
	env.Filters["bcdiv"] = func(ctx stick.Context, val stick.Value, args ...stick.Value) stick.Value {
		return "TODO bcdiv"
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
		} else if s, ok := val.(types.Hash); ok {
			return s.String()
		} else if s, ok := val.(types.Nonce); ok {
			return hex.EncodeToString(s[:])
		} else if s, ok := val.([]byte); ok {
			return hex.EncodeToString(s)
		}

		return val
	}

	env.Filters["henc"] = func(ctx stick.Context, val stick.Value, args ...stick.Value) stick.Value {

		if h, ok := val.(types.Hash); ok {
			return h.String()
			//return utils.EncodeHexBinaryNumber(h.String())
		} else if s, ok := val.(string); ok {
			return s
			//return utils.EncodeHexBinaryNumber(s)
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

		//TODO: remove this
		log.Panic()
		return ""
	}
	env.Filters["time_elapsed_string"] = func(ctx stick.Context, val stick.Value, args ...stick.Value) stick.Value {
		return "TODO time_elapsed_string"
	}
	env.Filters["time_diff_string"] = func(ctx stick.Context, val stick.Value, args ...stick.Value) stick.Value {
		return "TODO time_diff_string"
	}
	env.Filters["time_elapsed_string_short"] = func(ctx stick.Context, val stick.Value, args ...stick.Value) stick.Value {
		return "TODO time_elapsed_string_short"
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
	env.Tests["defined"] = func(ctx stick.Context, val stick.Value, args ...stick.Value) bool {
		return val != nil
	}

	serveMux := mux.NewRouter()

	serveMux.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		params := request.URL.Query()
		if params.Has("refresh") {
			writer.Header().Set("refresh", "120")
		}

		poolInfo := getFromAPI("pool_info", 5)
		blocks := getFromAPI("found_blocks?coinbase&limit=20", 5)
		shares := getFromAPI("shares?limit=20", 5)

		ctx := make(map[string]stick.Value)
		ctx["refresh"] = writer.Header().Get("refresh")
		ctx["blocks_found"] = blocks
		ctx["shares"] = shares
		ctx["pool"] = poolInfo

		render(writer, "index.html", ctx)
	})

	serveMux.HandleFunc("/api", func(writer http.ResponseWriter, request *http.Request) {
		render(writer, "api.html", nil)
	})

	serveMux.HandleFunc("/calculate-share-time", func(writer http.ResponseWriter, request *http.Request) {
		poolInfo := getFromAPI("pool_info", 5)
		hashRate := uint64(0)
		magnitude := uint64(1000)

		params := request.URL.Query()
		if params.Has("hashrate") {
			hashRate = toUint64(params.Get("hashrate"))
		}
		if params.Has("magnitude") {
			magnitude = toUint64(params.Get("magnitude"))
		}

		ctx := make(map[string]stick.Value)
		ctx["hashrate"] = hashRate
		ctx["magnitude"] = magnitude
		ctx["pool"] = poolInfo

		render(writer, "calculate-share-time.html", ctx)
	})

	serveMux.HandleFunc("/blocks", func(writer http.ResponseWriter, request *http.Request) {
		params := request.URL.Query()
		if params.Has("refresh") {
			writer.Header().Set("refresh", "600")
		}

		poolInfo := getFromAPI("pool_info", 5)
		blocks := getFromAPI("found_blocks?coinbase&limit=100", 30)

		ctx := make(map[string]stick.Value)
		ctx["refresh"] = writer.Header().Get("refresh")
		ctx["blocks_found"] = blocks
		ctx["pool"] = poolInfo

		render(writer, "blocks.html", ctx)
	})

	serveMux.HandleFunc("/miners", func(writer http.ResponseWriter, request *http.Request) {
		params := request.URL.Query()
		if params.Has("refresh") {
			writer.Header().Set("refresh", "600")
		}

		poolInfo := getFromAPI("pool_info", 5)
		shares := getFromAPI(fmt.Sprintf("shares?limit=%d&onlyBlocks", p2pool.PPLNSWindow), 30).([]any)

		miners := make(map[string]map[string]any, 0)

		wsize := uint64(p2pool.PPLNSWindow)
		count := uint64(30)

		tipHeight := toUint64(poolInfo.(map[string]any)["sidechain"].(map[string]any)["height"])
		wend := tipHeight - p2pool.PPLNSWindow

		tip := shares[0].(map[string]any)
		for _, s := range shares {
			share := s.(map[string]any)
			miner := share["miner"].(string)
			if _, ok := miners[miner]; !ok {
				miners[miner] = make(map[string]any)
				miners[miner]["weight"] = types.Difficulty{}
				miners[miner]["shares"] = make([]uint64, count)
				miners[miner]["uncles"] = make([]uint64, count)
			}

			index := (toUint64(tip["height"]) - toUint64(share["height"])) / ((wsize + count - 1) / count)
			miners[miner]["shares"].([]uint64)[index]++
			diff := toUint64(share["weight"])
			miners[miner]["weight"] = types.Difficulty{Uint128: miners[miner]["weight"].(types.Difficulty).Add64(diff)}

			if _, ok := share["uncles"]; ok {

				for _, u := range share["uncles"].([]any) {
					uncle := u.(map[string]any)
					if toUint64(uncle["height"]) <= wend {
						continue
					}
					miner := uncle["miner"].(string)
					if _, ok := miners[miner]; !ok {
						miners[miner] = make(map[string]any)
						miners[miner]["weight"] = types.Difficulty{}
						miners[miner]["shares"] = make([]uint64, count)
						miners[miner]["uncles"] = make([]uint64, count)
					}

					index := (toUint64(tip["height"]) - toUint64(uncle["height"])) / ((wsize + count - 1) / count)
					miners[miner]["uncles"].([]uint64)[index]++
					diff := toUint64(uncle["weight"])
					miners[miner]["weight"] = types.Difficulty{Uint128: miners[miner]["weight"].(types.Difficulty).Add64(diff)}
				}
			}
		}

		minerKeys := maps.Keys(miners)
		slices.SortFunc(minerKeys, func(a string, b string) bool {
			return miners[a]["weight"].(types.Difficulty).Cmp(miners[b]["weight"].(types.Difficulty).Uint128) > 0
		})

		sortedMiners := make(mapslice.MapSlice, len(minerKeys))

		for i, k := range minerKeys {
			miner := miners[k]
			minerShares := miner["shares"].([]uint64)
			sharesPosition := make([]byte, 2*2+len(minerShares))
			sharesPosition[0], sharesPosition[1] = '[', '<'
			sharesPosition[len(sharesPosition)-2], sharesPosition[len(sharesPosition)-1] = '<', ']'
			for i, p := range utils.ReverseSlice(slices.Clone(minerShares)) {
				if p > 0 {
					if p > 9 {
						sharesPosition[2+i] = '+'
					} else {
						sharesPosition[2+i] = 0x30 + byte(p)
					}
				} else {
					sharesPosition[2+i] = '.'
				}
			}
			minerUncles := miner["uncles"].([]uint64)
			unclesPosition := make([]byte, 2*2+len(minerUncles))
			unclesPosition[0], unclesPosition[1] = '[', '<'
			unclesPosition[len(unclesPosition)-2], unclesPosition[len(unclesPosition)-1] = '<', ']'
			for i, p := range utils.ReverseSlice(slices.Clone(minerUncles)) {
				if p > 0 {
					if p > 9 {
						unclesPosition[2+i] = '+'
					} else {
						unclesPosition[2+i] = 0x30 + byte(p)
					}
				} else {
					unclesPosition[2+i] = '.'
				}
			}

			miners[k]["shares_position"] = string(sharesPosition)
			miners[k]["uncles_position"] = string(unclesPosition)

			sortedMiners[i].Key = k
			sortedMiners[i].Value = miners[k]
		}

		ctx := make(map[string]stick.Value)
		ctx["refresh"] = writer.Header().Get("refresh")
		ctx["miners"] = sortedMiners
		ctx["tip"] = tip
		ctx["pool"] = poolInfo

		render(writer, "miners.html", ctx)
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
			error["message"] = "Block Not Found"
			render(writer, "error.html", ctx)
			return
		}

		var raw *block2.Block
		if s, ok := rawBlock.([]byte); ok && rawBlock != nil {

			if buf, err := hex.DecodeString(string(s)); err == nil {

				raw, _ = block2.NewBlockFromBytes(buf)
			}
		}

		poolInfo := getFromAPI("pool_info", 5)

		ctx := make(map[string]stick.Value)
		ctx["block"] = block
		ctx["raw"] = raw
		ctx["pool"] = poolInfo

		render(writer, "share.html", ctx)
	})

	serveMux.HandleFunc("/miner/{miner:[0-9]+|4[123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz]+}", func(writer http.ResponseWriter, request *http.Request) {
		params := request.URL.Query()
		if params.Has("refresh") {
			writer.Header().Set("refresh", "300")
		}
		address := mux.Vars(request)["miner"]
		m := getFromAPI(fmt.Sprintf("miner_info/%s", address))
		miner := m.(map[string]any)
		if m == nil || miner["address"] == nil {
			ctx := make(map[string]stick.Value)
			error := make(map[string]stick.Value)
			ctx["error"] = error
			ctx["code"] = http.StatusNotFound
			error["message"] = "Address Not Found"
			error["content"] = "<div class=\"center\" style=\"text-align: center\">You need to have mined at least one share in the past. Come back later :)</div>"
			render(writer, "error.html", ctx)
			return
		}

		poolInfo := getFromAPI("pool_info", 5).(map[string]any)

		const totalWindows = 4
		wsize := uint64(p2pool.PPLNSWindow * totalWindows)

		tipHeight := toUint64(poolInfo["sidechain"].(map[string]any)["height"])

		shares := getFromAPI(fmt.Sprintf("shares_in_window/%d?from=%d&window=%d", toUint64(miner["id"]), tipHeight, wsize)).([]any)
		payouts := getFromAPI(fmt.Sprintf("payouts/%d?search_limit=10", toUint64(miner["id"]))).([]any)
		lastShares := getFromAPI(fmt.Sprintf("shares?limit=50&miner=%d", toUint64(miner["id"]))).([]any)

		count := uint64(30 * totalWindows)

		blocksFound := make([]uint64, count)
		unclesFound := make([]uint64, count)

		var sharesInWindow, unclesInWindow uint64
		var longDiff, windowDiff types.Difficulty

		wend := tipHeight - p2pool.PPLNSWindow

		for _, share := range shares {
			s := share.(map[string]any)
			if p, ok := s["parent"]; ok {
				parent := p.(map[string]any)
				index := (tipHeight - toUint64(parent["height"])) / ((wsize + count - 1) / count)
				unclesFound[utils.Min(index, count-1)]++
				if toUint64(s["height"]) > wend {
					unclesInWindow++
					windowDiff.Uint128 = windowDiff.Add64(toUint64(s["weight"]))
				}
			} else {
				index := (tipHeight - toUint64(s["height"])) / ((wsize + count - 1) / count)
				blocksFound[utils.Min(index, count-1)]++
				if toUint64(s["height"]) > wend {
					sharesInWindow++
					windowDiff.Uint128 = windowDiff.Add64(toUint64(s["weight"]))
				}
			}
			longDiff.Uint128 = longDiff.Add64(toUint64(s["weight"]))
		}

		const separatorOffset = 30 * (totalWindows - 1)
		sharesPosition := make([]byte, 1+2*2+len(blocksFound))
		sharesPosition[0], sharesPosition[1] = '[', '<'
		sharesPosition[2+separatorOffset] = '|'
		sharesPosition[len(sharesPosition)-2], sharesPosition[len(sharesPosition)-1] = '<', ']'
		for i, p := range utils.ReverseSlice(slices.Clone(blocksFound)) {
			if i >= separatorOffset {
				i++
			}
			if p > 0 {
				if p > 9 {
					sharesPosition[2+i] = '+'
				} else {
					sharesPosition[2+i] = 0x30 + byte(p)
				}
			} else {
				sharesPosition[2+i] = '.'
			}
		}

		unclesPosition := make([]byte, 1+2*2+len(unclesFound))
		unclesPosition[0], unclesPosition[1] = '[', '<'
		unclesPosition[2+separatorOffset] = '|'
		unclesPosition[len(unclesPosition)-2], unclesPosition[len(unclesPosition)-1] = '<', ']'
		for i, p := range utils.ReverseSlice(slices.Clone(unclesFound)) {
			if i >= separatorOffset {
				i++
			}
			if p > 0 {
				if p > 9 {
					unclesPosition[2+i] = '+'
				} else {
					unclesPosition[2+i] = 0x30 + byte(p)
				}
			} else {
				unclesPosition[2+i] = '.'
			}
		}

		ctx := make(map[string]stick.Value)
		ctx["refresh"] = writer.Header().Get("refresh")
		ctx["pool"] = poolInfo
		ctx["miner"] = miner
		ctx["last_shares"] = lastShares
		ctx["last_payouts"] = payouts
		ctx["window_weight"] = windowDiff.Lo
		ctx["weight"] = longDiff.Lo
		ctx["window_count_blocks"] = sharesInWindow
		ctx["window_count_uncles"] = unclesInWindow
		ctx["count_blocks"] = func() (result uint64) {
			for _, n := range blocksFound {
				result += n
			}
			return
		}()
		ctx["count_uncles"] = func() (result uint64) {
			for _, n := range unclesFound {
				result += n
			}
			return
		}()
		ctx["position_blocks"] = string(sharesPosition)
		ctx["position_uncles"] = string(unclesPosition)
		render(writer, "miner.html", ctx)
	})

	serveMux.HandleFunc("/miner", func(writer http.ResponseWriter, request *http.Request) {
		params := request.URL.Query()
		if params.Get("address") == "" {
			http.Redirect(writer, request, "/", http.StatusMovedPermanently)
			return
		}
		http.Redirect(writer, request, fmt.Sprintf("/miner/%s", params.Get("address")), http.StatusMovedPermanently)
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
		miner := m.(map[string]any)

		if m == nil {
			ctx := make(map[string]stick.Value)
			error := make(map[string]stick.Value)
			ctx["error"] = error
			ctx["code"] = http.StatusNotFound
			error["message"] = "Address Not Found"
			error["content"] = "<div class=\"center\" style=\"text-align: center\">You need to have mined at least one share in the past. Come back later :)</div>"
			render(writer, "error.html", ctx)
			return
		}

		payouts := getFromAPI(fmt.Sprintf("payouts/%d?search_limit=0", toUint64(miner["id"]))).([]any)
		if len(payouts) == 0 {
			ctx := make(map[string]stick.Value)
			error := make(map[string]stick.Value)
			ctx["error"] = error
			ctx["code"] = http.StatusNotFound
			error["message"] = "No payout for address found"
			error["content"] = "<div class=\"center\" style=\"text-align: center\">You need to have mined at least one share in the past, and a main block found during that period. Come back later :)</div>"
			render(writer, "error.html", ctx)
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
		render(writer, "payouts.html", ctx)
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
