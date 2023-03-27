package index

import (
	"encoding/json"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
)

const MainBlockSelectFields = "id, height, timestamp, reward, coinbase_id, difficulty, metadata, side_template_id, coinbase_private_key"

type MainBlock struct {
	Id         types.Hash
	Height     uint64
	Timestamp  uint64
	Reward     uint64
	CoinbaseId types.Hash
	Difficulty uint64

	// Metadata should be jsonb blob, can be NULL. metadata such as pool ownership, links to other p2pool networks, and other interesting data
	Metadata map[string]any

	// sidechain data for blocks we own
	// SideTemplateId can be NULL
	SideTemplateId types.Hash
	// CoinbasePrivateKey private key for coinbase outputs we own (all 0x00 = not known, but should have one)
	CoinbasePrivateKey crypto.PrivateKeyBytes
}

func (b *MainBlock) GetMetadata(key string) any {
	return b.Metadata[key]
}

func (b *MainBlock) SetMetadata(key string, v any) {
	b.Metadata[key] = v
}

func (b *MainBlock) ScanFromRow(i *Index, row RowScanInterface) error {
	var metadataBuf []byte
	b.Metadata = make(map[string]any)
	if err := row.Scan(&b.Id, &b.Height, &b.Timestamp, &b.Reward, &b.CoinbaseId, &b.Difficulty, &metadataBuf, &b.SideTemplateId, &b.CoinbasePrivateKey); err != nil {
		return err
	} else if err = json.Unmarshal(metadataBuf, &b.Metadata); err != nil {
		return err
	}
	return nil
}
