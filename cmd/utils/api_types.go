package utils

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/cmd/index"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	types2 "git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"strings"
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

func (v VersionInfo) ShortVersion() types2.SemanticVersion {
	parts := strings.Split(v.Version, ".")
	for len(parts) < 2 {
		parts = append(parts, "0")
	}
	for len(parts) > 2 {
		parts = parts[:len(parts)-1]
	}
	return types2.SemanticVersionFromString(strings.Join(parts, "."))
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
	// Consensus Specifies the consensus parameters for the backing p2pool instance
	Consensus *sidechain.Consensus `json:"consensus"`

	// LastBlock Last sidechain block on database
	LastBlock *index.SideBlock `json:"last_block"`

	// SecondsSinceLastBlock
	// Prefer using max(0, time.Now().Unix()-int64(LastBlock .Timestamp)) instead
	SecondsSinceLastBlock int64 `json:"seconds_since_last_block"`

	// LastFound Last sidechain block on database found and accepted on Monero network
	LastFound *index.FoundBlock `json:"last_found"`

	Effort PoolInfoResultSideChainEffort `json:"effort"`
	Window PoolInfoResultSideChainWindow `json:"window"`

	// Found Total count of found blocks in database
	Found uint64 `json:"found"`
	// Miners Total count of miners in database
	Miners uint64 `json:"miners"`

	// Id Available on LastBlock .TemplateId
	// Deprecated
	Id types.Hash `json:"id"`

	// Height Available on LastBlock .SideHeight
	// Deprecated
	Height uint64 `json:"height"`

	// Version Available via sidechain.P2PoolShareVersion
	// Deprecated
	Version sidechain.ShareVersion `json:"version"`

	// Difficulty Available on LastBlock .Difficulty
	// Deprecated
	Difficulty types.Difficulty `json:"difficulty"`

	// CumulativeDifficulty Available on LastBlock .CumulativeDifficulty
	// Deprecated
	CumulativeDifficulty types.Difficulty `json:"cumulative_difficulty"`

	// Timestamp Available on LastBlock .Timestamp
	// Deprecated
	Timestamp uint64 `json:"timestamp"`

	// WindowSize Available on Window .Blocks
	// Deprecated
	WindowSize int `json:"window_size"`

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

type PoolInfoResultSideChainEffortLastEntry struct {
	Id     types.Hash `json:"id"`
	Effort float64    `json:"effort"`
}

type PoolInfoResultSideChainEffort struct {
	Current    float64                                  `json:"current"`
	Average10  float64                                  `json:"average10"`
	Average50  float64                                  `json:"average"`
	Average200 float64                                  `json:"average200"`
	Last       []PoolInfoResultSideChainEffortLastEntry `json:"last"`
}
type PoolInfoResultSideChainWindow struct {
	// Miners Unique miners found in window
	Miners int `json:"miners"`
	// Blocks total count of blocks in the window, including uncles
	Blocks int `json:"blocks"`
	Uncles int `json:"uncles"`

	// Top TemplateId of the window tip
	Top types.Hash `json:"top"`
	// Bottom TemplateId of the last non-uncle block in the window
	Bottom types.Hash `json:"bottom"`

	// BottomUncles TemplateId of the uncles included under the last block, if any
	BottomUncles []types.Hash `json:"bottom_uncles,omitempty"`

	Weight   types.Difficulty        `json:"weight"`
	Versions []SideChainVersionEntry `json:"versions"`
}

type PoolInfoResultMainChainConsensus struct {
	BlockTime             uint64 `json:"block_time"`
	TransactionUnlockTime uint64 `json:"transaction_unlock_time"`
	MinerRewardUnlockTime uint64 `json:"miner_reward_unlock_time"`

	// HardForkSupportedVersion
	HardForkSupportedVersion uint8 `json:"hard_fork_supported_version"`
	// HardForks HardFork information for Monero known hardfork by backing p2pool
	HardForks []sidechain.HardFork `json:"hard_forks,omitempty"`
}

type PoolInfoResultMainChain struct {
	Consensus  PoolInfoResultMainChainConsensus `json:"consensus"`
	Id         types.Hash                       `json:"id"`
	CoinbaseId types.Hash                       `json:"coinbase_id"`
	Height     uint64                           `json:"height"`
	Difficulty types.Difficulty                 `json:"difficulty"`
	Reward     uint64                           `json:"reward"`
	BaseReward uint64                           `json:"base_reward"`

	NextDifficulty types.Difficulty `json:"next_difficulty"`

	// BlockTime included in Consensus
	// Deprecated
	BlockTime int `json:"block_time"`
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

type TransactionLookupResult struct {
	Id     types.Hash                                `json:"id"`
	Inputs index.TransactionInputQueryResults        `json:"inputs"`
	Outs   []client.Output                           `json:"outs"`
	Match  []index.TransactionInputQueryResultsMatch `json:"matches"`
}
