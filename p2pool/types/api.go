package types

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
)

type P2PoolBinaryBlockResult struct {
	Version int    `json:"version"`
	Blob    string `json:"blob"`
	Error   string `json:"error,omitempty"`
}

type P2PoolSideChainStatusResult struct {
	Synchronized          bool             `json:"synchronized"`
	Height                uint64           `json:"tip_height"`
	Id                    types.Hash       `json:"tip_id"`
	Difficulty            types.Difficulty `json:"difficulty"`
	CummulativeDifficulty types.Difficulty `json:"cummulative_difficulty"`
	Blocks                int              `json:"blocks"`
}

type P2PoolServerStatusResult struct {
	PeerId          uint64 `json:"peer_id"`
	SoftwareId      string `json:"software_id"`
	SoftwareVersion string `json:"software_version"`
	ProtocolVersion string `json:"protocol_version"`
	ListenPort      uint16 `json:"listen_port"`
}

type P2PoolServerPeerResult struct {
	PeerId          uint64 `json:"peer_id"`
	Incoming        bool   `json:"incoming"`
	Address         string `json:"address"`
	SoftwareId      string `json:"software_id"`
	SoftwareVersion string `json:"software_version"`
	ProtocolVersion string `json:"protocol_version"`
	ConnectionTime  uint64 `json:"connection_time"`
	ListenPort      uint32 `json:"listen_port"`
	Latency         uint64 `json:"latency"`
}
