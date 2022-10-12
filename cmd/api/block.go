package main

import (
	"bytes"
	"git.gammaspectra.live/P2Pool/p2pool-observer/database"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/api"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"
)

// MapJSONBlock fills special values for any block
func MapJSONBlock(api *api.Api, block database.BlockInterface, extraUncleData, extraCoinbaseData bool) {

	b := block.GetBlock()
	b.Lock.Lock()
	defer b.Lock.Unlock()

	if extraCoinbaseData {

		var tx *database.CoinbaseTransaction
		if b.Main.Found {
			tx = api.GetDatabase().GetCoinbaseTransaction(b)
		}

		if b.Main.Found && tx != nil {
			b.Coinbase.Payouts = make([]*database.JSONCoinbaseOutput, len(tx.Outputs()))
			for _, output := range tx.Outputs() {
				b.Coinbase.Payouts[output.Index()] = &database.JSONCoinbaseOutput{
					Amount:  output.Amount(),
					Index:   output.Index(),
					Address: api.GetDatabase().GetMiner(output.Miner()).Address(),
				}
			}
		} else {
			payoutHint := api.GetBlockWindowPayouts(b)

			addresses := make(map[[32]byte]*database.JSONCoinbaseOutput, len(payoutHint))

			var k [32]byte

			for minerId, amount := range payoutHint {
				miner := api.GetDatabase().GetMiner(minerId)
				copy(k[:], miner.MoneroAddress().SpendPub.Bytes())
				copy(k[types.HashSize:], miner.MoneroAddress().ViewPub.Bytes())
				addresses[k] = &database.JSONCoinbaseOutput{
					Address: miner.Address(),
					Amount:  amount.Lo,
				}
			}

			sortedAddresses := maps.Keys(addresses)

			slices.SortFunc(sortedAddresses, func(a [32]byte, b [32]byte) bool {
				return bytes.Compare(a[:], b[:]) < 0
			})

			b.Coinbase.Payouts = make([]*database.JSONCoinbaseOutput, len(sortedAddresses))

			for i, key := range sortedAddresses {
				addresses[key].Index = uint64(i)
				b.Coinbase.Payouts[i] = addresses[key]
			}
		}
	}

	weight := b.Difficulty
	b.Uncles = make([]any, 0)

	if uncle, ok := block.(*database.UncleBlock); ok {
		b.Parent = &database.JSONBlockParent{
			Id:     uncle.ParentId,
			Height: uncle.ParentHeight,
		}

		weight.Uint128 = weight.Mul64(100 - p2pool.UnclePenalty).Div64(100)
	} else {
		for u := range api.GetDatabase().GetUnclesByParentId(b.Id) {
			uncleWeight := u.Block.Difficulty.Mul64(p2pool.UnclePenalty).Div64(100)
			weight.Uint128 = weight.Add(uncleWeight)

			if !extraUncleData {
				b.Uncles = append(b.Uncles, &database.JSONUncleBlockSimple{
					Id:     u.Block.Id,
					Height: u.Block.Height,
					Weight: u.Block.Difficulty.Mul64(100 - p2pool.UnclePenalty).Div64(100).Lo,
				})
			} else {
				b.Uncles = append(b.Uncles, &database.JSONUncleBlockExtra{
					Id:         u.Block.Id,
					Height:     u.Block.Height,
					Difficulty: u.Block.Difficulty,
					Timestamp:  u.Block.Timestamp,
					Miner:      api.GetDatabase().GetMiner(u.Block.MinerId).Address(),
					PowHash:    u.Block.PowHash,
					Weight:     u.Block.Difficulty.Mul64(100 - p2pool.UnclePenalty).Div64(100).Lo,
				})
			}
		}
	}

	b.Weight = weight.Lo

	if b.IsProofHigherThanDifficulty() && !b.Main.Found {
		b.Main.Orphan = true
	}

	b.Address = api.GetDatabase().GetMiner(b.MinerId).Address()
}
