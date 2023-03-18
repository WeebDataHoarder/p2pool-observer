package api

import (
	"encoding/json"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/database"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"io"
	"os"
	"path"
)

type Api struct {
	db   *database.Database
	path string
}

func New(db *database.Database, p string) (*Api, error) {
	api := &Api{
		db:   db,
		path: path.Clean(p),
	}

	if info, err := os.Stat(api.path); err != nil {
		return nil, err
	} else if !info.IsDir() {
		return nil, fmt.Errorf("directory path does not exist %s %s", p, api.path)
	}

	return api, nil
}

func (a *Api) GetDatabase() *database.Database {
	return a.db
}

func (a *Api) GetBlockWindowPayouts(tip *database.Block) (shares map[uint64]types.Difficulty) {
	//TODO: adjust for fork
	shares = make(map[uint64]types.Difficulty)

	blockCount := 0

	block := tip

	blockCache := make(map[uint64]*database.Block, p2pool.PPLNSWindow)
	for b := range a.db.GetBlocksInWindow(&tip.Height, p2pool.PPLNSWindow) {
		blockCache[b.Height] = b
	}

	for {
		if _, ok := shares[block.MinerId]; !ok {
			shares[block.MinerId] = types.DifficultyFrom64(0)
		}

		shares[block.MinerId] = shares[block.MinerId].Add(block.Difficulty)

		for uncle := range a.db.GetUnclesByParentId(block.Id) {
			if (tip.Height - uncle.Block.Height) >= p2pool.PPLNSWindow {
				continue
			}

			if _, ok := shares[uncle.Block.MinerId]; !ok {
				shares[uncle.Block.MinerId] = types.DifficultyFrom64(0)
			}

			product := uncle.Block.Difficulty.Mul64(p2pool.UnclePenalty)
			unclePenalty := product.Div64(100)

			shares[block.MinerId] = shares[block.MinerId].Add(unclePenalty)
			shares[uncle.Block.MinerId] = shares[uncle.Block.MinerId].Add(uncle.Block.Difficulty.Sub(unclePenalty))
		}

		blockCount++
		if b, ok := blockCache[block.Height-1]; ok && b.Id == block.PreviousId {
			block = b
		} else {
			block = a.db.GetBlockById(block.PreviousId)
		}
		if block == nil || blockCount >= p2pool.PPLNSWindow {
			break
		}
	}

	totalReward := tip.Coinbase.Reward

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

	if blockCount != p2pool.PPLNSWindow {
		return nil
	}

	return shares
}

func (a *Api) GetWindowPayouts(height, totalReward *uint64) (shares map[uint64]types.Difficulty) {
	shares = make(map[uint64]types.Difficulty)

	var tip uint64
	if height != nil {
		tip = *height
	} else {
		tip = a.db.GetChainTip().Height
	}

	blockCount := 0

	for block := range a.db.GetBlocksInWindow(&tip, p2pool.PPLNSWindow) {
		if _, ok := shares[block.MinerId]; !ok {
			shares[block.MinerId] = types.DifficultyFrom64(0)
		}

		shares[block.MinerId] = shares[block.MinerId].Add(block.Difficulty)

		for uncle := range a.db.GetUnclesByParentId(block.Id) {
			if (tip - uncle.Block.Height) >= p2pool.PPLNSWindow {
				continue
			}

			if _, ok := shares[uncle.Block.MinerId]; !ok {
				shares[uncle.Block.MinerId] = types.DifficultyFrom64(0)
			}

			product := uncle.Block.Difficulty.Mul64(p2pool.UnclePenalty)
			unclePenalty := product.Div64(100)

			shares[block.MinerId] = shares[block.MinerId].Add(unclePenalty)
			shares[uncle.Block.MinerId] = shares[uncle.Block.MinerId].Add(uncle.Block.Difficulty.Sub(unclePenalty))
		}

		blockCount++
	}

	if totalReward != nil && *totalReward > 0 {
		totalWeight := types.DifficultyFrom64(0)
		for _, w := range shares {
			totalWeight = totalWeight.Add(w)
		}

		w := types.DifficultyFrom64(0)
		rewardGiven := types.DifficultyFrom64(0)

		for miner, weight := range shares {
			w = w.Add(weight)
			nextValue := w.Mul64(*totalReward).Div(totalWeight)
			shares[miner] = nextValue.Sub(rewardGiven)
			rewardGiven = nextValue
		}
	}

	if blockCount != p2pool.PPLNSWindow {
		return nil
	}

	return shares
}

// GetPoolBlocks
// Deprecated
func (a *Api) GetPoolBlocks() (result []struct {
	Height      uint64     `json:"height"`
	Hash        types.Hash `json:"hash"`
	Difficulty  uint64     `json:"difficulty"`
	TotalHashes uint64     `json:"totalHashes"`
	Ts          uint64     `json:"ts"`
}, err error) {
	f, err := os.Open(fmt.Sprintf("%s/pool/blocks", a.path))
	if err != nil {
		return result, err
	}
	defer f.Close()
	if buf, err := io.ReadAll(f); err != nil {
		return result, err
	} else {
		err = json.Unmarshal(buf, &result)
		return result, err
	}
}

// GetPoolStats
// Deprecated
func (a *Api) GetPoolStats() (result struct {
	PoolList       []string `json:"pool_list"`
	PoolStatistics struct {
		HashRate           uint64     `json:"hashRate"`
		Difficulty         uint64     `json:"difficulty"`
		Hash               types.Hash `json:"hash"`
		Height             uint64     `json:"height"`
		Miners             uint64     `json:"miners"`
		TotalHashes        uint64     `json:"totalHashes"`
		LastBlockFoundTime uint64     `json:"lastBlockFoundTime"`
		LastBlockFound     uint64     `json:"lastBlockFound"`
		TotalBlocksFound   uint64     `json:"totalBlocksFound"`
	} `json:"pool_statistics"`
}, err error) {
	f, err := os.Open(fmt.Sprintf("%s/pool/stats", a.path))
	if err != nil {
		return result, err
	}
	defer f.Close()
	if buf, err := io.ReadAll(f); err != nil {
		return result, err
	} else {
		err = json.Unmarshal(buf, &result)
		return result, err
	}
}
