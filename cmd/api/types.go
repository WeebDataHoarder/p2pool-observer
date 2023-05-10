package main

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/index"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	types2 "git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"github.com/ake-persson/mapslice-json"
)

type sideChainVersionEntry struct {
	Weight          types.Difficulty       `json:"weight"`
	Share           float64                `json:"share"`
	Count           int                    `json:"count"`
	SoftwareId      types2.SoftwareId      `json:"software_id"`
	SoftwareVersion types2.SoftwareVersion `json:"software_version"`
	SoftwareString  string                 `json:"software_string"`
}

type poolInfoResult struct {
	SideChain poolInfoResultSideChain `json:"sidechain"`
	MainChain poolInfoResultMainChain `json:"mainchain"`
	Versions  struct {
		P2Pool versionInfo `json:"p2pool"`
		Monero versionInfo `json:"monero"`
	} `json:"versions"`
}

type poolInfoResultSideChain struct {
	Consensus             *sidechain.Consensus          `json:"consensus"`
	Id                    types.Hash                    `json:"id"`
	Height                uint64                        `json:"height"`
	Difficulty            types.Difficulty              `json:"difficulty"`
	CumulativeDifficulty  types.Difficulty              `json:"cumulative_difficulty"`
	SecondsSinceLastBlock int64                         `json:"seconds_since_last_block"`
	Timestamp             uint64                        `json:"timestamp"`
	Effort                poolInfoResultSideChainEffort `json:"effort"`
	Window                poolInfoResultSideChainWindow `json:"window"`
	WindowSize            int                           `json:"window_size"`
	MaxWindowSize         int                           `json:"max_window_size"`
	BlockTime             int                           `json:"block_time"`
	UnclePenalty          int                           `json:"uncle_penalty"`
	Found                 uint64                        `json:"found"`
	Miners                uint64                        `json:"miners"`
}

type poolInfoResultSideChainEffort struct {
	Current    float64           `json:"current"`
	Average10  float64           `json:"average10"`
	Average50  float64           `json:"average"`
	Average200 float64           `json:"average200"`
	Last       mapslice.MapSlice `json:"last"`
}
type poolInfoResultSideChainWindow struct {
	Miners   int                     `json:"miners"`
	Blocks   int                     `json:"blocks"`
	Uncles   int                     `json:"uncles"`
	Weight   types.Difficulty        `json:"weight"`
	Versions []sideChainVersionEntry `json:"versions"`
}

type poolInfoResultMainChain struct {
	Id             types.Hash       `json:"id"`
	Height         uint64           `json:"height"`
	Difficulty     types.Difficulty `json:"difficulty"`
	NextDifficulty types.Difficulty `json:"next_difficulty"`
	BlockTime      int              `json:"block_time"`
}

type minerInfoBlockData struct {
	ShareCount      uint64 `json:"shares"`
	UncleCount      uint64 `json:"uncles"`
	LastShareHeight uint64 `json:"last_height"`
}

type minerInfoResult struct {
	Id                 uint64                                                          `json:"id"`
	Address            *address.Address                                                `json:"address"`
	Alias              string                                                          `json:"alias,omitempty"`
	Shares             [index.InclusionAlternateInVerifiedChain + 1]minerInfoBlockData `json:"shares"`
	LastShareHeight    uint64                                                          `json:"last_share_height"`
	LastShareTimestamp uint64                                                          `json:"last_share_timestamp"`
}
