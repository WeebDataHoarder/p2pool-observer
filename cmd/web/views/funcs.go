package views

import (
	hex2 "encoding/hex"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/cmd/index"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"github.com/mazznoer/colorgrad"
	"strconv"
	"strings"
	"time"
)

func utc_date[T int64 | uint64 | int | float64](v T) string {
	return time.Unix(int64(v), 0).UTC().Format("02-01-2006 15:04:05 MST")
}

func time_elapsed_short[T int64 | uint64 | int | float64](v T) string {
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

func side_block_weight(s *index.SideBlock, tipHeight, windowSize uint64, consensus *sidechain.Consensus) uint64 {
	w, _ := s.Weight(tipHeight, windowSize, consensus.UnclePenalty)
	return w
}

func si_units[T int64 | uint64 | int | float64](v T, n ...int) string {
	if len(n) > 0 {
		return utils.SiUnits(float64(v), n[0])
	} else {
		return utils.SiUnits(float64(v), 3)
	}
}
func date_diff_short[T int64 | uint64 | int | float64](v T) string {
	diff := time.Since(time.Unix(int64(v), 0).UTC())
	s := fmt.Sprintf("%02d:%02d:%02d", int64(diff.Hours())%24, int64(diff.Minutes())%60, int64(diff.Seconds())%60)

	days := int64(diff.Hours() / 24)
	if days > 0 {
		return strconv.FormatInt(days, 10) + ":" + s
	}

	return s
}

func time_duration_long[T int64 | uint64 | int | float64](v T) string {
	diff := time.Second * time.Duration(v)
	diff += time.Microsecond * time.Duration((float64(v)-float64(int64(v)))*1000000)
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

func str(val any) string {
	switch t := val.(type) {
	case float32:
		return strconv.FormatFloat(float64(t), 'f', -1, 64)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case uint32:
		return strconv.FormatFloat(float64(t), 'f', -1, 64)
	case uint64:
		return strconv.FormatFloat(float64(t), 'f', -1, 64)
	case int:
		return strconv.FormatFloat(float64(t), 'f', -1, 64)
	case fmt.Stringer:
		return t.String()
	default:
		return fmt.Sprintf("%v", val)
	}
}

func found_block_effort(b, previous *index.FoundBlock) float64 {
	if previous == nil {
		return 0
	}

	return float64(b.CumulativeDifficulty.Sub(previous.CumulativeDifficulty).Mul64(100).Lo) / float64(b.MainBlock.Difficulty)
}

var effortColorGradient = colorgrad.RdYlBu()

const effortRangeStart = 0.15
const effortRangeEnd = 0.85

func effort_color(effort float64) string {
	probability := utils.ProbabilityEffort(effort)

	// rescale
	probability *= effortRangeEnd - effortRangeStart
	probability += effortRangeStart

	return effortColorGradient.At(1 - probability).Hex()
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
	} else if d, ok := v.(uint64); ok {
		return d / blockTime
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
