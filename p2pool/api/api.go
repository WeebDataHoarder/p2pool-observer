package api

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/database"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/randomx"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
)

type Api struct {
	db    *database.Database
	p2api *P2PoolApi
}

func New(db *database.Database, p2api *P2PoolApi) (*Api, error) {
	api := &Api{
		db:    db,
		p2api: p2api,
	}

	return api, nil
}

func (a *Api) GetP2PoolAPI() *P2PoolApi {
	return a.p2api
}

func (a *Api) GetDatabase() *database.Database {
	return a.db
}

func (a *Api) GetBlockWindowPayouts(tip database.BlockInterface) (shares map[uint64]types.Difficulty, bottomHeight uint64) {
	if tip == nil {
		return nil, 0
	}
	//TODO: adjust for fork
	shares = make(map[uint64]types.Difficulty)

	blockCache := make(map[uint64]*database.Block, a.p2api.Consensus().ChainWindowSize)

	for b := range a.db.GetBlocksInWindow(&tip.GetBlock().Height, a.p2api.Consensus().ChainWindowSize) {
		blockCache[b.Height] = b
	}

	mainchainDiff := a.p2api.MainDifficultyByHeight(randomx.SeedHeight(tip.GetBlock().Main.Height))

	if mainchainDiff == types.ZeroDifficulty {
		return nil, 0
	}

	//TODO: remove this hack
	sidechainVersion := sidechain.ShareVersion_V1
	if tip.GetBlock().Timestamp >= sidechain.ShareVersion_V2MainNetTimestamp {
		sidechainVersion = sidechain.ShareVersion_V2
	}
	maxPplnsWeight := types.MaxDifficulty

	if sidechainVersion > sidechain.ShareVersion_V1 {
		maxPplnsWeight = mainchainDiff.Mul64(2)
	}
	var pplnsWeight types.Difficulty

	var blockDepth uint64
	block := tip.GetBlock()

	for {

		curWeight := block.Difficulty

		for uncle := range a.db.GetUnclesByParentId(block.Id) {
			if (tip.GetBlock().Height - uncle.Block.Height) >= a.p2api.Consensus().ChainWindowSize {
				continue
			}

			unclePenalty := uncle.Block.Difficulty.Mul64(a.p2api.Consensus().UnclePenalty).Div64(100)
			uncleWeight := uncle.Block.Difficulty.Sub(unclePenalty)
			newPplnsWeight := pplnsWeight.Add(uncleWeight)

			if newPplnsWeight.Cmp(maxPplnsWeight) > 0 {
				continue
			}

			curWeight = curWeight.Add(unclePenalty)

			if _, ok := shares[uncle.Block.MinerId]; !ok {
				shares[uncle.Block.MinerId] = types.DifficultyFrom64(0)
			}
			shares[uncle.Block.MinerId] = shares[uncle.Block.MinerId].Add(uncleWeight)

			pplnsWeight = newPplnsWeight
		}

		if _, ok := shares[block.MinerId]; !ok {
			shares[block.MinerId] = types.DifficultyFrom64(0)
		}
		shares[block.MinerId] = shares[block.MinerId].Add(curWeight)

		pplnsWeight = pplnsWeight.Add(curWeight)

		if pplnsWeight.Cmp(maxPplnsWeight) > 0 {
			break
		}

		blockDepth++

		if blockDepth >= a.p2api.Consensus().ChainWindowSize {
			break
		}

		if block.Height == 0 {
			break
		}

		if b, ok := blockCache[block.Height-1]; ok && b.Id == block.PreviousId {
			block = b
		} else {
			block = a.db.GetBlockById(block.PreviousId)
		}
		if block == nil {
			break
		}
	}

	totalReward := tip.GetBlock().Coinbase.Reward

	if totalReward > 0 {
		totalWeight := types.DifficultyFrom64(0)
		for _, w := range shares {
			totalWeight = totalWeight.Add(w)
		}

		w := types.DifficultyFrom64(0)
		rewardGiven := types.DifficultyFrom64(0)

		for miner, weight := range shares {
			w = w.Add(weight)
			nextValue := w.Mul64(totalReward).Div(totalWeight)
			shares[miner] = nextValue.Sub(rewardGiven)
			rewardGiven = nextValue
		}
	}

	if (block == nil || block.Height != 0) &&
		((sidechainVersion == sidechain.ShareVersion_V1 && blockDepth != a.p2api.Consensus().ChainWindowSize) ||
			(sidechainVersion > sidechain.ShareVersion_V1 && (blockDepth != a.p2api.Consensus().ChainWindowSize) && pplnsWeight.Cmp(maxPplnsWeight) <= 0)) {
		return nil, blockDepth
	}

	return shares, blockDepth
}
