package main

import (
	"encoding/binary"
	"git.gammaspectra.live/P2Pool/p2pool-observer/index"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/randomx"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/api"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"
	"lukechampine.com/uint128"
)

type BlockCoinbase struct {
	Id         types.Hash             `json:"id"`
	Reward     uint64                 `json:"reward"`
	PrivateKey crypto.PrivateKeyBytes `json:"private_key"`
	//Payouts extra JSON field, do not use
	Payouts []*JSONCoinbaseOutput `json:"payouts,omitempty"`
}

type BlockMainData struct {
	Id     types.Hash `json:"id"`
	Height uint64     `json:"height"`
	Found  bool       `json:"found"`

	//Orphan extra JSON field, do not use
	Orphan bool `json:"orphan,omitempty"`
}

type JSONBlockParent struct {
	Id     types.Hash `json:"id"`
	Height uint64     `json:"height"`
}

type JSONUncleBlockSimple struct {
	Id     types.Hash `json:"id"`
	Height uint64     `json:"height"`
	Weight uint64     `json:"weight"`
}

type JSONCoinbaseOutput struct {
	Amount            uint64 `json:"amount"`
	Index             uint64 `json:"index"`
	Address           string `json:"address"`
	Alias             string `json:"alias,omitempty"`
	GlobalOutputIndex uint64 `json:"global_output_index"`
}

type JSONUncleBlockExtra struct {
	Id         types.Hash       `json:"id"`
	Height     uint64           `json:"height"`
	Difficulty types.Difficulty `json:"difficulty"`
	Timestamp  uint64           `json:"timestamp"`
	Miner      string           `json:"miner"`
	MinerAlias string           `json:"miner_alias,omitempty"`
	PowHash    types.Hash       `json:"pow"`
	Weight     uint64           `json:"weight"`
}

// JSONBlock
// Deprecated
type JSONBlock struct {
	Id                   types.Hash       `json:"id"`
	Height               uint64           `json:"height"`
	PreviousId           types.Hash       `json:"previous_id"`
	Coinbase             BlockCoinbase    `json:"coinbase"`
	Difficulty           uint64           `json:"difficulty"`
	CumulativeDifficulty types.Difficulty `json:"cumulative_difficulty"`
	Timestamp            uint64           `json:"timestamp"`
	MinerId              uint64           `json:"-"`
	//Address extra JSON field, do not use
	Address    string     `json:"miner,omitempty"`
	MinerAlias string     `json:"miner_alias,omitempty"`
	PowHash    types.Hash `json:"pow"`

	Main BlockMainData `json:"main"`

	Nonce            uint32 `json:"nonce"`
	ExtraNonce       uint32 `json:"extra_nonce"`
	TransactionCount uint32 `json:"transaction_count"`
	SoftwareId       uint32 `json:"software_id"`
	SoftwareVersion  uint32 `json:"software_version"`

	Template struct {
		Id         types.Hash       `json:"id"`
		Difficulty types.Difficulty `json:"difficulty"`
	} `json:"template"`

	//Parent extra JSON field, do not use
	Parent *JSONBlockParent `json:"parent,omitempty"`
	//Uncles extra JSON field, do not use
	Uncles []any `json:"uncles"`
	//Weight extra JSON field, do not use
	Weight uint64 `json:"weight"`

	//Orphan extra JSON field, do not use
	Orphan bool `json:"orphan,omitempty"`

	//Invalid extra JSON field, do not use
	Invalid *bool `json:"invalid,omitempty"`
}

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

	//TODO: remove this hack
	sidechainVersion := sidechain.ShareVersion_V1
	if tip.Timestamp >= sidechain.ShareVersion_V2MainNetTimestamp {
		sidechainVersion = sidechain.ShareVersion_V2
	}
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

// MapJSONBlock fills special values for any block
func MapJSONBlock(p2api *api.P2PoolApi, indexDb *index.Index, block *index.SideBlock, mainFound *index.BlockFound, extraUncleData, extraCoinbaseData bool) *JSONBlock {

	mainDifficulty := indexDb.GetDifficultyByHeight(block.MainHeight)
	blockMiner := indexDb.GetMiner(block.Miner)
	b := &JSONBlock{
		Id:                   block.TemplateId,
		Height:               block.SideHeight,
		PreviousId:           block.ParentTemplateId,
		Difficulty:           block.Difficulty,
		CumulativeDifficulty: block.CumulativeDifficulty,
		Timestamp:            block.Timestamp,
		MinerId:              block.Miner,
		Address:              blockMiner.Address().ToBase58(),
		MinerAlias:           blockMiner.Alias(),
		PowHash:              block.PowHash,
		Nonce:                block.Nonce,
		ExtraNonce:           block.ExtraNonce,
		TransactionCount:     block.TransactionCount,
		SoftwareId:           uint32(block.SoftwareId),
		SoftwareVersion:      uint32(block.SoftwareVersion),
		Main: BlockMainData{
			Id:     block.MainId,
			Height: block.MainHeight,
		},
		Template: struct {
			Id         types.Hash       `json:"id"`
			Difficulty types.Difficulty `json:"difficulty"`
		}{
			Id:         types.ZeroHash, //TODO
			Difficulty: mainDifficulty,
		},
		Orphan: block.Inclusion == index.InclusionOrphan,
	}

	if mainDifficulty.Cmp64(block.PowDifficulty) < 0 {
		if mainFound == nil {
			for _, bf := range indexDb.GetBlocksFound("WHERE main_id = $1", 1, &block.MainId) {
				mainFound = bf
			}
		}

		if mainFound != nil {
			b.Main.Found = true
			b.Coinbase.Id = mainFound.MainBlock.CoinbaseId
			b.Coinbase.PrivateKey = mainFound.MainBlock.CoinbasePrivateKey
			b.Coinbase.Reward = mainFound.MainBlock.Reward
		} else {
			b.Main.Orphan = true
		}
	}

	if extraCoinbaseData {

		var tx index.MainCoinbaseOutputs
		if mainFound != nil {
			tx = indexDb.GetMainCoinbaseOutputs(mainFound.MainBlock.CoinbaseId)
		}

		if mainFound != nil && tx != nil {
			b.Coinbase.Payouts = make([]*JSONCoinbaseOutput, len(tx))
			for _, output := range tx {
				miner := indexDb.GetMiner(output.Miner)
				b.Coinbase.Payouts[output.Index] = &JSONCoinbaseOutput{
					Amount:            output.Value,
					Index:             uint64(output.Index),
					Address:           miner.Address().ToBase58(),
					Alias:             miner.Alias(),
					GlobalOutputIndex: output.GlobalOutputIndex,
				}
			}
		} else {
			shares, _ := PayoutHint(p2api, indexDb, block)
			if shares != nil {
				poolBlock := p2api.ByMainId(block.MainId)
				if poolBlock != nil {
					addresses := make(map[address.PackedAddress]*JSONCoinbaseOutput, len(shares))
					for minerId, amount := range PayoutAmountHint(shares, poolBlock.Main.Coinbase.TotalReward) {
						miner := indexDb.GetMiner(minerId)
						addresses[*miner.Address().ToPackedAddress()] = &JSONCoinbaseOutput{
							Address:           miner.Address().ToBase58(),
							Alias:             miner.Alias(),
							Amount:            amount,
							GlobalOutputIndex: 0,
						}
					}

					sortedAddresses := maps.Keys(addresses)

					slices.SortFunc(sortedAddresses, func(a address.PackedAddress, b address.PackedAddress) bool {
						return a.Compare(&b) < 0
					})

					n := len(sortedAddresses)

					//Shuffle shares
					if poolBlock.ShareVersion() > sidechain.ShareVersion_V1 && n > 1 {
						h := crypto.PooledKeccak256(poolBlock.Side.CoinbasePrivateKeySeed[:])
						seed := binary.LittleEndian.Uint64(h[:])

						if seed == 0 {
							seed = 1
						}

						for i := 0; i < (n - 1); i++ {
							seed = utils.XorShift64Star(seed)
							k := int(uint128.From64(seed).Mul64(uint64(n - i)).Hi)
							//swap
							sortedAddresses[i], sortedAddresses[i+k] = sortedAddresses[i+k], sortedAddresses[i]
						}
					}

					b.Coinbase.Payouts = make([]*JSONCoinbaseOutput, len(sortedAddresses))

					for i, key := range sortedAddresses {
						addresses[key].Index = uint64(i)
						b.Coinbase.Payouts[i] = addresses[key]
					}
				}
			}
		}
	}

	weight := types.DifficultyFrom64(b.Difficulty)
	b.Uncles = make([]any, 0)

	if block.IsUncle() {
		b.Parent = &JSONBlockParent{
			Id:     block.UncleOf,
			Height: block.EffectiveHeight,
		}

		weight = weight.Mul64(100 - p2api.Consensus().UnclePenalty).Div64(100)
	} else {
		for u := range indexDb.GetSideBlocksByUncleId(b.Id) {
			uncleWeight := types.DifficultyFrom64(u.Difficulty).Mul64(p2api.Consensus().UnclePenalty).Div64(100)
			weight = weight.Add(uncleWeight)

			type JSONUncleBlockSimple struct {
				Id     types.Hash `json:"id"`
				Height uint64     `json:"height"`
				Weight uint64     `json:"weight"`
			}

			type JSONUncleBlockExtra struct {
				Id              types.Hash `json:"id"`
				Height          uint64     `json:"height"`
				Difficulty      uint64     `json:"difficulty"`
				Timestamp       uint64     `json:"timestamp"`
				Miner           string     `json:"miner"`
				MinerAlias      string     `json:"miner_alias,omitempty"`
				SoftwareId      uint32     `json:"software_id"`
				SoftwareVersion uint32     `json:"software_version"`
				PowHash         types.Hash `json:"pow"`
				Weight          uint64     `json:"weight"`
			}

			if !extraUncleData {
				b.Uncles = append(b.Uncles, &JSONUncleBlockSimple{
					Id:     u.TemplateId,
					Height: u.SideHeight,
					Weight: types.DifficultyFrom64(u.Difficulty).Mul64(100 - p2api.Consensus().UnclePenalty).Div64(100).Lo,
				})
			} else {
				uncleMiner := indexDb.GetMiner(u.Miner)
				b.Uncles = append(b.Uncles, &JSONUncleBlockExtra{
					Id:              u.TemplateId,
					Height:          u.SideHeight,
					Difficulty:      u.Difficulty,
					Timestamp:       u.Timestamp,
					Miner:           uncleMiner.Address().ToBase58(),
					MinerAlias:      uncleMiner.Alias(),
					SoftwareId:      uint32(u.SoftwareId),
					SoftwareVersion: uint32(u.SoftwareVersion),
					PowHash:         u.PowHash,
					Weight:          types.DifficultyFrom64(u.Difficulty).Mul64(100 - p2api.Consensus().UnclePenalty).Div64(100).Lo,
				})
			}
		}
	}

	b.Weight = weight.Lo

	return b
}
