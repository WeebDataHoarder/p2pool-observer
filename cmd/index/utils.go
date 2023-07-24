package index

import (
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/block"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/randomx"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/transaction"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"git.gammaspectra.live/P2Pool/sha3"
	"slices"
)

type GetByTemplateIdFunc func(h types.Hash) *SideBlock
type GetUnclesByTemplateIdFunc func(h types.Hash) chan *SideBlock
type SideBlockWindowAddWeightFunc func(b *SideBlock, weight types.Difficulty)

type SideBlockWindowSlot struct {
	Block *SideBlock
	// Uncles that count for the window weight
	Uncles []*SideBlock
}

// IterateSideBlocksInPPLNSWindow
// Copy of sidechain.IterateBlocksInPPLNSWindow
func IterateSideBlocksInPPLNSWindow(tip *SideBlock, consensus *sidechain.Consensus, difficultyByHeight block.GetDifficultyByHeightFunc, getByTemplateId GetByTemplateIdFunc, getUnclesByTemplateId GetUnclesByTemplateIdFunc, addWeightFunc SideBlockWindowAddWeightFunc, slotFunc func(slot SideBlockWindowSlot)) error {

	cur := tip

	var blockDepth uint64

	var mainchainDiff types.Difficulty

	if tip.ParentTemplateId != types.ZeroHash {
		seedHeight := randomx.SeedHeight(tip.MainHeight)
		mainchainDiff = difficultyByHeight(seedHeight)
		if mainchainDiff == types.ZeroDifficulty {
			return fmt.Errorf("couldn't get mainchain difficulty for height = %d", seedHeight)
		}
	}

	// Dynamic PPLNS window starting from v2
	// Limit PPLNS weight to 2x of the Monero difficulty (max 2 blocks per PPLNS window on average)
	sidechainVersion := sidechain.P2PoolShareVersion(consensus, tip.Timestamp)

	maxPplnsWeight := types.MaxDifficulty

	if sidechainVersion > sidechain.ShareVersion_V1 {
		maxPplnsWeight = mainchainDiff.Mul64(2)
	}

	var pplnsWeight types.Difficulty

	for {
		curEntry := SideBlockWindowSlot{
			Block: cur,
		}
		curWeight := types.DifficultyFrom64(cur.Difficulty)

		for uncle := range getUnclesByTemplateId(cur.TemplateId) {
			//Needs to be added regardless - for other consumers
			if !slices.ContainsFunc(curEntry.Uncles, func(sideBlock *SideBlock) bool {
				return sideBlock.TemplateId == uncle.TemplateId
			}) {
				curEntry.Uncles = append(curEntry.Uncles, uncle)
			}

			// Skip uncles which are already out of PPLNS window
			if (tip.SideHeight - uncle.SideHeight) >= consensus.ChainWindowSize {
				continue
			}

			// Take some % of uncle's weight into this share
			uncleWeight, unclePenalty := consensus.ApplyUnclePenalty(types.DifficultyFrom64(uncle.Difficulty))
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
		if cur.SideHeight == 0 {
			break
		}

		parentId := cur.ParentTemplateId
		cur = getByTemplateId(parentId)

		if cur == nil {
			return fmt.Errorf("could not find parent %s", parentId.String())
		}
	}

	return nil
}

// BlocksInPPLNSWindow
// Copy of sidechain.BlocksInPPLNSWindow
func BlocksInPPLNSWindow(tip *SideBlock, consensus *sidechain.Consensus, difficultyByHeight block.GetDifficultyByHeightFunc, getByTemplateId GetByTemplateIdFunc, getUnclesByTemplateId GetUnclesByTemplateIdFunc, addWeightFunc SideBlockWindowAddWeightFunc) (bottomHeight uint64, err error) {

	cur := tip

	var blockDepth uint64

	var mainchainDiff types.Difficulty

	if tip.ParentTemplateId != types.ZeroHash {
		seedHeight := randomx.SeedHeight(tip.MainHeight)
		mainchainDiff = difficultyByHeight(seedHeight)
		if mainchainDiff == types.ZeroDifficulty {
			return 0, fmt.Errorf("couldn't get mainchain difficulty for height = %d", seedHeight)
		}
	}

	// Dynamic PPLNS window starting from v2
	// Limit PPLNS weight to 2x of the Monero difficulty (max 2 blocks per PPLNS window on average)
	sidechainVersion := sidechain.P2PoolShareVersion(consensus, tip.Timestamp)

	maxPplnsWeight := types.MaxDifficulty

	if sidechainVersion > sidechain.ShareVersion_V1 {
		maxPplnsWeight = mainchainDiff.Mul64(2)
	}

	var pplnsWeight types.Difficulty

	for {
		curEntry := SideBlockWindowSlot{
			Block: cur,
		}
		curWeight := types.DifficultyFrom64(cur.Difficulty)

		for uncle := range getUnclesByTemplateId(cur.TemplateId) {
			//Needs to be added regardless - for other consumers
			if !slices.ContainsFunc(curEntry.Uncles, func(sideBlock *SideBlock) bool {
				return sideBlock.TemplateId == uncle.TemplateId
			}) {
				curEntry.Uncles = append(curEntry.Uncles, uncle)
			}

			// Skip uncles which are already out of PPLNS window
			if (tip.SideHeight - uncle.SideHeight) >= consensus.ChainWindowSize {
				continue
			}

			// Take some % of uncle's weight into this share
			uncleWeight, unclePenalty := consensus.ApplyUnclePenalty(types.DifficultyFrom64(uncle.Difficulty))
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

		// Always add non-uncle shares even if PPLNS weight goes above the limit
		bottomHeight = cur.SideHeight

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
		if cur.SideHeight == 0 {
			break
		}

		parentId := cur.ParentTemplateId
		cur = getByTemplateId(parentId)

		if cur == nil {
			return 0, fmt.Errorf("could not find parent %s", parentId.String())
		}
	}

	return bottomHeight, nil
}

// GetSharesOrdered
// Copy of sidechain.GetSharesOrdered
func GetSharesOrdered(indexDb *Index, tip *SideBlock, difficultyByHeight block.GetDifficultyByHeightFunc, getByTemplateId GetByTemplateIdFunc, getUnclesByTemplateId GetUnclesByTemplateIdFunc, preAllocatedShares sidechain.Shares) (shares sidechain.Shares, bottomHeight uint64) {
	index := 0
	l := len(preAllocatedShares)

	if bottomHeight, err := BlocksInPPLNSWindow(tip, indexDb.Consensus(), difficultyByHeight, getByTemplateId, getUnclesByTemplateId, func(b *SideBlock, weight types.Difficulty) {
		addr := indexDb.GetMiner(b.Miner).Address().ToPackedAddress()
		if index < l {
			preAllocatedShares[index].Address = addr

			preAllocatedShares[index].Weight = weight
		} else {
			preAllocatedShares = append(preAllocatedShares, &sidechain.Share{
				Address: addr,
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

// GetShares
// Copy of sidechain.GetShares
func GetShares(indexDb *Index, tip *SideBlock, coinbasePrivateKeySeed types.Hash, difficultyByHeight block.GetDifficultyByHeightFunc, getByTemplateId GetByTemplateIdFunc, getUnclesByTemplateId GetUnclesByTemplateIdFunc, preAllocatedShares sidechain.Shares) (shares sidechain.Shares, bottomHeight uint64) {
	shares, bottomHeight = GetSharesOrdered(indexDb, tip, difficultyByHeight, getByTemplateId, getUnclesByTemplateId, preAllocatedShares)
	if shares == nil {
		return
	}

	//Shuffle shares
	sidechain.ShuffleShares(shares, sidechain.P2PoolShareVersion(indexDb.Consensus(), tip.Timestamp), coinbasePrivateKeySeed)

	return shares, bottomHeight
}

// CalculateOutputs
// Copy of sidechain.CalculateOutputs
func CalculateOutputs(indexDb *Index, block *SideBlock, transactionOutputType uint8, totalReward uint64, coinbasePrivateKey crypto.PrivateKey, coinbasePrivateKeySeed types.Hash, difficultyByHeight block.GetDifficultyByHeightFunc, getByTemplateId GetByTemplateIdFunc, getUnclesByTemplateId GetUnclesByTemplateIdFunc, derivationCache sidechain.DerivationCacheInterface, preAllocatedShares sidechain.Shares, preAllocatedRewards []uint64) (outputs transaction.Outputs, bottomHeight uint64) {
	tmpShares, bottomHeight := GetShares(indexDb, block, coinbasePrivateKeySeed, difficultyByHeight, getByTemplateId, getUnclesByTemplateId, preAllocatedShares)
	if preAllocatedRewards == nil {
		preAllocatedRewards = make([]uint64, 0, len(tmpShares))
	}
	tmpRewards := sidechain.SplitReward(preAllocatedRewards, totalReward, tmpShares)

	if tmpShares == nil || tmpRewards == nil || len(tmpRewards) != len(tmpShares) {
		return nil, 0
	}

	n := uint64(len(tmpShares))

	outputs = make(transaction.Outputs, n)

	txType := transactionOutputType

	txPrivateKeySlice := coinbasePrivateKey.AsSlice()
	txPrivateKeyScalar := coinbasePrivateKey.AsScalar()

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
