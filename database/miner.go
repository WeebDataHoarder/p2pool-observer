package database

import (
	"bytes"
	"database/sql"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/transaction"
	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"
	"sync/atomic"
)

type Miner struct {
	id            uint64
	addr          string
	alias         sql.NullString
	moneroAddress atomic.Pointer[address.Address]
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

func (m *Miner) Address() string {
	return m.addr
}

func (m *Miner) MoneroAddress() *address.Address {
	if a := m.moneroAddress.Load(); a != nil {
		return a
	} else {
		a = address.FromBase58(m.addr)
		m.moneroAddress.Store(a)
		return a
	}
}



type outputResult struct {
	Miner  *Miner
	Output *transaction.CoinbaseTransactionOutput
}

func MatchOutputs(c *transaction.CoinbaseTransaction, miners []*Miner, privateKey crypto.PrivateKey) (result []outputResult) {
	addresses := make(map[address.PackedAddress]*Miner, len(miners))

	outputs := c.Outputs

	for _, m := range miners {
		addresses[*m.MoneroAddress().ToPackedAddress()] = m
	}

	sortedAddresses := maps.Keys(addresses)

	slices.SortFunc(sortedAddresses, func(a address.PackedAddress, b address.PackedAddress) bool {
		return a.Compare(&b) < 0
	})

	result = make([]outputResult, 0, len(miners))

	for _, k := range sortedAddresses {
		derivation := privateKey.GetDerivation8(k.ViewPublicKey())
		for i, o := range outputs {
			if o == nil {
				continue
			}

			if o.Type == transaction.TxOutToTaggedKey && o.ViewTag != crypto.GetDerivationViewTagForOutputIndex(derivation, o.Index) { //fast check
				continue
			}

			sharedData := crypto.GetDerivationSharedDataForOutputIndex(derivation, o.Index)
			if bytes.Compare(o.EphemeralPublicKey[:], address.GetPublicKeyForSharedData(&k, sharedData).AsSlice()) == 0 {
				//TODO: maybe clone?
				result = append(result, outputResult{
					Miner:  addresses[k],
					Output: o,
				})
				outputs[i] = nil
				outputs = slices.Compact(outputs)
				break
			}
		}
	}

	slices.SortFunc(result, func(a outputResult, b outputResult) bool {
		return a.Output.Index < b.Output.Index
	})

	return result
}
