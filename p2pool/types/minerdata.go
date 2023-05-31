package types

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/mempool"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"time"
)

type MinerData struct {
	MajorVersion          uint8            `json:"major_version"`
	Height                uint64           `json:"height"`
	PrevId                types.Hash       `json:"prev_id"`
	SeedHash              types.Hash       `json:"seed_hash"`
	Difficulty            types.Difficulty `json:"difficulty"`
	MedianWeight          uint64           `json:"median_weight"`
	AlreadyGeneratedCoins uint64           `json:"already_generated_coins"`
	MedianTimestamp       uint64           `json:"median_timestamp"`
	TimeReceived          time.Time        `json:"time_received"`
	TxBacklog             mempool.Mempool  `json:"tx_backlog"`
}
