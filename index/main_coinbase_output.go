package index

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
)

type MainCoinbaseOutputs []MainCoinbaseOutput

const MainCoinbaseOutputSelectFields = "id, index, global_output_index, miner, value"

type MainCoinbaseOutput struct {
	// Id coinbase id
	Id types.Hash
	// Index transaction output index
	Index uint32
	// Monero global output idx
	GlobalOutputIndex uint64

	// Miner owner of the output
	Miner uint64

	Value uint64
}

func (o *MainCoinbaseOutput) ScanFromRow(i *Index, row RowScanInterface) error {
	if err := row.Scan(&o.Id, &o.Index, &o.GlobalOutputIndex, &o.Miner, &o.Value); err != nil {
		return err
	}
	return nil
}
