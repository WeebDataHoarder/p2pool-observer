package main

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/cmd/index"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/transaction"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/api"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"slices"
)

func PayoutAmountHint(shares map[uint64]types.Difficulty, reward uint64) map[uint64]uint64 {
	amountShares := make(map[uint64]uint64)
	totalWeight := types.DifficultyFrom64(0)
	for _, w := range shares {
		totalWeight = totalWeight.Add(w)
	}

	w := types.DifficultyFrom64(0)
	rewardGiven := types.DifficultyFrom64(0)

	for miner, weight := range shares {
		w = w.Add(weight)
		nextValue := w.Mul64(reward).Div(totalWeight)
		amountShares[miner] = nextValue.Sub(rewardGiven).Lo
		rewardGiven = nextValue
	}
	return amountShares
}

func Outputs(p2api *api.P2PoolApi, indexDb *index.Index, tip *index.SideBlock, derivationCache sidechain.DerivationCacheInterface, preAllocatedShares sidechain.Shares, preAllocatedRewards []uint64) (outputs transaction.Outputs, bottomHeight uint64) {
	if tip == nil {
		return nil, 0
	}

	poolBlock := p2api.LightByMainId(tip.MainId)
	if poolBlock != nil {
		window := index.QueryIterateToSlice(indexDb.GetSideBlocksInPPLNSWindow(tip))
		if len(window) == 0 {
			return nil, 0
		}
		var hintIndex int

		getByTemplateIdFull := func(h types.Hash) *index.SideBlock {
			if i := slices.IndexFunc(window, func(e *index.SideBlock) bool {
				return e.TemplateId == h
			}); i != -1 {
				hintIndex = i
				return window[i]
			}
			return nil
		}
		getByTemplateId := func(h types.Hash) *index.SideBlock {
			//fast lookup first
			if i := slices.IndexFunc(window[hintIndex:], func(e *index.SideBlock) bool {
				return e.TemplateId == h
			}); i != -1 {
				hintIndex += i
				return window[hintIndex]
			}
			return getByTemplateIdFull(h)
		}

		getUnclesOf := func(h types.Hash) index.QueryIterator[index.SideBlock] {
			parentEffectiveHeight := window[hintIndex].EffectiveHeight
			if window[hintIndex].TemplateId != h {
				parentEffectiveHeight = 0
			}

			startIndex := 0

			return &index.FakeQueryResult[index.SideBlock]{
				NextFunction: func() (int, *index.SideBlock) {
					for _, b := range window[startIndex+hintIndex:] {
						if b.UncleOf == h {
							startIndex++
							return startIndex, b
						}
						startIndex++

						if parentEffectiveHeight != 0 && b.EffectiveHeight < parentEffectiveHeight {
							//early exit
							break
						}
					}
					return 0, nil
				},
			}
		}

		return index.CalculateOutputs(indexDb,
			tip,
			poolBlock.GetTransactionOutputType(),
			poolBlock.Main.Coinbase.TotalReward,
			&poolBlock.Side.CoinbasePrivateKey,
			poolBlock.Side.CoinbasePrivateKeySeed,
			p2api.MainDifficultyByHeight,
			getByTemplateId,
			getUnclesOf,
			derivationCache,
			preAllocatedShares,
			preAllocatedRewards,
		)
	} else {
		return nil, 0
	}
}

func PayoutHint(p2api *api.P2PoolApi, indexDb *index.Index, tip *index.SideBlock) (shares map[uint64]types.Difficulty, blockDepth uint64) {
	shares = make(map[uint64]types.Difficulty)

	blockDepth, err := index.BlocksInPPLNSWindowFast(indexDb, tip, p2api.MainDifficultyByHeight, func(b *index.SideBlock, weight types.Difficulty) {
		if _, ok := shares[b.Miner]; !ok {
			shares[b.Miner] = types.DifficultyFrom64(0)
		}
		shares[b.Miner] = shares[b.Miner].Add(weight)
	})

	if err != nil {
		return nil, blockDepth
	}

	return shares, blockDepth
}
