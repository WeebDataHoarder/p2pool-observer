package types

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"time"
)

type MinerData struct {
	MajorVersion          uint8
	Height                uint64
	PrevId                types.Hash
	SeedHash              types.Hash
	Difficulty            types.Difficulty
	MedianWeight          uint64
	AlreadyGeneratedCoins uint64
	MedianTimestamp       uint64
	//TxBacklog any
	TimeReceived time.Time
}
