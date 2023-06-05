package sidechain

import (
	"encoding/binary"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/block"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/randomx"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/transaction"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"git.gammaspectra.live/P2Pool/sha3"
	"golang.org/x/exp/slices"
	"log"
	"math"
	"math/bits"
)

type GetByMainIdFunc func(h types.Hash) *PoolBlock
type GetByMainHeightFunc func(height uint64) UniquePoolBlockSlice
type GetByTemplateIdFunc func(h types.Hash) *PoolBlock
type GetBySideHeightFunc func(height uint64) UniquePoolBlockSlice

// GetChainMainByHashFunc if h = types.ZeroHash, return tip
type GetChainMainByHashFunc func(h types.Hash) *ChainMain

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

	var hashers []*sha3.HasherState

	defer func() {
		for _, h := range hashers {
			crypto.PutKeccak256Hasher(h)
		}
	}()

	utils.SplitWork(-2, n, func(workIndex uint64, workerIndex int) error {
		output := transaction.Output{
			Index: workIndex,
			Type:  txType,
		}
		output.Reward = tmpRewards[output.Index]
		output.EphemeralPublicKey, output.ViewTag = derivationCache.GetEphemeralPublicKey(&tmpShares[output.Index].Address, txPrivateKeySlice, txPrivateKeyScalar, output.Index, hashers[workerIndex])

		outputs[output.Index] = output

		return nil
	}, func(routines, routineIndex int) error {
		hashers = append(hashers, crypto.GetKeccak256Hasher())
		return nil
	}, nil)

	return outputs, bottomHeight
}

type PoolBlockWindowSlot struct {
	Block *PoolBlock
	// Uncles that count for the window weight
	Uncles UniquePoolBlockSlice
}

type PoolBlockWindowAddWeightFunc func(b *PoolBlock, weight types.Difficulty)

func IterateBlocksInPPLNSWindow(tip *PoolBlock, consensus *Consensus, difficultyByHeight block.GetDifficultyByHeightFunc, getByTemplateId GetByTemplateIdFunc, addWeightFunc PoolBlockWindowAddWeightFunc, slotFunc func(slot PoolBlockWindowSlot)) error {

	cur := tip

	var blockDepth uint64

	var mainchainDiff types.Difficulty

	if tip.Side.Parent != types.ZeroHash {
		seedHeight := randomx.SeedHeight(tip.Main.Coinbase.GenHeight)
		mainchainDiff = difficultyByHeight(seedHeight)
		if mainchainDiff == types.ZeroDifficulty {
			return fmt.Errorf("couldn't get mainchain difficulty for height = %d", seedHeight)
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

	for {
		curEntry := PoolBlockWindowSlot{
			Block: cur,
		}
		curWeight := cur.Side.Difficulty

		for _, uncleId := range cur.Side.Uncles {
			if uncle := getByTemplateId(uncleId); uncle == nil {
				//cannot find uncles
				return fmt.Errorf("could not find uncle %s", uncleId.String())
			} else {
				//Needs to be added regardless - for other consumers
				curEntry.Uncles = append(curEntry.Uncles, uncle)

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

				if addWeightFunc != nil {
					addWeightFunc(uncle, uncleWeight)
				}

				pplnsWeight = newPplnsWeight
			}
		}

		// Always add non-uncle shares even if PPLNS weight goes above the limit
		slotFunc(curEntry)

		if addWeightFunc != nil {
			addWeightFunc(cur, curWeight)
		}

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

		parentId := cur.Side.Parent
		cur = getByTemplateId(parentId)

		if cur == nil {
			return fmt.Errorf("could not find parent %s", parentId.String())
		}
	}
	return nil
}

func BlocksInPPLNSWindow(tip *PoolBlock, consensus *Consensus, difficultyByHeight block.GetDifficultyByHeightFunc, getByTemplateId GetByTemplateIdFunc, addWeightFunc PoolBlockWindowAddWeightFunc) (bottomHeight uint64, err error) {

	cur := tip

	var blockDepth uint64

	var mainchainDiff types.Difficulty

	if tip.Side.Parent != types.ZeroHash {
		seedHeight := randomx.SeedHeight(tip.Main.Coinbase.GenHeight)
		mainchainDiff = difficultyByHeight(seedHeight)
		if mainchainDiff == types.ZeroDifficulty {
			return 0, fmt.Errorf("couldn't get mainchain difficulty for height = %d", seedHeight)
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

	for {
		curWeight := cur.Side.Difficulty

		for _, uncleId := range cur.Side.Uncles {
			if uncle := getByTemplateId(uncleId); uncle == nil {
				//cannot find uncles
				return 0, fmt.Errorf("could not find uncle %s", uncleId.String())
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

				if addWeightFunc != nil {
					addWeightFunc(uncle, uncleWeight)
				}

				pplnsWeight = newPplnsWeight
			}
		}

		// Always add non-uncle shares even if PPLNS weight goes above the limit
		bottomHeight = cur.Side.Height

		if addWeightFunc != nil {
			addWeightFunc(cur, curWeight)
		}

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

		parentId := cur.Side.Parent
		cur = getByTemplateId(parentId)

		if cur == nil {
			return 0, fmt.Errorf("could not find parent %s", parentId.String())
		}
	}
	return bottomHeight, nil
}

func GetSharesOrdered(tip *PoolBlock, consensus *Consensus, difficultyByHeight block.GetDifficultyByHeightFunc, getByTemplateId GetByTemplateIdFunc, preAllocatedShares Shares) (shares Shares, bottomHeight uint64) {
	index := 0
	l := len(preAllocatedShares)

	insertShare := func(weight types.Difficulty, a address.PackedAddress) {
		if index < l {
			preAllocatedShares[index].Address = a
			preAllocatedShares[index].Weight = weight
		} else {
			preAllocatedShares = append(preAllocatedShares, &Share{
				Address: a,
				Weight:  weight,
			})
		}
		index++
	}

	if bottomHeight, err := BlocksInPPLNSWindow(tip, consensus, difficultyByHeight, getByTemplateId, func(b *PoolBlock, weight types.Difficulty) {
		insertShare(weight, b.GetAddress())
	}); err != nil {
		return nil, 0
	} else {
		shares = preAllocatedShares[:index]

		//remove dupes
		shares = shares.Compact()

		return shares, bottomHeight
	}
}

func GetShares(tip *PoolBlock, consensus *Consensus, difficultyByHeight block.GetDifficultyByHeightFunc, getByTemplateId GetByTemplateIdFunc, preAllocatedShares Shares) (shares Shares, bottomHeight uint64) {
	shares, bottomHeight = GetSharesOrdered(tip, consensus, difficultyByHeight, getByTemplateId, preAllocatedShares)
	if shares == nil {
		return
	}

	//Shuffle shares
	ShuffleShares(shares, tip.ShareVersion(), tip.Side.CoinbasePrivateKeySeed)

	return shares, bottomHeight
}

// ShuffleShares Shuffles shares according to consensus parameters via ShuffleSequence. Requires pre-sorted shares based on address
func ShuffleShares[T any](shares []T, shareVersion ShareVersion, privateKeySeed types.Hash) {
	ShuffleSequence(shareVersion, privateKeySeed, len(shares), func(i, j int) {
		shares[i], shares[j] = shares[j], shares[i]
	})
}

// ShuffleSequence Iterates through a swap sequence according to consensus parameters.
func ShuffleSequence(shareVersion ShareVersion, privateKeySeed types.Hash, items int, swap func(i, j int)) {
	n := uint64(items)
	if shareVersion > ShareVersion_V1 && n > 1 {
		h := crypto.PooledKeccak256(privateKeySeed[:])
		seed := binary.LittleEndian.Uint64(h[:])

		if seed == 0 {
			seed = 1
		}

		for i := uint64(0); i < (n - 1); i++ {
			seed = utils.XorShift64Star(seed)
			k, _ := bits.Mul64(seed, n-i)
			//swap
			swap(int(i), int(i+k))
		}
	}
}

func GetDifficulty(tip *PoolBlock, consensus *Consensus, getByTemplateId GetByTemplateIdFunc, preAllocatedDifficultyData []DifficultyData, preAllocatedTimestampDifferences []uint32) (difficulty types.Difficulty, verifyError, invalidError error) {
	index := 0
	l := len(preAllocatedDifficultyData)

	insertDifficultyData := func(cumDiff types.Difficulty, timestamp uint64) {
		if index < l {
			preAllocatedDifficultyData[index].CumulativeDifficulty = cumDiff
			preAllocatedDifficultyData[index].Timestamp = timestamp
		} else {
			preAllocatedDifficultyData = append(preAllocatedDifficultyData, DifficultyData{
				CumulativeDifficulty: cumDiff,
				Timestamp:            timestamp,
			})
		}
		index++
	}

	cur := tip
	var blockDepth uint64
	var oldestTimestamp uint64 = math.MaxUint64
	for {
		oldestTimestamp = utils.Min(oldestTimestamp, cur.Main.Timestamp)
		insertDifficultyData(cur.Side.CumulativeDifficulty, cur.Main.Timestamp)

		for _, uncleId := range cur.Side.Uncles {
			if uncle := getByTemplateId(uncleId); uncle == nil {
				//cannot find uncles
				return types.ZeroDifficulty, fmt.Errorf("could not find uncle %s", uncleId), nil
			} else {
				// Skip uncles which are already out of PPLNS window
				if (tip.Side.Height - uncle.Side.Height) >= consensus.ChainWindowSize {
					continue
				}

				oldestTimestamp = utils.Min(oldestTimestamp, uncle.Main.Timestamp)
				insertDifficultyData(uncle.Side.CumulativeDifficulty, uncle.Main.Timestamp)
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

		parentId := cur.Side.Parent
		cur = getByTemplateId(parentId)

		if cur == nil {
			return types.ZeroDifficulty, fmt.Errorf("could not find parent %s", parentId), nil
		}
	}

	difficultyData := preAllocatedDifficultyData[:index]

	if len(difficultyData) > len(preAllocatedTimestampDifferences) {
		preAllocatedTimestampDifferences = make([]uint32, len(difficultyData))
	}

	// Discard 10% oldest and 10% newest (by timestamp) blocks
	for i := range difficultyData {
		preAllocatedTimestampDifferences[i] = uint32(difficultyData[i].Timestamp - oldestTimestamp)
	}
	tmpTimestamps := preAllocatedTimestampDifferences[:len(difficultyData)]

	cutSize := (len(difficultyData) + 9) / 10
	index1 := cutSize - 1
	index2 := len(difficultyData) - cutSize

	//TODO: replace this with introspective selection, use order for now
	slices.Sort(tmpTimestamps)

	timestamp1 := oldestTimestamp + uint64(tmpTimestamps[index1])
	timestamp2 := oldestTimestamp + uint64(tmpTimestamps[index2])

	// Make a reasonable assumption that each block has higher timestamp, so deltaT can't be less than deltaIndex
	// Because if it is, someone is trying to mess with timestamps
	// In reality, deltaT ~ deltaIndex*10 (sidechain block time)
	deltaIndex := uint64(1)
	if index2 > index2 {
		deltaIndex = uint64(index2 - index1)
	}
	deltaT := deltaIndex
	if timestamp2 > (timestamp1 + deltaIndex) {
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
	return curDifficulty, nil, nil
}

func SplitRewardNoAllocate(preAllocatedRewards []uint64, reward uint64, shares Shares) (rewards []uint64) {
	var totalWeight types.Difficulty
	for i := range shares {
		totalWeight = totalWeight.Add(shares[i].Weight)
	}

	if totalWeight.Equals64(0) {
		//TODO: err
		return nil
	}

	rewards = preAllocatedRewards

	var w types.Difficulty
	var rewardGiven uint64

	for i := range shares {
		w = w.Add(shares[i].Weight)
		nextValue := w.Mul64(reward).Div(totalWeight)
		rewards = append(rewards, nextValue.Lo-rewardGiven)
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

func SplitReward(reward uint64, shares Shares) (rewards []uint64) {
	return SplitRewardNoAllocate(make([]uint64, 0, len(shares)), reward, shares)
}

func IsLongerChain(block, candidate *PoolBlock, consensus *Consensus, getByTemplateId GetByTemplateIdFunc, getChainMainByHash GetChainMainByHashFunc) (isLonger, isAlternative bool) {
	if candidate == nil || !candidate.Verified.Load() || candidate.Invalid.Load() {
		return false, false
	}

	// Switching from an empty to a non-empty chain
	if block == nil {
		return true, true
	}

	// If these two blocks are on the same chain, they must have a common ancestor

	blockAncestor := block
	for blockAncestor != nil && blockAncestor.Side.Height > candidate.Side.Height {
		blockAncestor = getByTemplateId(blockAncestor.Side.Parent)
		//TODO: err on blockAncestor nil
	}

	if blockAncestor != nil {
		candidateAncestor := candidate
		for candidateAncestor != nil && candidateAncestor.Side.Height > blockAncestor.Side.Height {
			candidateAncestor = getByTemplateId(candidateAncestor.Side.Parent)
			//TODO: err on candidateAncestor nil
		}

		for blockAncestor != nil && candidateAncestor != nil {
			if blockAncestor.Side.Parent.Equals(candidateAncestor.Side.Parent) {
				return block.Side.CumulativeDifficulty.Cmp(candidate.Side.CumulativeDifficulty) < 0, false
			}
			blockAncestor = getByTemplateId(blockAncestor.Side.Parent)
			candidateAncestor = getByTemplateId(candidateAncestor.Side.Parent)
		}
	}

	// They're on totally different chains. Compare total difficulties over the last m_chainWindowSize blocks

	var blockTotalDiff, candidateTotalDiff types.Difficulty

	oldChain := block
	newChain := candidate

	var candidateMainchainHeight, candidateMainchainMinHeight uint64

	var moneroBlocksReserve = consensus.ChainWindowSize * consensus.TargetBlockTime * 2 / monero.BlockTime
	currentChainMoneroBlocks, candidateChainMoneroBlocks := make([]types.Hash, 0, moneroBlocksReserve), make([]types.Hash, 0, moneroBlocksReserve)

	for i := uint64(0); i < consensus.ChainWindowSize && (oldChain != nil || newChain != nil); i++ {
		if oldChain != nil {
			blockTotalDiff = blockTotalDiff.Add(oldChain.Side.Difficulty)
			for _, uncle := range oldChain.Side.Uncles {
				if u := getByTemplateId(uncle); u != nil {
					blockTotalDiff = blockTotalDiff.Add(u.Side.Difficulty)
				}
			}
			if !slices.Contains(currentChainMoneroBlocks, oldChain.Main.PreviousId) && getChainMainByHash(oldChain.Main.PreviousId) != nil {
				currentChainMoneroBlocks = append(currentChainMoneroBlocks, oldChain.Main.PreviousId)
			}
			oldChain = getByTemplateId(oldChain.Side.Parent)
		}

		if newChain != nil {
			if candidateMainchainMinHeight != 0 {
				candidateMainchainMinHeight = utils.Min(candidateMainchainMinHeight, newChain.Main.Coinbase.GenHeight)
			} else {
				candidateMainchainMinHeight = newChain.Main.Coinbase.GenHeight
			}
			candidateTotalDiff = candidateTotalDiff.Add(newChain.Side.Difficulty)
			for _, uncle := range newChain.Side.Uncles {
				if u := getByTemplateId(uncle); u != nil {
					candidateTotalDiff = candidateTotalDiff.Add(u.Side.Difficulty)
				}
			}
			if !slices.Contains(candidateChainMoneroBlocks, newChain.Main.PreviousId) {
				if data := getChainMainByHash(newChain.Main.PreviousId); data != nil {
					candidateChainMoneroBlocks = append(candidateChainMoneroBlocks, newChain.Main.PreviousId)
					candidateMainchainHeight = utils.Max(candidateMainchainHeight, data.Height)
				}
			}

			newChain = getByTemplateId(newChain.Side.Parent)
		}
	}

	if blockTotalDiff.Cmp(candidateTotalDiff) >= 0 {
		return false, true
	}

	// Candidate chain must be built on top of recent mainchain blocks
	if headerTip := getChainMainByHash(types.ZeroHash); headerTip != nil {
		if candidateMainchainHeight+10 < headerTip.Height {
			log.Printf("[SideChain] Received a longer alternative chain but it's stale: height %d, current height %d", candidateMainchainHeight, headerTip.Height)
			return false, true
		}

		limit := consensus.ChainWindowSize * 4 * consensus.TargetBlockTime / monero.BlockTime
		if candidateMainchainMinHeight+limit < headerTip.Height {
			log.Printf("[SideChain] Received a longer alternative chain but it's stale: min height %d, must be >= %d", candidateMainchainMinHeight, headerTip.Height-limit)
			return false, true
		}

		// Candidate chain must have been mined on top of at least half as many known Monero blocks, compared to the current chain
		if len(candidateChainMoneroBlocks)*2 < len(currentChainMoneroBlocks) {
			log.Printf("[SideChain] Received a longer alternative chain but it wasn't mined on current Monero blockchain: only %d / %d blocks found", len(candidateChainMoneroBlocks), len(currentChainMoneroBlocks))
			return false, true
		}

		return true, true
	} else {
		return false, true
	}
}
