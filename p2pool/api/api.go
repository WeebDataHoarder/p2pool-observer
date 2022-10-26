package api

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/database"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"io"
	"lukechampine.com/uint128"
	"os"
	"path"
	"strconv"
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

func (a *Api) getBlockPath(height uint64) string {
	index := strconv.FormatInt(int64(height), 10)
	return fmt.Sprintf("%s/share/%s/%s", a.path, index[len(index)-1:], index)
}

func (a *Api) getRawBlockPath(id types.Hash) string {
	index := id.String()
	return fmt.Sprintf("%s/blocks/%s/%s", a.path, index[:1], index)
}

func (a *Api) getFailedRawBlockPath(id types.Hash) string {
	index := id.String()
	return fmt.Sprintf("%s/failed_blocks/%s/%s", a.path, index[:1], index)
}

func (a *Api) BlockExists(height uint64) bool {
	_, err := os.Stat(a.getBlockPath(height))
	return err == nil
}

func (a *Api) GetShareEntry(height uint64) (*database.Block, []*database.UncleBlock, error) {
	if f, err := os.Open(a.getBlockPath(height)); err != nil {
		return nil, nil, err
	} else {
		defer f.Close()
		if buf, err := io.ReadAll(f); err != nil {
			return nil, nil, err
		} else {
			return database.NewBlockFromJSONBlock(a.db, buf)
		}
	}
}

func (a *Api) GetShareFromFailedRawEntry(id types.Hash) (*database.Block, error) {

	if b, err := a.GetFailedRawBlock(id); err != nil {
		return nil, err
	} else {
		b, _, err := database.NewBlockFromBinaryBlock(a.db, b, nil, false)
		return b, err
	}
}

func (a *Api) GetFailedRawBlockBytes(id types.Hash) (buf []byte, err error) {
	if f, err := os.Open(a.getFailedRawBlockPath(id)); err != nil {
		return nil, err
	} else {
		defer f.Close()
		return io.ReadAll(f)
	}
}

func (a *Api) GetFailedRawBlock(id types.Hash) (b *sidechain.Share, err error) {
	if buf, err := a.GetFailedRawBlockBytes(id); err != nil {
		return nil, err
	} else {
		data := make([]byte, len(buf)/2)
		_, _ = hex.Decode(data, buf)
		return sidechain.NewShareFromBytes(data)
	}
}

func (a *Api) GetRawBlockBytes(id types.Hash) (buf []byte, err error) {
	if f, err := os.Open(a.getRawBlockPath(id)); err != nil {
		return nil, err
	} else {
		defer f.Close()
		return io.ReadAll(f)
	}
}

func (a *Api) GetRawBlock(id types.Hash) (b *sidechain.Share, err error) {
	if buf, err := a.GetRawBlockBytes(id); err != nil {
		return nil, err
	} else {
		data := make([]byte, len(buf)/2)
		_, _ = hex.Decode(data, buf)
		return sidechain.NewShareFromBytes(data)
	}
}

func (a *Api) GetDatabase() *database.Database {
	return a.db
}

func (a *Api) GetShareFromRawEntry(id types.Hash, errOnUncles bool) (b *database.Block, uncles []*database.UncleBlock, err error) {
	var raw *sidechain.Share
	if raw, err = a.GetRawBlock(id); err != nil {
		return
	} else {
		u := make([]*sidechain.Share, 0, len(raw.Side.Uncles))
		for _, uncleId := range raw.Side.Uncles {
			if uncle, err := a.GetRawBlock(uncleId); err != nil {
				return nil, nil, err
			} else {
				u = append(u, uncle)
			}
		}

		return database.NewBlockFromBinaryBlock(a.db, raw, u, errOnUncles)
	}
}

func (a *Api) GetBlockWindowPayouts(tip *database.Block) (shares map[uint64]uint128.Uint128) {
	shares = make(map[uint64]uint128.Uint128)

	blockCount := 0

	block := tip

	blockCache := make(map[uint64]*database.Block, p2pool.PPLNSWindow)
	for b := range a.db.GetBlocksInWindow(&tip.Height, p2pool.PPLNSWindow) {
		blockCache[b.Height] = b
	}

	for {
		if _, ok := shares[block.MinerId]; !ok {
			shares[block.MinerId] = uint128.From64(0)
		}

		shares[block.MinerId] = shares[block.MinerId].Add(block.Difficulty.Uint128)

		for uncle := range a.db.GetUnclesByParentId(block.Id) {
			if (tip.Height - uncle.Block.Height) >= p2pool.PPLNSWindow {
				continue
			}

			if _, ok := shares[uncle.Block.MinerId]; !ok {
				shares[uncle.Block.MinerId] = uint128.From64(0)
			}

			product := uncle.Block.Difficulty.Uint128.Mul64(p2pool.UnclePenalty)
			unclePenalty := product.Div64(100)

			shares[block.MinerId] = shares[block.MinerId].Add(unclePenalty)
			shares[uncle.Block.MinerId] = shares[uncle.Block.MinerId].Add(uncle.Block.Difficulty.Uint128.Sub(unclePenalty))
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
		totalWeight := uint128.From64(0)
		for _, w := range shares {
			totalWeight = totalWeight.Add(w)
		}

		w := uint128.From64(0)
		rewardGiven := uint128.From64(0)

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

func (a *Api) GetWindowPayouts(height, totalReward *uint64) (shares map[uint64]uint128.Uint128) {
	shares = make(map[uint64]uint128.Uint128)

	var tip uint64
	if height != nil {
		tip = *height
	} else {
		tip = a.db.GetChainTip().Height
	}

	blockCount := 0

	for block := range a.db.GetBlocksInWindow(&tip, p2pool.PPLNSWindow) {
		if _, ok := shares[block.MinerId]; !ok {
			shares[block.MinerId] = uint128.From64(0)
		}

		shares[block.MinerId] = shares[block.MinerId].Add(block.Difficulty.Uint128)

		for uncle := range a.db.GetUnclesByParentId(block.Id) {
			if (tip - uncle.Block.Height) >= p2pool.PPLNSWindow {
				continue
			}

			if _, ok := shares[uncle.Block.MinerId]; !ok {
				shares[uncle.Block.MinerId] = uint128.From64(0)
			}

			product := uncle.Block.Difficulty.Uint128.Mul64(p2pool.UnclePenalty)
			unclePenalty := product.Div64(100)

			shares[block.MinerId] = shares[block.MinerId].Add(unclePenalty)
			shares[uncle.Block.MinerId] = shares[uncle.Block.MinerId].Add(uncle.Block.Difficulty.Uint128.Sub(unclePenalty))
		}

		blockCount++
	}

	if totalReward != nil && *totalReward > 0 {
		totalWeight := uint128.From64(0)
		for _, w := range shares {
			totalWeight = totalWeight.Add(w)
		}

		w := uint128.From64(0)
		rewardGiven := uint128.From64(0)

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
