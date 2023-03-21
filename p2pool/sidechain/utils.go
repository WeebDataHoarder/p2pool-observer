package sidechain

import (
	"encoding/binary"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/block"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/randomx"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/transaction"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"golang.org/x/exp/slices"
	"log"
	"lukechampine.com/uint128"
	"math"
)

type GetByTemplateIdFunc func(h types.Hash) *PoolBlock

func CalculateOutputs(block *PoolBlock, consensus *Consensus, difficultyByHeight block.GetDifficultyByHeightFunc, getByTemplateId GetByTemplateIdFunc, derivationCache DerivationCacheInterface, preAllocatedShares Shares) (outputs transaction.Outputs, bottomHeight uint64) {
	tmpShares, bottomHeight := GetShares(block, consensus, difficultyByHeight, getByTemplateId, preAllocatedShares)
	tmpRewards := SplitReward(block.Main.Coinbase.TotalReward, tmpShares)

	if tmpShares == nil || tmpRewards == nil || len(tmpRewards) != len(tmpShares) {
		return nil, 0
	}

	n := uint64(len(tmpShares))

	outputs = make(transaction.Outputs, n)

	txType := block.GetTransactionOutputType()

	txPrivateKeySlice := block.Side.CoinbasePrivateKey.AsSlice()
	txPrivateKeyScalar := block.Side.CoinbasePrivateKey.AsScalar()

	_ = utils.SplitWork(-2, n, func(workIndex uint64, workerIndex int) error {
		output := transaction.Output{
			Index: workIndex,
			Type:  txType,
		}
		output.Reward = tmpRewards[output.Index]
		output.EphemeralPublicKey, output.ViewTag = derivationCache.GetEphemeralPublicKey(&tmpShares[output.Index].Address, txPrivateKeySlice, txPrivateKeyScalar, output.Index)

		outputs[output.Index] = output

		return nil
	})

	return outputs, bottomHeight
}

func GetShares(tip *PoolBlock, consensus *Consensus, difficultyByHeight block.GetDifficultyByHeightFunc, getByTemplateId GetByTemplateIdFunc, preAllocatedShares Shares) (shares Shares, bottomHeight uint64) {

	var blockDepth uint64

	cur := tip

	var mainchainDiff types.Difficulty

	if tip.Side.Parent != types.ZeroHash {
		seedHeight := randomx.SeedHeight(tip.Main.Coinbase.GenHeight)
		mainchainDiff = difficultyByHeight(seedHeight)
		if mainchainDiff == types.ZeroDifficulty {
			log.Printf("[SideChain] get_shares: couldn't get mainchain difficulty for height = %d", seedHeight)
			return nil, 0
		}
	}

	// Dynamic PPLNS window starting from v2
	// Limit PPLNS weight to 2x of the Monero difficulty (max 2 blocks per PPLNS window on average)
	sidechainVersion := tip.ShareVersion()

	maxPplnsWeight := types.MaxDifficulty

	if sidechainVersion > ShareVersion_V1 {
		maxPplnsWeight = mainchainDiff.Mul64(2)
	}

	var pplnsWeight types.Difficulty

	sharesSet := make(map[address.PackedAddress]*Share, consensus.ChainWindowSize*2)

	index := 0
	indexGet := 0
	l := len(preAllocatedShares)
	getPreAllocated := func() (s *Share) {
		if indexGet < l {
			s = preAllocatedShares[indexGet]
		} else {
			s = &Share{}
		}
		indexGet++
		return s
	}

	insertSet := func(weight types.Difficulty, a *address.PackedAddress) {
		if _, ok := sharesSet[*a]; ok {
			sharesSet[*a].Weight = sharesSet[*a].Weight.Add(weight)
		} else {
			s := getPreAllocated()
			s.Weight = weight
			s.Address = *a
			sharesSet[*a] = s
		}
	}

	insertPreAllocated := func(share *Share) {
		if index < l {
			preAllocatedShares[index] = share
		} else {
			preAllocatedShares = append(preAllocatedShares, share)
		}
		index++
	}

	for {
		curWeight := cur.Side.Difficulty

		for _, uncleId := range cur.Side.Uncles {
			if uncle := getByTemplateId(uncleId); uncle == nil {
				//cannot find uncles
				return nil, 0
			} else {
				// Skip uncles which are already out of PPLNS window
				if (tip.Side.Height - uncle.Side.Height) >= consensus.ChainWindowSize {
					continue
				}

				// Take some % of uncle's weight into this share
				unclePenalty := uncle.Side.Difficulty.Mul64(consensus.UnclePenalty).Div64(100)
				uncleWeight := uncle.Side.Difficulty.Sub(unclePenalty)
				newPplnsWeight := pplnsWeight.Add(uncleWeight)

				// Skip uncles that push PPLNS weight above the limit
				if newPplnsWeight.Cmp(maxPplnsWeight) > 0 {
					continue
				}
				curWeight = curWeight.Add(unclePenalty)

				insertSet(uncleWeight, uncle.GetAddress())

				pplnsWeight = newPplnsWeight
			}
		}

		// Always add non-uncle shares even if PPLNS weight goes above the limit
		insertSet(curWeight, cur.GetAddress())

		pplnsWeight = pplnsWeight.Add(curWeight)

		// One non-uncle share can go above the limit, but it will also guarantee that "shares" is never empty
		if pplnsWeight.Cmp(maxPplnsWeight) > 0 {
			break
		}

		blockDepth++

		if blockDepth >= consensus.ChainWindowSize {
			break
		}

		// Reached the genesis block so we're done
		if cur.Side.Height == 0 {
			break
		}

		cur = getByTemplateId(cur.Side.Parent)

		if cur == nil {
			return nil, 0
		}
	}

	bottomHeight = cur.Side.Height

	for _, share := range sharesSet {
		insertPreAllocated(share)
	}

	shares = preAllocatedShares[:index]

	// Combine shares with the same wallet addresses
	slices.SortFunc(shares, func(a *Share, b *Share) bool {
		return a.Address.Compare(&b.Address) < 0
	})

	n := len(shares)

	//Shuffle shares
	if sidechainVersion > ShareVersion_V1 && n > 1 {
		h := crypto.PooledKeccak256(tip.Side.CoinbasePrivateKeySeed[:])
		seed := binary.LittleEndian.Uint64(h[:])

		if seed == 0 {
			seed = 1
		}

		for i := 0; i < (n - 1); i++ {
			seed = utils.XorShift64Star(seed)
			k := int(uint128.From64(seed).Mul64(uint64(n - i)).Hi)
			//swap
			shares[i], shares[i+k] = shares[i+k], shares[i]
		}
	}

	return shares, bottomHeight
}

func GetDifficulty(tip *PoolBlock, consensus *Consensus, getByTemplateId GetByTemplateIdFunc) types.Difficulty {
	difficultyData := make([]DifficultyData, 0, consensus.ChainWindowSize*2)
	cur := tip
	var blockDepth uint64
	var oldestTimestamp uint64 = math.MaxUint64
	for {
		oldestTimestamp = utils.Min(oldestTimestamp, cur.Main.Timestamp)
		difficultyData = append(difficultyData, DifficultyData{CumulativeDifficulty: cur.Side.CumulativeDifficulty, Timestamp: cur.Main.Timestamp})

		for _, uncleId := range cur.Side.Uncles {
			if uncle := getByTemplateId(uncleId); uncle == nil {
				//cannot find uncles
				return types.ZeroDifficulty
			} else {
				// Skip uncles which are already out of PPLNS window
				if (tip.Side.Height - uncle.Side.Height) >= consensus.ChainWindowSize {
					continue
				}

				oldestTimestamp = utils.Min(oldestTimestamp, uncle.Main.Timestamp)
				difficultyData = append(difficultyData, DifficultyData{CumulativeDifficulty: uncle.Side.CumulativeDifficulty, Timestamp: uncle.Main.Timestamp})
			}
		}

		blockDepth++

		if blockDepth >= consensus.ChainWindowSize {
			break
		}

		// Reached the genesis block so we're done
		if cur.Side.Height == 0 {
			break
		}

		cur = getByTemplateId(cur.Side.Parent)

		if cur == nil {
			return types.ZeroDifficulty
		}
	}

	// Discard 10% oldest and 10% newest (by timestamp) blocks
	tmpTimestamps := make([]uint32, 0, len(difficultyData))
	for i := range difficultyData {
		tmpTimestamps = append(tmpTimestamps, uint32(difficultyData[i].Timestamp-oldestTimestamp))
	}

	cutSize := (len(difficultyData) + 9) / 10
	index1 := cutSize - 1
	index2 := len(difficultyData) - cutSize

	//TODO: replace this with introspective selection, use order for now
	slices.Sort(tmpTimestamps)

	timestamp1 := oldestTimestamp + uint64(tmpTimestamps[index1])
	timestamp2 := oldestTimestamp + uint64(tmpTimestamps[index2])

	deltaT := uint64(1)
	if timestamp2 > timestamp1 {
		deltaT = timestamp2 - timestamp1
	}

	var diff1 = types.Difficulty{Hi: math.MaxUint64, Lo: math.MaxUint64}
	var diff2 types.Difficulty

	for i := range difficultyData {
		d := &difficultyData[i]
		if timestamp1 <= d.Timestamp && d.Timestamp <= timestamp2 {
			if d.CumulativeDifficulty.Cmp(diff1) < 0 {
				diff1 = d.CumulativeDifficulty
			}
			if diff2.Cmp(d.CumulativeDifficulty) < 0 {
				diff2 = d.CumulativeDifficulty
			}
		}
	}

	deltaDiff := diff2.Sub(diff1)

	curDifficulty := deltaDiff.Mul64(consensus.TargetBlockTime).Div64(deltaT)

	if curDifficulty.Cmp64(consensus.MinimumDifficulty) < 0 {
		curDifficulty = types.DifficultyFrom64(consensus.MinimumDifficulty)
	}
	return curDifficulty
}

func SplitReward(reward uint64, shares Shares) (rewards []uint64) {
	var totalWeight types.Difficulty
	for i := range shares {
		totalWeight = totalWeight.Add(shares[i].Weight)
	}

	if totalWeight.Equals64(0) {
		//TODO: err
		return nil
	}

	rewards = make([]uint64, len(shares))

	var w types.Difficulty
	var rewardGiven uint64

	for i := range shares {
		w = w.Add(shares[i].Weight)
		nextValue := w.Mul64(reward).Div(totalWeight)
		rewards[i] = nextValue.Lo - rewardGiven
		rewardGiven = nextValue.Lo
	}

	// Double check that we gave out the exact amount
	rewardGiven = 0
	for _, r := range rewards {
		rewardGiven += r
	}
	if rewardGiven != reward {
		return nil
	}

	return rewards
}
