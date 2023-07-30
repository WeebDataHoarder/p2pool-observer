package index

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
)

type FoundBlock struct {
	MainBlock MainBlock `json:"main_block"`

	SideHeight           uint64           `json:"side_height"`
	Miner                uint64           `json:"miner"`
	UncleOf              types.Hash       `json:"uncle_of,omitempty"`
	EffectiveHeight      uint64           `json:"effective_height"`
	WindowDepth          uint32           `json:"window_depth"`
	WindowOutputs        uint32           `json:"window_outputs"`
	TransactionCount     uint32           `json:"transaction_count"`
	Difficulty           uint64           `json:"difficulty"`
	CumulativeDifficulty types.Difficulty `json:"cumulative_difficulty"`
	Inclusion            BlockInclusion   `json:"inclusion"`

	// Extra information filled just for JSON purposes
	MinerAddress *address.Address `json:"miner_address,omitempty"`
	MinerAlias   string           `json:"miner_alias,omitempty"`
}

func (b *FoundBlock) ScanFromRow(i *Index, row RowScanInterface) error {
	if err := row.Scan(
		&b.MainBlock.Id, &b.MainBlock.Height, &b.MainBlock.Timestamp, &b.MainBlock.Reward, &b.MainBlock.CoinbaseId, &b.MainBlock.CoinbasePrivateKey, &b.MainBlock.Difficulty, &b.MainBlock.SideTemplateId,
		&b.SideHeight, &b.Miner, &b.UncleOf, &b.EffectiveHeight, &b.WindowDepth, &b.WindowOutputs, &b.TransactionCount, &b.Difficulty, &b.CumulativeDifficulty, &b.Inclusion,
	); err != nil {
		return err
	}
	return nil
}
