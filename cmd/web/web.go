package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	utils2 "git.gammaspectra.live/P2Pool/p2pool-observer/cmd/utils"
	"git.gammaspectra.live/P2Pool/p2pool-observer/index"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero"
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
	"io"
	"log"
	"math"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"reflect"
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

func toString(t any) string {

	if s, ok := t.(string); ok {
		return s
	} else if h, ok := t.(types.Hash); ok {
		return h.String()
	}

	return ""
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

	var basePoolInfo *utils2.PoolInfoResult

	for {
		t := getTypeFromAPI[utils2.PoolInfoResult]("pool_info")
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

	consensusData, _ := json.Marshal(basePoolInfo.SideChain.Consensus)
	consensus, err := sidechain.NewConsensusFromJSON(consensusData)
	if err != nil {
		log.Panic(err)
	}

	log.Printf("Consensus id = %s", consensus.Id())

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
				e["is_onion"] = request.Host == torHost
				e["consensus"] = consensus
				e["irc_link"] = ircLink
				e["irc_link_title"] = ircLinkTitle
				e["matrix_link"] = matrixLink
				e["webchat_link"] = webchatLink
				e["donation_address"] = types.DonationAddress
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
		ctx["irc_link"] = ircLink
		ctx["irc_link_title"] = ircLinkTitle
		ctx["matrix_link"] = matrixLink
		ctx["webchat_link"] = webchatLink
		ctx["donation_address"] = types.DonationAddress

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
		if strVal, ok := args[0].(string); ok {
			if d, err := types.DifficultyFromString(strVal); err == nil {
				return d.Div64(toUint64(args[1])).Lo
			}
		} else if d, ok := args[0].(types.Difficulty); ok {
			return d.Div64(toUint64(args[1])).Lo
		}
		return 0
	}
	env.Functions["diff_uint"] = func(ctx stick.Context, args ...stick.Value) stick.Value {
		if strVal, ok := args[0].(string); ok {
			if d, err := types.DifficultyFromString(strVal); err == nil {
				return d.Lo
			}
		} else if d, ok := args[0].(types.Difficulty); ok {
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

	env.Functions["is_nil"] = func(ctx stick.Context, args ...stick.Value) stick.Value {
		if len(args) != 1 {
			return false
		}
		return args[0] == nil || (reflect.ValueOf(args[0]).Kind() == reflect.Pointer && reflect.ValueOf(args[0]).IsNil())
	}

	env.Functions["get_slice_index"] = func(ctx stick.Context, args ...stick.Value) stick.Value {
		if len(args) != 2 {
			return nil
		}
		if reflect.ValueOf(args[0]).Kind() == reflect.Slice {
			i := int(toUint64(args[1]))
			slice := reflect.ValueOf(args[0])
			if i < 0 || slice.Len() <= i {
				return nil
			}
			return slice.Index(i).Interface()
		}
		return nil
	}

	env.Functions["is_zero_hash"] = func(ctx stick.Context, args ...stick.Value) stick.Value {
		if len(args) != 1 {
			return nil
		}

		if s, ok := args[0].(string); ok {
			return s == types.ZeroHash.String()
		} else if h, ok := args[0].(types.Hash); ok {
			return h == types.ZeroHash
		}

		return false
	}

	env.Functions["prove_output_number"] = func(ctx stick.Context, args ...stick.Value) stick.Value {
		if len(args) != 2 {
			return nil
		}

		n := uint64(math.Ceil(math.Log2(float64(consensus.ChainWindowSize * 4))))

		//height | index

		return (toUint64(args[0]) << n) | toUint64(args[1])
	}

	env.Functions["get_tx_proof_v2"] = func(ctx stick.Context, args ...stick.Value) stick.Value {
		if len(args) != 3 {
			return nil
		}
		keyBytes := args[2].(crypto.PrivateKeyBytes)
		return address2.GetTxProofV2(address2.FromBase58(args[0].(string)), args[1].(types.Hash), &keyBytes, "")
	}

	env.Functions["get_tx_proof_v1"] = func(ctx stick.Context, args ...stick.Value) stick.Value {
		if len(args) != 3 {
			return nil
		}

		keyBytes := args[2].(crypto.PrivateKeyBytes)
		return address2.GetTxProofV1(address2.FromBase58(args[0].(string)), args[1].(types.Hash), &keyBytes, "")
	}

	env.Functions["get_ephemeral_pubkey"] = func(ctx stick.Context, args ...stick.Value) stick.Value {
		if len(args) != 3 {
			return nil
		}
		keyBytes := args[1].(crypto.PrivateKeyBytes)
		return address2.GetEphemeralPublicKey(address2.FromBase58(args[0].(string)), &keyBytes, toUint64(args[2]))
	}

	env.Functions["coinbase_extra"] = func(ctx stick.Context, args ...stick.Value) stick.Value {
		if len(args) != 1 {
			return nil
		}
		coinbaseExtra := args[0].(*sidechain.PoolBlock).Main.Coinbase.Extra
		var result []string
		for _, t := range coinbaseExtra {
			buf, _ := t.MarshalBinary()
			result = append(result, hex.EncodeToString(buf))
		}
		return strings.Join(result, " ")
	}

	env.Functions["block_address"] = func(ctx stick.Context, args ...stick.Value) stick.Value {
		if len(args) != 1 {
			return nil
		}
		return args[0].(*sidechain.PoolBlock).GetAddress().Reference().ToAddress(consensus.NetworkType.AddressNetwork()).ToBase58()
	}

	env.Functions["extra_nonce"] = func(ctx stick.Context, args ...stick.Value) stick.Value {
		if len(args) != 1 {
			return nil
		}
		return binary.LittleEndian.Uint32(args[0].(*sidechain.PoolBlock).CoinbaseExtra(sidechain.SideExtraNonce))
	}

	env.Functions["software_info"] = func(ctx stick.Context, args ...stick.Value) stick.Value {
		if len(args) != 2 {
			return nil
		}

		if toUint64(args[0]) == 0 && toUint64(args[1]) == 0 {
			return "Not present"
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

	env.Functions["found_block_effort"] = func(ctx stick.Context, args ...stick.Value) stick.Value {
		if len(args) != 2 || args[1] == nil {
			return nil
		}

		b := args[0].(*index.FoundBlock)
		previous := args[1].(*index.FoundBlock)
		return float64(b.CumulativeDifficulty.SubWrap(previous.CumulativeDifficulty).Mul64(100).Lo) / float64(b.MainBlock.Difficulty)
	}

	env.Functions["side_block_valuation"] = func(ctx stick.Context, args ...stick.Value) stick.Value {
		if len(args) != 1 {
			return nil
		}

		if sideBlock, ok := args[0].(*index.SideBlock); ok {
			if sideBlock.IsOrphan() {
				return "0%"
			} else if sideBlock.IsUncle() {
				return fmt.Sprintf("%d%% (uncle)", 100-consensus.UnclePenalty)
			} else if len(sideBlock.Uncles) > 0 {
				return fmt.Sprintf("100%% + %d%% of %d uncle(s)", consensus.UnclePenalty, len(sideBlock.Uncles))
			} else {
				return "100%"
			}
		} else if poolBlock, ok := args[0].(*sidechain.PoolBlock); ok {
			if len(poolBlock.Side.Uncles) > 0 {
				return fmt.Sprintf("100%% + %d%% of %d uncle(s)", consensus.UnclePenalty, len(poolBlock.Side.Uncles))
			} else {
				return "100%"
			}
		} else {
			return ""
		}
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
			return m[toString(args[1])]
		} else if ms, ok := args[0].(mapslice.MapSlice); ok {
			k := toString(args[1])
			for _, e := range ms {
				if s, ok := e.Key.(string); ok && s == k {
					return e.Value
				}
			}
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
			binary.LittleEndian.PutUint32(buf[:], s)
			return hex.EncodeToString(buf[:])
		} else if s, ok := val.(uint64); ok {
			var buf [8]byte
			binary.LittleEndian.PutUint64(buf[:], s)
			return hex.EncodeToString(buf[:])
		}

		return val
	}

	env.Filters["henc"] = func(ctx stick.Context, val stick.Value, args ...stick.Value) stick.Value {

		if h, ok := val.(types.Hash); ok {
			return utils.EncodeHexBinaryNumber(h.String())
		} else if k, ok := val.(crypto.PrivateKeyBytes); ok {
			return utils.EncodeHexBinaryNumber(k.String())
		} else if s, ok := val.(string); ok {
			return utils.EncodeHexBinaryNumber(s)
		} else if h, ok := val.(types.Hash); ok {
			return utils.EncodeHexBinaryNumber(h.String())
		} else if s, ok := val.(fmt.Stringer); ok {
			return utils.EncodeHexBinaryNumber(s.String())
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
		} else if h, ok := val.(types.Hash); ok {
			value = h.String()
		} else if s, ok := val.(fmt.Stringer); ok {
			value = s.String()
		} else {
			value = fmt.Sprintf("%s", value)
		}

		return utils.Shorten(value, int(toUint64(args[0])))
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

		poolInfo := getTypeFromAPI[utils2.PoolInfoResult]("pool_info", 5)

		secondsPerBlock := float64(poolInfo.MainChain.Difficulty.Lo) / float64(poolInfo.SideChain.Difficulty.Div64(consensus.TargetBlockTime).Lo)

		blocksToFetch := uint64(math.Ceil((((time.Hour*24).Seconds()/secondsPerBlock)*2)/100) * 100)

		blocks := getSliceFromAPI[*index.FoundBlock](fmt.Sprintf("found_blocks?limit=%d", blocksToFetch), 5)
		shares := getSideBlocksFromAPI("side_blocks?limit=50", 5)

		ctx := make(map[string]stick.Value)
		ctx["refresh"] = writer.Header().Get("refresh")

		blocksFound := NewPositionChart(30*4, consensus.ChainWindowSize*4)

		tip := int64(poolInfo.SideChain.Height)
		for _, b := range blocks {
			blocksFound.Add(int(tip-int64(b.SideHeight)), 1)
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
				if buf, err := json.Marshal(checkInformation.Tip); err == nil && checkInformation.Tip != nil {
					b := sidechain.PoolBlock{}
					if json.Unmarshal(buf, &b) == nil {
						rawTip = &b
						theirTip = getTypeFromAPI[index.SideBlock](fmt.Sprintf("block_by_id/%s", types.HashFromBytes(b.CoinbaseExtra(sidechain.SideTemplateId))))
					}
				}
			}
			ctx := make(map[string]stick.Value)
			ctx["address"] = addressPort
			ctx["your_tip"] = theirTip
			ctx["your_tip_raw"] = rawTip
			ctx["our_tip"] = ourTip
			ctx["check"] = checkInformation

			render(request, writer, "connectivity-check.html", ctx)
		} else {
			ctx := make(map[string]stick.Value)
			ctx["address"] = netip.AddrPort{}

			render(request, writer, "connectivity-check.html", ctx)
		}
	})

	serveMux.HandleFunc("/transaction-lookup", func(writer http.ResponseWriter, request *http.Request) {

		params := request.URL.Query()

		var txId types.Hash
		if params.Has("txid") {
			txId, _ = types.HashFromString(params.Get("txid"))
		}

		type transactionLookupResult struct {
			Id     types.Hash                                `json:"id"`
			Inputs index.TransactionInputQueryResults        `json:"inputs"`
			Outs   []client.Output                           `json:"outs"`
			Match  []index.TransactionInputQueryResultsMatch `json:"matches"`
		}

		if txId != types.ZeroHash {
			fullResult := getTypeFromAPI[transactionLookupResult](fmt.Sprintf("transaction_lookup/%s", txId.String()))

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
				minerCoinbaseChart := NewPositionChart(170, timeScaleItems)
				minerCoinbaseChart.SetIdle('_')
				minerSweepChart := NewPositionChart(170, timeScaleItems)
				minerSweepChart.SetIdle('_')
				otherCoinbaseMinerChart := NewPositionChart(170, timeScaleItems)
				otherCoinbaseMinerChart.SetIdle('_')
				otherSweepMinerChart := NewPositionChart(170, timeScaleItems)
				otherSweepMinerChart.SetIdle('_')
				noMinerChart := NewPositionChart(170, timeScaleItems)
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
					ctx := make(map[string]stick.Value)
					ctx["txid"] = txId
					ctx["result"] = fullResult
					ctx["likely_miner"] = likelyMiner
					ctx["miner_count"] = minerCount
					ctx["no_miner_count"] = noMinerCount
					ctx["other_miner_count"] = otherMinerCount
					ctx["miner"] = topMiner
					ctx["miner_ratio"] = minerRatio * 100
					ctx["no_miner_ratio"] = noMinerRatio * 100
					ctx["other_miner_ratio"] = otherMinerRatio * 100
					ctx["top_timestamp"] = topTimestamp
					ctx["bottom_timestamp"] = bottomTimestamp
					ctx["miner_coinbase_chart"] = minerCoinbaseChart.String()
					ctx["other_miner_coinbase_chart"] = otherCoinbaseMinerChart.String()
					ctx["miner_sweep_chart"] = minerSweepChart.String()
					ctx["other_miner_sweep_chart"] = otherSweepMinerChart.String()
					ctx["no_miner_chart"] = noMinerChart.String()

					render(request, writer, "transaction-lookup.html", ctx)
					return
				}
			}
		}

		ctx := make(map[string]stick.Value)
		ctx["txid"] = txId

		render(request, writer, "transaction-lookup.html", ctx)
	})

	serveMux.HandleFunc("/sweeps", func(writer http.ResponseWriter, request *http.Request) {
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

		poolInfo := getTypeFromAPI[utils2.PoolInfoResult]("pool_info", 5)

		ctx := make(map[string]stick.Value)
		ctx["refresh"] = writer.Header().Get("refresh")
		ctx["pool"] = poolInfo

		if miner != nil {
			blocks := getSliceFromAPI[*index.MainLikelySweepTransaction](fmt.Sprintf("sweeps/%d?limit=100", toUint64(miner["id"])))
			ctx["sweeps"] = blocks
			ctx["miner"] = miner

			render(request, writer, "sweeps_miner.html", ctx)
		} else {
			blocks := getSliceFromAPI[*index.MainLikelySweepTransaction]("sweeps?limit=100", 30)
			ctx["sweeps"] = blocks

			render(request, writer, "sweeps.html", ctx)
		}
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

		poolInfo := getTypeFromAPI[utils2.PoolInfoResult]("pool_info", 5)

		ctx := make(map[string]stick.Value)
		ctx["refresh"] = writer.Header().Get("refresh")
		ctx["pool"] = poolInfo

		if miner != nil {
			blocks := getSliceFromAPI[*index.FoundBlock](fmt.Sprintf("found_blocks?&limit=100&miner=%d", toUint64(miner["id"])))
			ctx["blocks_found"] = blocks
			ctx["miner"] = miner

			render(request, writer, "blocks_miner.html", ctx)
		} else {
			blocks := getSliceFromAPI[*index.FoundBlock]("found_blocks?limit=100", 30)
			ctx["blocks_found"] = blocks

			render(request, writer, "blocks.html", ctx)
		}
	})

	serveMux.HandleFunc("/miners", func(writer http.ResponseWriter, request *http.Request) {
		params := request.URL.Query()
		if params.Has("refresh") {
			writer.Header().Set("refresh", "600")
		}

		poolInfo := getTypeFromAPI[utils2.PoolInfoResult]("pool_info", 5)

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

		miners := make(map[uint64]map[string]any, 0)

		tipHeight := poolInfo.SideChain.Height
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
			ctx := make(map[string]stick.Value)
			error := make(map[string]stick.Value)
			ctx["error"] = error
			ctx["code"] = http.StatusNotFound
			error["message"] = "Share Not Found"
			render(request, writer, "error.html", ctx)
			return
		}
		rawBlock = getFromAPIRaw(fmt.Sprintf("block_by_id/%s/raw", block.MainId))

		coinbase = getSliceFromAPI[index.MainCoinbaseOutput](fmt.Sprintf("block_by_id/%s/coinbase", block.MainId))

		poolInfo := getTypeFromAPI[utils2.PoolInfoResult]("pool_info", 5)

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
			data, _ := json.Marshal(indices)
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
							if json.Unmarshal(data, &r) == nil && len(r) == len(indices) {
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

		ctx := make(map[string]stick.Value)
		ctx["block"] = block
		ctx["raw"] = raw
		ctx["pool"] = poolInfo
		ctx["payouts"] = payouts
		ctx["coinbase"] = coinbase
		ctx["share_sweeps"] = likelySweeps
		ctx["share_sweeps_count"] = sweepsCount

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

		poolInfo := getTypeFromAPI[utils2.PoolInfoResult]("pool_info", 5)

		const totalWindows = 4
		wsize := consensus.ChainWindowSize * totalWindows

		currentWindowSize := uint64(poolInfo.SideChain.WindowSize)

		tipHeight := poolInfo.SideChain.Height

		var shares, lastShares []*index.SideBlock

		var lastFound []*index.FoundBlock
		var payouts []*index.Payout
		var sweeps []*index.MainLikelySweepTransaction
		if toUint64(miner["id"]) != 0 {
			shares = getSideBlocksFromAPI(fmt.Sprintf("side_blocks_in_window/%d?from=%d&window=%d&noMiner&noMainStatus&noUncles", toUint64(miner["id"]), tipHeight, wsize))
			payouts = getSliceFromAPI[*index.Payout](fmt.Sprintf("payouts/%d?search_limit=1000", toUint64(miner["id"])))
			lastShares = getSideBlocksFromAPI(fmt.Sprintf("side_blocks?limit=50&miner=%d", toUint64(miner["id"])))
			lastFound = getSliceFromAPI[*index.FoundBlock](fmt.Sprintf("found_blocks?limit=10&miner=%d", toUint64(miner["id"])))
			sweeps = getSliceFromAPI[*index.MainLikelySweepTransaction](fmt.Sprintf("sweeps/%d?limit=5", toUint64(miner["id"])))
		}

		sharesFound := NewPositionChart(30*totalWindows, consensus.ChainWindowSize*totalWindows)
		unclesFound := NewPositionChart(30*totalWindows, consensus.ChainWindowSize*totalWindows)

		var sharesInWindow, unclesInWindow uint64
		var longDiff, windowDiff types.Difficulty

		wend := tipHeight - currentWindowSize

		foundPayout := NewPositionChart(30*totalWindows, consensus.ChainWindowSize*totalWindows)
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

		ctx := make(map[string]stick.Value)
		ctx["refresh"] = writer.Header().Get("refresh")
		ctx["pool"] = poolInfo
		ctx["miner"] = miner
		if raw != nil {
			ctx["last_raw_share"] = raw
		}
		ctx["last_sweeps"] = sweeps
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

		totalWeight := poolInfo.SideChain.Window.Weight.Mul64(4).Mul64(uint64(poolInfo.SideChain.MaxWindowSize)).Div64(uint64(poolInfo.SideChain.WindowSize))
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

		ctx["hashrate_local"] = hashRate
		ctx["magnitude_local"] = magnitude

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
		ctx["last_shares_efforts"] = efforts

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
		requestIndex := toUint64(mux.Vars(request)["index"])

		block := getTypeFromAPI[index.SideBlock](fmt.Sprintf("block_by_id/%s", identifier))

		if block == nil || !block.MinedMainAtHeight {
			ctx := make(map[string]stick.Value)
			error := make(map[string]stick.Value)
			ctx["error"] = error
			ctx["code"] = http.StatusNotFound
			error["message"] = "Share Was Not Found"
			render(request, writer, "error.html", ctx)
			return
		}

		raw := getTypeFromAPI[sidechain.PoolBlock](fmt.Sprintf("block_by_id/%s/light", block.MainId))
		if raw == nil || raw.ShareVersion() == sidechain.ShareVersion_None {
			raw = nil
		}

		payouts := getSliceFromAPI[index.MainCoinbaseOutput](fmt.Sprintf("block_by_id/%s/coinbase", block.MainId))

		if raw == nil {
			ctx := make(map[string]stick.Value)
			error := make(map[string]stick.Value)
			ctx["error"] = error
			ctx["code"] = http.StatusNotFound
			error["message"] = "Coinbase Was Not Found"
			render(request, writer, "error.html", ctx)
			return
		}

		if uint64(len(payouts)) <= requestIndex {
			ctx := make(map[string]stick.Value)
			error := make(map[string]stick.Value)
			ctx["error"] = error
			ctx["code"] = http.StatusNotFound
			error["message"] = "Payout Was Not Found"
			render(request, writer, "error.html", ctx)
			return
		}

		poolInfo := getTypeFromAPI[utils2.PoolInfoResult]("pool_info", 5)

		ctx := make(map[string]stick.Value)
		ctx["block"] = block
		ctx["raw"] = raw
		ctx["payout"] = payouts[requestIndex]
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

		payouts := getSliceFromAPI[*index.Payout](fmt.Sprintf("payouts/%d?search_limit=0", toUint64(miner["id"])))
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
				result += p.Reward
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
