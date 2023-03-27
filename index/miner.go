package index

import (
	"database/sql"
	"errors"
	"git.gammaspectra.live/P2Pool/moneroutil"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
)

const MinerSelectFields = "id, alias, spend_public_key, view_public_key"

type Miner struct {
	id    uint64
	addr  *address.Address
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
	return m.addr
}

func (m *Miner) ScanFromRow(i *Index, row RowScanInterface) error {
	var spendPub, viewPub crypto.PublicKeyBytes
	if err := row.Scan(&m.id, &m.alias, &spendPub, &viewPub); err != nil {
		return err
	}
	var network uint8
	switch i.consensus.NetworkType {
	case sidechain.NetworkMainnet:
		network = moneroutil.MainNetwork
	case sidechain.NetworkTestnet:
		network = moneroutil.TestNetwork
	default:
		return errors.New("unknown network type")
	}

	m.addr = address.FromRawAddress(network, &spendPub, &viewPub)
	return nil
}
