package utils

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/index"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	types2 "git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"github.com/ake-persson/mapslice-json"
	"time"
)

const (
	JSONEventSideBlock     = "side_block"
	JSONEventFoundBlock    = "found_block"
	JSONEventOrphanedBlock = "orphaned_block"
)

type JSONEvent struct {
	Type                string                    `json:"type"`
	SideBlock           *index.SideBlock          `json:"side_block,omitempty"`
	FoundBlock          *index.FoundBlock         `json:"found_block,omitempty"`
	MainCoinbaseOutputs index.MainCoinbaseOutputs `json:"main_coinbase_outputs,omitempty"`
}

type VersionInfo struct {
	Version          string `json:"version"`
	Timestamp        int64  `json:"timestamp"`
	Link             string `json:"link"`
	CheckedTimestamp int64  `json:"-"`
}

type ReleaseDataJson struct {
	TagName         string    `json:"tag_name"`
	TargetCommitish string    `json:"target_commitish"`
	Name            string    `json:"name"`
	PublishedAt     time.Time `json:"published_at"`
}

type SideChainVersionEntry struct {
	Weight          types.Difficulty       `json:"weight"`
	Share           float64                `json:"share"`
	Count           int                    `json:"count"`
	SoftwareId      types2.SoftwareId      `json:"software_id"`
	SoftwareVersion types2.SoftwareVersion `json:"software_version"`
	SoftwareString  string                 `json:"software_string"`
}

type PoolInfoResult struct {
	SideChain PoolInfoResultSideChain `json:"sidechain"`
	MainChain PoolInfoResultMainChain `json:"mainchain"`
	Versions  struct {
		P2Pool VersionInfo `json:"p2pool"`
		Monero VersionInfo `json:"monero"`
	} `json:"versions"`
}

type PoolInfoResultSideChain struct {
	Consensus             *sidechain.Consensus          `json:"consensus"`
	Id                    types.Hash                    `json:"id"`
	Height                uint64                        `json:"height"`
	Version               sidechain.ShareVersion        `json:"version"`
	Difficulty            types.Difficulty              `json:"difficulty"`
	CumulativeDifficulty  types.Difficulty              `json:"cumulative_difficulty"`
	SecondsSinceLastBlock int64                         `json:"seconds_since_last_block"`
	Timestamp             uint64                        `json:"timestamp"`
	Effort                PoolInfoResultSideChainEffort `json:"effort"`
	Window                PoolInfoResultSideChainWindow `json:"window"`
	WindowSize            int                           `json:"window_size"`
	Found                 uint64                        `json:"found"`
	Miners                uint64                        `json:"miners"`

	// MaxWindowSize Available on Consensus
	// Deprecated
	MaxWindowSize int `json:"max_window_size"`

	// BlockTime Available on Consensus
	// Deprecated
	BlockTime int `json:"block_time"`

	// UnclePenalty Available on Consensus
	// Deprecated
	UnclePenalty int `json:"uncle_penalty"`
}

type PoolInfoResultSideChainEffort struct {
	Current    float64           `json:"current"`
	Average10  float64           `json:"average10"`
	Average50  float64           `json:"average"`
	Average200 float64           `json:"average200"`
	Last       mapslice.MapSlice `json:"last"`
}
type PoolInfoResultSideChainWindow struct {
	Miners   int                     `json:"miners"`
	Blocks   int                     `json:"blocks"`
	Uncles   int                     `json:"uncles"`
	Weight   types.Difficulty        `json:"weight"`
	Versions []SideChainVersionEntry `json:"versions"`
}

type PoolInfoResultMainChain struct {
	Id             types.Hash       `json:"id"`
	Height         uint64           `json:"height"`
	Difficulty     types.Difficulty `json:"difficulty"`
	Reward         uint64           `json:"reward"`
	BaseReward     uint64           `json:"base_reward"`
	NextDifficulty types.Difficulty `json:"next_difficulty"`
	BlockTime      int              `json:"block_time"`
}

type MinerInfoBlockData struct {
	ShareCount      uint64 `json:"shares"`
	UncleCount      uint64 `json:"uncles"`
	LastShareHeight uint64 `json:"last_height"`
}

type MinerInfoResult struct {
	Id                 uint64                                   `json:"id"`
	Address            *address.Address                         `json:"address"`
	Alias              string                                   `json:"alias,omitempty"`
	Shares             [index.InclusionCount]MinerInfoBlockData `json:"shares"`
	LastShareHeight    uint64                                   `json:"last_share_height"`
	LastShareTimestamp uint64                                   `json:"last_share_timestamp"`
}
