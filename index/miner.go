package index

import (
	"database/sql"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
)

const MinerSelectFields = "id, alias, spend_public_key, view_public_key"

type Miner struct {
	id    uint64
	addr  address.Address
	alias sql.NullString
}

func (m *Miner) Id() uint64 {
	return m.id
}

func (m *Miner) Alias() string {
	if m.alias.Valid {
		return m.alias.String
	}
	return ""
}

func (m *Miner) Address() *address.Address {
	return &m.addr
}

func (m *Miner) ScanFromRow(i *Index, row RowScanInterface) error {
	var spendPub, viewPub crypto.PublicKeyBytes
	if err := row.Scan(&m.id, &m.alias, &spendPub, &viewPub); err != nil {
		return err
	}

	network, err := i.consensus.NetworkType.AddressNetwork()
	if err != nil {
		return err
	}
	m.addr = *address.FromRawAddress(network, &spendPub, &viewPub)
	return nil
}
