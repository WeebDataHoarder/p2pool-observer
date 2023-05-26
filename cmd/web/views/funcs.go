package views

import (
	"encoding/binary"
	hex2 "encoding/hex"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/index"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	types2 "git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"golang.org/x/exp/constraints"
	"log"
	"strconv"
	"strings"
	"time"
)

func utc_date[T constraints.Integer | constraints.Float](v T) string {
	return time.Unix(int64(v), 0).UTC().Format("02-01-2006 15:04:05 MST")
}

func time_elapsed_short[T constraints.Integer | constraints.Float](v T) string {
	diff := time.Since(time.Unix(int64(v), 0).UTC())

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

	if v == 0 {
		return "never"
	} else if len(result) == 0 {
		return "just now"
	} else {
		return strings.Join(result[0:1], " ") + " ago"
	}
}

func shorten(val any, n int) string {
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

	return utils.Shorten(value, n)
}

func software_info(softwareId types2.SoftwareId, softwareVersion types2.SoftwareVersion) string {
	if softwareId == 0 && softwareVersion == 0 {
		return "Not present"
	}
	return fmt.Sprintf("%s %s", softwareId.String(), softwareVersion.String())
}

func side_block_weight(s *index.SideBlock, tipHeight, windowSize uint64, consensus *sidechain.Consensus) uint64 {
	w, _ := s.Weight(tipHeight, windowSize, consensus.UnclePenalty)
	return w
}

func side_block_valuation(b any, consensus *sidechain.Consensus) string {
	if sideBlock, ok := b.(*index.SideBlock); ok {
		if sideBlock.IsOrphan() {
			return "0%"
		} else if sideBlock.IsUncle() {
			return fmt.Sprintf("%d%% (uncle)", 100-consensus.UnclePenalty)
		} else if len(sideBlock.Uncles) > 0 {
			return fmt.Sprintf("100%% + %d%% of %d uncle(s)", consensus.UnclePenalty, len(sideBlock.Uncles))
		} else {
			return "100%"
		}
	} else if poolBlock, ok := b.(*sidechain.PoolBlock); ok {
		if len(poolBlock.Side.Uncles) > 0 {
			return fmt.Sprintf("100%% + %d%% of %d uncle(s)", consensus.UnclePenalty, len(poolBlock.Side.Uncles))
		} else {
			return "100%"
		}
	} else {
		return ""
	}
}

func si_units[T constraints.Integer | constraints.Float](v T, n ...int) string {
	if len(n) > 0 {
		return utils.SiUnits(float64(v), n[0])
	} else {
		return utils.SiUnits(float64(v), 3)
	}
}
func date_diff_short[T constraints.Integer | constraints.Float](v T) string {
	diff := time.Since(time.Unix(int64(v), 0).UTC())
	s := fmt.Sprintf("%02d:%02d:%02d", int64(diff.Hours())%24, int64(diff.Minutes())%60, int64(diff.Seconds())%60)

	days := int64(diff.Hours() / 24)
	if days > 0 {
		return strconv.FormatInt(days, 10) + ":" + s
	}

	return s
}

func time_duration_long[T constraints.Integer | constraints.Float](v T) string {
	diff := time.Second * time.Duration(v)
	diff += time.Microsecond * time.Duration((float64(uint64(v))-float64(v))*1000000)
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

func benc(n uint64) string {
	return utils.EncodeBinaryNumber(n)
}

func bencstr(val string) string {
	if n, err := strconv.ParseUint(val, 10, 0); err == nil {
		return utils.EncodeBinaryNumber(n)
	} else {
		panic(err)
	}
}

func henc(val any) string {
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

type a index.Payout

func str(val any) string {
	if strVal, ok := val.(fmt.Stringer); ok {
		return strVal.String()
	}
	switch val.(type) {
	case float32:
		return strconv.FormatFloat(float64(val.(float32)), 'f', -1, 64)
	case float64:
		return strconv.FormatFloat(val.(float64), 'f', -1, 64)
	case uint32:
		return strconv.FormatFloat(float64(val.(uint32)), 'f', -1, 64)
	case uint64:
		return strconv.FormatFloat(float64(val.(uint64)), 'f', -1, 64)
	case int:
		return strconv.FormatFloat(float64(val.(int)), 'f', -1, 64)
	default:
		return fmt.Sprintf("%s", val)
	}
}

func found_block_effort(b, previous *index.FoundBlock) float64 {
	if previous == nil {
		return 0
	}

	return float64(b.CumulativeDifficulty.SubWrap(previous.CumulativeDifficulty).Mul64(100).Lo) / float64(b.MainBlock.Difficulty)
}

func effort_color(effort float64) string {
	if effort < 100 {
		return "#00C000"
	} else if effort < 200 {
		return "#E0E000"
	} else {
		return "#FF0000"
	}
}

func monero_to_xmr(v uint64) string {
	return utils.XMRUnits(v)
}

func diff_hashrate(v any, blockTime uint64) uint64 {
	if strVal, ok := v.(string); ok {
		if d, err := types.DifficultyFromString(strVal); err == nil {
			return d.Div64(blockTime).Lo
		}
	} else if d, ok := v.(types.Difficulty); ok {
		return d.Div64(blockTime).Lo
	}
	return 0
}

func coinbase_extra(b *sidechain.PoolBlock) string {
	coinbaseExtra := b.Main.Coinbase.Extra
	var result []string
	for _, t := range coinbaseExtra {
		buf, _ := t.MarshalBinary()
		result = append(result, hex2.EncodeToString(buf))
	}
	return strings.Join(result, " ")
}

func hex(val any) string {
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
		return hex2.EncodeToString(s)
	} else if s, ok := val.(uint32); ok {
		var buf [4]byte
		binary.LittleEndian.PutUint32(buf[:], s)
		return hex2.EncodeToString(buf[:])
	} else if s, ok := val.(uint64); ok {
		var buf [8]byte
		binary.LittleEndian.PutUint64(buf[:], s)
		return hex2.EncodeToString(buf[:])
	}

	return fmt.Sprintf("%s", val)
}
