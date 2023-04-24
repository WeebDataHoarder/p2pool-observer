package utils

import "git.gammaspectra.live/P2Pool/p2pool-observer/index"

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
