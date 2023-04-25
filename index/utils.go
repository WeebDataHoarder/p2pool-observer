package index

import (
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/block"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/randomx"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
)

type GetByTemplateIdFunc func(h types.Hash) *SideBlock
type GetUnclesByTemplateIdFunc func(h types.Hash) chan *SideBlock
type SideBlockWindowAddWeightFunc func(b *SideBlock, weight types.Difficulty)

type SideBlockWindowSlot struct {
	Block *SideBlock
	// Uncles that count for the window weight
	Uncles []*SideBlock
}

func IterateSideBlocksInPPLNSWindow(tip *SideBlock, consensus *sidechain.Consensus, difficultyByHeight block.GetDifficultyByHeightFunc, getByTemplateId GetByTemplateIdFunc, getUnclesByTemplateId GetUnclesByTemplateIdFunc, addWeightFunc SideBlockWindowAddWeightFunc, errorFunc func(err error)) (results chan SideBlockWindowSlot) {
	results = make(chan SideBlockWindowSlot)

	go func() {
		defer close(results)

		cur := tip

		var blockDepth uint64

		var mainchainDiff types.Difficulty

		if tip.ParentTemplateId != types.ZeroHash {
			seedHeight := randomx.SeedHeight(tip.MainHeight)
			mainchainDiff = difficultyByHeight(seedHeight)
			if mainchainDiff == types.ZeroDifficulty {
				if errorFunc != nil {
					errorFunc(fmt.Errorf("couldn't get mainchain difficulty for height = %d", seedHeight))
				}
				return
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
				curEntry.Uncles = append(curEntry.Uncles, uncle)

				// Skip uncles which are already out of PPLNS window
				if (tip.SideHeight - uncle.SideHeight) >= consensus.ChainWindowSize {
					continue
				}

				// Take some % of uncle's weight into this share
				unclePenalty := (uncle.Difficulty * consensus.UnclePenalty) / 100
				uncleWeight := uncle.Difficulty - unclePenalty
				newPplnsWeight := pplnsWeight.Add64(uncleWeight)

				// Skip uncles that push PPLNS weight above the limit
				if newPplnsWeight.Cmp(maxPplnsWeight) > 0 {
					continue
				}
				curWeight = curWeight.Add64(unclePenalty)

				if addWeightFunc != nil {
					addWeightFunc(uncle, types.DifficultyFrom64(uncleWeight))
				}

				pplnsWeight = newPplnsWeight
			}

			// Always add non-uncle shares even if PPLNS weight goes above the limit
			results <- curEntry

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
				if errorFunc != nil {
					errorFunc(fmt.Errorf("could not find parent %s", parentId.String()))
				}
				return
			}
		}
	}()

	return results
}
