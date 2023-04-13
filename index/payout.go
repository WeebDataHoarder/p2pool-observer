package index

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
)

type Payout struct {
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
}

func (p *Payout) ScanFromRow(i *Index, row RowScanInterface) error {
	if err := row.Scan(&p.MainId, &p.MainHeight, &p.Timestamp, &p.CoinbaseId, &p.PrivateKey, &p.TemplateId, &p.SideHeight, &p.UncleOf, &p.Reward, &p.Index, &p.GlobalOutputIndex); err != nil {
		return err
	}

	return nil
}
