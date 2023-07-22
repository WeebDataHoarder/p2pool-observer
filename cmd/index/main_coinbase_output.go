package index

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
)

type MainCoinbaseOutputs []MainCoinbaseOutput

const MainCoinbaseOutputSelectFields = "id, index, global_output_index, miner, value"

type MainCoinbaseOutput struct {
	// Id coinbase id
	Id types.Hash `json:"id"`
	// Index transaction output index
	Index uint32 `json:"index"`
	// Monero global output idx
	GlobalOutputIndex uint64 `json:"global_output_index"`

	// Miner owner of the output
	Miner uint64 `json:"miner"`

	Value uint64 `json:"value"`

	// Extra information filled just for JSON purposes

	MinerAddress *address.Address `json:"miner_address,omitempty"`
	MinerAlias   string           `json:"miner_alias,omitempty"`
}

func (o *MainCoinbaseOutput) ScanFromRow(i *Index, row RowScanInterface) error {
	if err := row.Scan(&o.Id, &o.Index, &o.GlobalOutputIndex, &o.Miner, &o.Value); err != nil {
		return err
	}
	return nil
}
