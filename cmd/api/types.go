package main

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"github.com/ake-persson/mapslice-json"
)

type poolInfoResult struct {
	SideChain poolInfoResultSideChain `json:"sidechain"`
	MainChain poolInfoResultMainChain `json:"mainchain"`
}

type poolInfoResultSideChain struct {
	Id           types.Hash                    `json:"id"`
	Height       uint64                        `json:"height"`
	Difficulty   types.Difficulty              `json:"difficulty"`
	Timestamp    uint64                        `json:"timestamp"`
	Effort       poolInfoResultSideChainEffort `json:"effort"`
	Window       poolInfoResultSideChainWindow `json:"window"`
	WindowSize   int                           `json:"window_size"`
	BlockTime    int                           `json:"block_time"`
	UnclePenalty int                           `json:"uncle_penalty"`
	Found        uint64                        `json:"found"`
	Miners       uint64                        `json:"miners"`
}

type poolInfoResultSideChainEffort struct {
	Current float64           `json:"current"`
	Average float64           `json:"average"`
	Last    mapslice.MapSlice `json:"last"`
}
type poolInfoResultSideChainWindow struct {
	Miners int              `json:"miners"`
	Blocks int              `json:"blocks"`
	Uncles int              `json:"uncles"`
	Weight types.Difficulty `json:"weight"`
}

type poolInfoResultMainChain struct {
	Id         types.Hash       `json:"id"`
	Height     uint64           `json:"height"`
	Difficulty types.Difficulty `json:"difficulty"`
	BlockTime  int              `json:"block_time"`
}

type minerInfoResult struct {
	Id      uint64           `json:"id"`
	Address *address.Address `json:"address"`
	Shares  struct {
		Blocks uint64 `json:"blocks"`
		Uncles uint64 `json:"uncles"`
	} `json:"shares"`
	LastShareHeight    uint64 `json:"last_share_height"`
	LastShareTimestamp uint64 `json:"last_share_timestamp"`
}

type sharesInWindowResult struct {
	Parent    *sharesInWindowResultParent `json:"parent,omitempty"`
	Id        types.Hash                  `json:"id"`
	Height    uint64                      `json:"height"`
	Timestamp uint64                      `json:"timestamp"`
	Weight    types.Difficulty            `json:"weight"`
	Uncles    []sharesInWindowResultUncle `json:"uncles,omitempty"`
}

type sharesInWindowResultParent struct {
	Id     types.Hash `json:"id"`
	Height uint64     `json:"height"`
}

type sharesInWindowResultUncle struct {
	Id     types.Hash       `json:"id"`
	Height uint64           `json:"height"`
	Weight types.Difficulty `json:"weight"`
}
