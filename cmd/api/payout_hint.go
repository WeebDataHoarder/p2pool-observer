package main

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/index"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/randomx"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/api"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"golang.org/x/exp/slices"
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

func PayoutHint(p2api *api.P2PoolApi, indexDb *index.Index, tip *index.SideBlock) (shares map[uint64]types.Difficulty, bottomHeight uint64) {
	if tip == nil {
		return nil, 0
	}
	window := index.ChanToSlice(indexDb.GetSideBlocksInPPLNSWindow(tip))
	if len(window) == 0 {
		return nil, 0
	}
	shares = make(map[uint64]types.Difficulty)
	getByTemplateId := func(h types.Hash) *index.SideBlock {
		if i := slices.IndexFunc(window, func(e *index.SideBlock) bool {
			return e.TemplateId == h
		}); i != -1 {
			return window[i]
		}
		return nil
	}
	getUnclesOf := func(h types.Hash) (result []*index.SideBlock) {
		for _, b := range window {
			if b.UncleOf == h {
				result = append(result, b)
			}
		}
		return result
	}

	mainchainDiff := p2api.MainDifficultyByHeight(randomx.SeedHeight(tip.MainHeight))

	if mainchainDiff == types.ZeroDifficulty {
		return nil, 0
	}

	sidechainVersion := sidechain.P2PoolShareVersion(p2api.Consensus(), tip.Timestamp)
	maxPplnsWeight := types.MaxDifficulty

	if sidechainVersion > sidechain.ShareVersion_V1 {
		maxPplnsWeight = mainchainDiff.Mul64(2)
	}
	var pplnsWeight types.Difficulty

	var blockDepth uint64

	cur := window[0]
	for ; cur != nil; cur = getByTemplateId(cur.ParentTemplateId) {

		curWeight := types.DifficultyFrom64(cur.Difficulty)

		for _, uncle := range getUnclesOf(cur.TemplateId) {
			if (tip.SideHeight - uncle.SideHeight) >= p2api.Consensus().ChainWindowSize {
				continue
			}

			unclePenalty := types.DifficultyFrom64(uncle.Difficulty).Mul64(p2api.Consensus().UnclePenalty).Div64(100)
			uncleWeight := types.DifficultyFrom64(uncle.Difficulty).Sub(unclePenalty)
			newPplnsWeight := pplnsWeight.Add(uncleWeight)

			if newPplnsWeight.Cmp(maxPplnsWeight) > 0 {
				continue
			}

			curWeight = curWeight.Add(unclePenalty)

			if _, ok := shares[uncle.Miner]; !ok {
				shares[uncle.Miner] = types.DifficultyFrom64(0)
			}
			shares[uncle.Miner] = shares[uncle.Miner].Add(uncleWeight)

			pplnsWeight = newPplnsWeight
		}

		if _, ok := shares[cur.Miner]; !ok {
			shares[cur.Miner] = types.DifficultyFrom64(0)
		}
		shares[cur.Miner] = shares[cur.Miner].Add(curWeight)

		pplnsWeight = pplnsWeight.Add(curWeight)

		if pplnsWeight.Cmp(maxPplnsWeight) > 0 {
			break
		}

		blockDepth++

		if blockDepth >= p2api.Consensus().ChainWindowSize {
			break
		}

		if cur.SideHeight == 0 {
			break
		}
	}

	if (cur == nil || cur.SideHeight != 0) &&
		((sidechainVersion == sidechain.ShareVersion_V1 && blockDepth != p2api.Consensus().ChainWindowSize) ||
			(sidechainVersion > sidechain.ShareVersion_V1 && (blockDepth != p2api.Consensus().ChainWindowSize) && pplnsWeight.Cmp(maxPplnsWeight) <= 0)) {
		return nil, blockDepth
	}

	return shares, blockDepth
}
