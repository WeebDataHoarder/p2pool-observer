package sidechain

import (
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/block"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/randomx"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/transaction"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"git.gammaspectra.live/P2Pool/sha3"
	"lukechampine.com/uint128"
	"math"
	"math/bits"
	"slices"
)

type GetByMainIdFunc func(h types.Hash) *PoolBlock
type GetByMainHeightFunc func(height uint64) UniquePoolBlockSlice
type GetByTemplateIdFunc func(h types.Hash) *PoolBlock
type GetBySideHeightFunc func(height uint64) UniquePoolBlockSlice

// GetChainMainByHashFunc if h = types.ZeroHash, return tip
type GetChainMainByHashFunc func(h types.Hash) *ChainMain

func CalculateOutputs(block *PoolBlock, consensus *Consensus, difficultyByHeight block.GetDifficultyByHeightFunc, getByTemplateId GetByTemplateIdFunc, derivationCache DerivationCacheInterface, preAllocatedShares Shares, preAllocatedRewards []uint64) (outputs transaction.Outputs, bottomHeight uint64) {
	tmpShares, bottomHeight := GetShares(block, consensus, difficultyByHeight, getByTemplateId, preAllocatedShares)
	if preAllocatedRewards == nil {
		preAllocatedRewards = make([]uint64, 0, len(tmpShares))
	}
	tmpRewards := SplitReward(preAllocatedRewards, block.Main.Coinbase.TotalReward, tmpShares)

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

		if err := cur.iteratorUncles(getByTemplateId, func(uncle *PoolBlock) {
			//Needs to be added regardless - for other consumers
			curEntry.Uncles = append(curEntry.Uncles, uncle)

			// Skip uncles which are already out of PPLNS window
			if (tip.Side.Height - uncle.Side.Height) >= consensus.ChainWindowSize {
				return
			}

			// Take some % of uncle's weight into this share
			uncleWeight, unclePenalty := consensus.ApplyUnclePenalty(uncle.Side.Difficulty)
			newPplnsWeight := pplnsWeight.Add(uncleWeight)

			// Skip uncles that push PPLNS weight above the limit
			if newPplnsWeight.Cmp(maxPplnsWeight) > 0 {
				return
			}
			curWeight = curWeight.Add(unclePenalty)

			if addWeightFunc != nil {
				addWeightFunc(uncle, uncleWeight)
			}

			pplnsWeight = newPplnsWeight
		}); err != nil {
			return err
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
		cur = cur.iteratorGetParent(getByTemplateId)

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

		if err := cur.iteratorUncles(getByTemplateId, func(uncle *PoolBlock) {
			// Skip uncles which are already out of PPLNS window
			if (tip.Side.Height - uncle.Side.Height) >= consensus.ChainWindowSize {
				return
			}

			// Take some % of uncle's weight into this share
			uncleWeight, unclePenalty := consensus.ApplyUnclePenalty(uncle.Side.Difficulty)

			newPplnsWeight := pplnsWeight.Add(uncleWeight)

			// Skip uncles that push PPLNS weight above the limit
			if newPplnsWeight.Cmp(maxPplnsWeight) > 0 {
				return
			}
			curWeight = curWeight.Add(unclePenalty)

			addWeightFunc(uncle, uncleWeight)

			pplnsWeight = newPplnsWeight

		}); err != nil {
			return 0, err
		}

		// Always add non-uncle shares even if PPLNS weight goes above the limit
		bottomHeight = cur.Side.Height

		addWeightFunc(cur, curWeight)

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
		cur = cur.iteratorGetParent(getByTemplateId)

		if cur == nil {
			return 0, fmt.Errorf("could not find parent %s", parentId.String())
		}
	}
	return bottomHeight, nil
}

func GetSharesOrdered(tip *PoolBlock, consensus *Consensus, difficultyByHeight block.GetDifficultyByHeightFunc, getByTemplateId GetByTemplateIdFunc, preAllocatedShares Shares) (shares Shares, bottomHeight uint64) {
	index := 0
	l := len(preAllocatedShares)

	if bottomHeight, err := BlocksInPPLNSWindow(tip, consensus, difficultyByHeight, getByTemplateId, func(b *PoolBlock, weight types.Difficulty) {
		if index < l {
			preAllocatedShares[index].Address = b.Side.PublicKey

			preAllocatedShares[index].Weight = weight
		} else {
			preAllocatedShares = append(preAllocatedShares, &Share{
				Address: b.Side.PublicKey,
				Weight:  weight,
			})
		}
		index++
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
		seed := crypto.PooledKeccak256(privateKeySeed[:]).Uint64()

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

type DifficultyData struct {
	cumulativeDifficulty types.Difficulty
	timestamp            uint64
}

// GetDifficultyForNextBlock Gets the difficulty at tip (the next block will require this difficulty)
// preAllocatedDifficultyData should contain enough capacity to fit all entries to iterate through.
// preAllocatedTimestampDifferences should contain enough capacity to fit all differences.
//
// Ported from SideChain::get_difficulty() from C p2pool,
// somewhat based on Blockchain::get_difficulty_for_next_block() from Monero with the addition of uncles
func GetDifficultyForNextBlock(tip *PoolBlock, consensus *Consensus, getByTemplateId GetByTemplateIdFunc, preAllocatedDifficultyData []DifficultyData, preAllocatedTimestampData []uint64) (difficulty types.Difficulty, verifyError, invalidError error) {

	difficultyData := preAllocatedDifficultyData[:0]

	timestampData := preAllocatedTimestampData[:0]

	cur := tip
	var blockDepth uint64

	for {
		difficultyData = append(difficultyData, DifficultyData{
			cumulativeDifficulty: cur.Side.CumulativeDifficulty,
			timestamp:            cur.Main.Timestamp,
		})

		timestampData = append(timestampData, cur.Main.Timestamp)

		if err := cur.iteratorUncles(getByTemplateId, func(uncle *PoolBlock) {
			// Skip uncles which are already out of PPLNS window
			if (tip.Side.Height - uncle.Side.Height) >= consensus.ChainWindowSize {
				return
			}

			difficultyData = append(difficultyData, DifficultyData{
				cumulativeDifficulty: uncle.Side.CumulativeDifficulty,
				timestamp:            uncle.Main.Timestamp,
			})

			timestampData = append(timestampData, uncle.Main.Timestamp)
		}); err != nil {
			return types.ZeroDifficulty, err, nil
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
		cur = cur.iteratorGetParent(getByTemplateId)

		if cur == nil {
			return types.ZeroDifficulty, fmt.Errorf("could not find parent %s", parentId.String()), nil
		}
	}

	difficulty, invalidError = NextDifficulty(consensus, timestampData, difficultyData)
	return
}

// NextDifficulty returns the next block difficulty based on gathered timestamp/difficulty data
// Returns error on wrap/overflow/underflow on uint128 operations
func NextDifficulty(consensus *Consensus, timestamps []uint64, difficultyData []DifficultyData) (nextDifficulty types.Difficulty, err error) {
	// Discard 10% oldest and 10% newest (by timestamp) blocks

	cutSize := (len(timestamps) + 9) / 10
	lowIndex := cutSize - 1
	upperIndex := len(timestamps) - cutSize

	utils.NthElementSlice(timestamps, lowIndex)
	timestampLowerBound := timestamps[lowIndex]

	utils.NthElementSlice(timestamps, upperIndex)
	timestampUpperBound := timestamps[upperIndex]

	// Make a reasonable assumption that each block has higher timestamp, so deltaTimestamp can't be less than deltaIndex
	// Because if it is, someone is trying to mess with timestamps
	// In reality, deltaTimestamp ~ deltaIndex*10 (sidechain block time)
	deltaIndex := uint64(1)
	if upperIndex > lowIndex {
		deltaIndex = uint64(upperIndex - lowIndex)
	}
	deltaTimestamp := deltaIndex
	if timestampUpperBound > (timestampLowerBound + deltaIndex) {
		deltaTimestamp = timestampUpperBound - timestampLowerBound
	}

	var minDifficulty = types.Difficulty{Hi: math.MaxUint64, Lo: math.MaxUint64}
	var maxDifficulty types.Difficulty

	for i := range difficultyData {
		// Pick only the cumulative difficulty from specifically the entries that are within the timestamp upper and low bounds
		if timestampLowerBound <= difficultyData[i].timestamp && difficultyData[i].timestamp <= timestampUpperBound {
			if minDifficulty.Cmp(difficultyData[i].cumulativeDifficulty) > 0 {
				minDifficulty = difficultyData[i].cumulativeDifficulty
			}
			if maxDifficulty.Cmp(difficultyData[i].cumulativeDifficulty) < 0 {
				maxDifficulty = difficultyData[i].cumulativeDifficulty
			}
		}
	}

	// Specific section that could wrap and needs to be detected
	// Use calls that panic on wrap/overflow/underflow
	{
		defer func() {
			if e := recover(); e != nil {
				if panicError, ok := e.(error); ok {
					err = fmt.Errorf("panic in NextDifficulty, wrap occured?: %w", panicError)
				} else {
					err = fmt.Errorf("panic in NextDifficulty, wrap occured?: %v", e)
				}
			}
		}()

		deltaDifficulty := uint128.Uint128(maxDifficulty).Sub(uint128.Uint128(minDifficulty))
		curDifficulty := deltaDifficulty.Mul64(consensus.TargetBlockTime).Div64(deltaTimestamp)

		if curDifficulty.Cmp64(consensus.MinimumDifficulty) < 0 {
			return types.DifficultyFrom64(consensus.MinimumDifficulty), nil
		}
		return types.Difficulty(curDifficulty), nil
	}
}

func SplitRewardAllocate(reward uint64, shares Shares) (rewards []uint64) {
	return SplitReward(make([]uint64, 0, len(shares)), reward, shares)
}

func SplitReward(preAllocatedRewards []uint64, reward uint64, shares Shares) (rewards []uint64) {
	var totalWeight types.Difficulty
	for i := range shares {
		totalWeight = totalWeight.Add(shares[i].Weight)
	}

	if totalWeight.Equals64(0) {
		//TODO: err
		return nil
	}

	rewards = preAllocatedRewards[:0]

	var w types.Difficulty
	var rewardGiven uint64

	for _, share := range shares {
		w = w.Add(share.Weight)
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
		blockAncestor = blockAncestor.iteratorGetParent(getByTemplateId)
		//TODO: err on blockAncestor nil
	}

	if blockAncestor != nil {
		candidateAncestor := candidate
		for candidateAncestor != nil && candidateAncestor.Side.Height > blockAncestor.Side.Height {
			candidateAncestor = candidateAncestor.iteratorGetParent(getByTemplateId)
			//TODO: err on candidateAncestor nil
		}

		for blockAncestor != nil && candidateAncestor != nil {
			if blockAncestor.Side.Parent == candidateAncestor.Side.Parent {
				return block.Side.CumulativeDifficulty.Cmp(candidate.Side.CumulativeDifficulty) < 0, false
			}
			blockAncestor = blockAncestor.iteratorGetParent(getByTemplateId)
			candidateAncestor = candidateAncestor.iteratorGetParent(getByTemplateId)
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
			_ = oldChain.iteratorUncles(getByTemplateId, func(uncle *PoolBlock) {
				blockTotalDiff = blockTotalDiff.Add(uncle.Side.Difficulty)
			})
			if !slices.Contains(currentChainMoneroBlocks, oldChain.Main.PreviousId) && getChainMainByHash(oldChain.Main.PreviousId) != nil {
				currentChainMoneroBlocks = append(currentChainMoneroBlocks, oldChain.Main.PreviousId)
			}
			oldChain = oldChain.iteratorGetParent(getByTemplateId)
		}

		if newChain != nil {
			if candidateMainchainMinHeight != 0 {
				candidateMainchainMinHeight = min(candidateMainchainMinHeight, newChain.Main.Coinbase.GenHeight)
			} else {
				candidateMainchainMinHeight = newChain.Main.Coinbase.GenHeight
			}
			candidateTotalDiff = candidateTotalDiff.Add(newChain.Side.Difficulty)
			_ = newChain.iteratorUncles(getByTemplateId, func(uncle *PoolBlock) {
				candidateTotalDiff = candidateTotalDiff.Add(uncle.Side.Difficulty)
			})
			if !slices.Contains(candidateChainMoneroBlocks, newChain.Main.PreviousId) {
				if data := getChainMainByHash(newChain.Main.PreviousId); data != nil {
					candidateChainMoneroBlocks = append(candidateChainMoneroBlocks, newChain.Main.PreviousId)
					candidateMainchainHeight = max(candidateMainchainHeight, data.Height)
				}
			}

			newChain = newChain.iteratorGetParent(getByTemplateId)
		}
	}

	if blockTotalDiff.Cmp(candidateTotalDiff) >= 0 {
		return false, true
	}

	// Candidate chain must be built on top of recent mainchain blocks
	if headerTip := getChainMainByHash(types.ZeroHash); headerTip != nil {
		if candidateMainchainHeight+10 < headerTip.Height {
			utils.Logf("[SideChain] Received a longer alternative chain but it's stale: height %d, current height %d", candidateMainchainHeight, headerTip.Height)
			return false, true
		}

		limit := consensus.ChainWindowSize * 4 * consensus.TargetBlockTime / monero.BlockTime
		if candidateMainchainMinHeight+limit < headerTip.Height {
			utils.Logf("[SideChain] Received a longer alternative chain but it's stale: min height %d, must be >= %d", candidateMainchainMinHeight, headerTip.Height-limit)
			return false, true
		}

		// Candidate chain must have been mined on top of at least half as many known Monero blocks, compared to the current chain
		if len(candidateChainMoneroBlocks)*2 < len(currentChainMoneroBlocks) {
			utils.Logf("[SideChain] Received a longer alternative chain but it wasn't mined on current Monero blockchain: only %d / %d blocks found", len(candidateChainMoneroBlocks), len(currentChainMoneroBlocks))
			return false, true
		}

		return true, true
	} else {
		return false, true
	}
}
