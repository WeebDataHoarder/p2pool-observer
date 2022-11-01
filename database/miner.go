package database

import (
	"bytes"
	"database/sql"
	"filippo.io/edwards25519"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/transaction"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
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

type addressSortType [types.HashSize * 2]byte

type outputResult struct {
	Miner  *Miner
	Output *transaction.CoinbaseTransactionOutput
}

func MatchOutputs(c *transaction.CoinbaseTransaction, miners []*Miner, privateKey types.Hash) (result []outputResult) {
	addresses := make(map[addressSortType]*Miner, len(miners))

	outputs := c.Outputs

	var k addressSortType
	for _, m := range miners {
		copy(k[:], m.MoneroAddress().SpendPub.Bytes())
		copy(k[types.HashSize:], m.MoneroAddress().ViewPub.Bytes())
		addresses[k] = m
	}

	sortedAddresses := maps.Keys(addresses)

	slices.SortFunc(sortedAddresses, func(a addressSortType, b addressSortType) bool {
		return bytes.Compare(a[:], b[:]) < 0
	})

	result = make([]outputResult, 0, len(miners))

	for _, k = range sortedAddresses {
		miner := addresses[k]

		pK, _ := edwards25519.NewScalar().SetCanonicalBytes(privateKey[:])
		derivation := miner.MoneroAddress().GetDerivationForPrivateKey(pK)
		for i, o := range outputs {
			if o == nil {
				continue
			}

			if o.Type == transaction.TxOutToTaggedKey && o.ViewTag != crypto.GetDerivationViewTagForOutputIndex(derivation, o.Index) { //fast check
				continue
			}

			sharedData := crypto.GetDerivationSharedDataForOutputIndex(derivation, o.Index)
			if bytes.Compare(o.EphemeralPublicKey[:], miner.MoneroAddress().GetPublicKeyForSharedData(sharedData).Bytes()) == 0 {
				//TODO: maybe clone?
				result = append(result, outputResult{
					Miner:  miner,
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
