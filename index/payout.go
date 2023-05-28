package index

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
)

type Payout struct {
	Miner             uint64                 `json:"miner_id"`
	TemplateId        types.Hash             `json:"template_id"`
	SideHeight        uint64                 `json:"side_height"`
	UncleOf           types.Hash             `json:"uncle_of"`
	MainId            types.Hash             `json:"main_id"`
	MainHeight        uint64                 `json:"main_height"`
	Timestamp         uint64                 `json:"timestamp"`
	CoinbaseId        types.Hash             `json:"coinbase_id"`
	Reward            uint64                 `json:"coinbase_reward"`
	PrivateKey        crypto.PrivateKeyBytes `json:"coinbase_private_key"`
	Index             uint64                 `json:"coinbase_output_index"`
	GlobalOutputIndex uint64                 `json:"global_output_index"`
	IncludingHeight   uint64                 `json:"including_height"`
}

func (p *Payout) ScanFromRow(i *Index, row RowScanInterface) error {
	if err := row.Scan(&p.Miner, &p.MainId, &p.MainHeight, &p.Timestamp, &p.CoinbaseId, &p.PrivateKey, &p.TemplateId, &p.SideHeight, &p.UncleOf, &p.Reward, &p.Index, &p.GlobalOutputIndex, &p.IncludingHeight); err != nil {
		return err
	}

	return nil
}
