package sidechain

import (
	"bytes"
	"errors"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero"
	mainblock "git.gammaspectra.live/P2Pool/p2pool-observer/monero/block"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/transaction"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"golang.org/x/exp/slices"
	"math"
	"sync"
	"sync/atomic"
)

type ConsensusBroadcaster interface {
	ConsensusProvider
	Broadcast(block *PoolBlock)
}

type SideChain struct {
	cache  *DerivationCache
	server ConsensusBroadcaster

	blocksByLock       sync.RWMutex
	blocksByTemplateId map[types.Hash]*PoolBlock
	blocksByHeight     map[uint64][]*PoolBlock

	chainTip          atomic.Pointer[PoolBlock]
	currentDifficulty atomic.Pointer[types.Difficulty]
}

func NewSideChain(server ConsensusBroadcaster) *SideChain {
	return &SideChain{
		cache:              NewDerivationCache(),
		server:             server,
		blocksByTemplateId: make(map[types.Hash]*PoolBlock),
		blocksByHeight:     make(map[uint64][]*PoolBlock),
	}
}

func (c *SideChain) Consensus() *Consensus {
	return c.server.Consensus()
}

func (c *SideChain) PreprocessBlock(block *PoolBlock) (err error) {
	c.blocksByLock.RLock()
	defer c.blocksByLock.RUnlock()

	if len(block.Main.Coinbase.Outputs) == 0 {
		if outputs := c.getOutputs(block); outputs == nil {
			return errors.New("nil transaction outputs")
		} else {
			block.Main.Coinbase.Outputs = outputs
		}

		if outputBlob, err := block.Main.Coinbase.OutputsBlob(); err != nil {
			return err
		} else if uint64(len(outputBlob)) != block.Main.Coinbase.OutputsBlobSize {
			return errors.New("invalid output blob size")
		}
	}

	return nil
}

func (c *SideChain) AddPoolBlockExternal(block *PoolBlock) (err error) {
	// Technically some p2pool node could keep stuffing block with transactions until reward is less than 0.6 XMR
	// But default transaction picking algorithm never does that. It's better to just ban such nodes
	if block.Main.Coinbase.TotalReward < 600000000000 {
		return errors.New("block reward too low")
	}

	// Enforce deterministic tx keys starting from v15
	if block.Main.MajorVersion >= monero.HardForkViewTagsVersion {
		kP := c.cache.GetDeterministicTransactionKey(block.GetAddress(), block.Main.PreviousId)
		if bytes.Compare(block.CoinbaseExtra(SideCoinbasePublicKey), kP.PublicKey.AsSlice()) != 0 || bytes.Compare(block.Side.CoinbasePrivateKey[:], kP.PrivateKey.AsSlice()) != 0 {
			return errors.New("invalid deterministic transaction keys")
		}
	}
	// Both tx types are allowed by Monero consensus during v15 because it needs to process pre-fork mempool transactions,
	// but P2Pool can switch to using only TXOUT_TO_TAGGED_KEY for miner payouts starting from v15
	expectedTxType := c.GetTransactionOutputType(block.Main.MajorVersion)

	if err = c.PreprocessBlock(block); err != nil {
		return err
	}
	for _, o := range block.Main.Coinbase.Outputs {
		if o.Type != expectedTxType {
			return errors.New("unexpected transaction type")
		}
	}

	templateId := c.Consensus().CalculateSideTemplateId(&block.Main, &block.Side)
	if templateId != block.SideTemplateId(c.Consensus()) {
		return fmt.Errorf("invalid template id %s, expected %s", templateId.String(), block.SideTemplateId(c.Consensus()).String())
	}

	if block.Side.Difficulty.Cmp64(c.Consensus().MinimumDifficulty) < 0 {
		return fmt.Errorf("block has invalid difficulty %d, expected >= %d", block.Side.Difficulty.Lo, c.Consensus().MinimumDifficulty)
	}

	//TODO: cache?
	expectedDifficulty := c.GetDifficulty(block)
	tooLowDiff := block.Side.Difficulty.Cmp(expectedDifficulty) < 0

	if otherBlock := c.GetPoolBlockByTemplateId(templateId); otherBlock != nil {
		//already added
		//TODO: specifically check Main id for nonce changes! p2pool does not do this
		return nil
	}

	// This is mainly an anti-spam measure, not an actual verification step
	if tooLowDiff {
		// Reduce required diff by 50% (by doubling this block's diff) to account for alternative chains
		diff2 := block.Side.Difficulty.Mul64(2)
		tip := c.GetChainTip()
		for tmp := tip; tmp != nil && (tmp.Side.Height+c.Consensus().ChainWindowSize > tip.Side.Height); tmp = c.GetParent(tmp) {
			if diff2.Cmp(tmp.Side.Difficulty) >= 0 {
				tooLowDiff = false
				break
			}
		}
	}

	//TODO log

	if tooLowDiff {
		//TODO warn
		return nil
	}

	// This check is not always possible to perform because of mainchain reorgs
	//TODO: cache current miner data?
	if data := mainblock.GetBlockHeaderByHash(block.Main.PreviousId); data != nil {
		if data.Height+1 != block.Main.Coinbase.GenHeight {
			return fmt.Errorf("wrong mainchain height %d, expected %d", block.Main.Coinbase.GenHeight, data.Height+1)
		}
	} else {
		//TODO warn unknown block, reorg
	}

	if _, err := block.PowHashWithError(); err != nil {
		return err
	} else {
		//TODO: fast monero submission
		if isHigher, err := block.IsProofHigherThanDifficultyWithError(); err != nil {
			return err
		} else if !isHigher {
			return fmt.Errorf("not enough PoW for height = %d, mainchain height %d", block.Side.Height, block.Main.Coinbase.GenHeight)
		}
	}

	//TODO: block found section

	return c.AddPoolBlock(block)
}

func (c *SideChain) AddPoolBlock(block *PoolBlock) (err error) {
	c.blocksByLock.Lock()
	defer c.blocksByLock.Unlock()
	if _, ok := c.blocksByTemplateId[block.SideTemplateId(c.Consensus())]; ok {
		//already inserted
		//TODO WARN
		return nil
	}
	c.blocksByTemplateId[block.SideTemplateId(c.Consensus())] = block

	if l, ok := c.blocksByHeight[block.Side.Height]; ok {
		c.blocksByHeight[block.Side.Height] = append(l, block)
	} else {
		c.blocksByHeight[block.Side.Height] = []*PoolBlock{block}
	}

	c.updateDepths(block)

	if block.Verified.Load() {
		if !block.Invalid.Load() {
			c.updateChainTip(block)
		}
	} else {
		c.verifyLoop(block)
	}

	return nil
}

func (c *SideChain) verifyLoop(block *PoolBlock) {
	// PoW is already checked at this point

	blocksToVerify := make([]*PoolBlock, 1, 8)
	blocksToVerify[0] = block
	var highestBlock *PoolBlock
	for len(blocksToVerify) != 0 {
		block = blocksToVerify[len(blocksToVerify)-1]
		blocksToVerify = blocksToVerify[:len(blocksToVerify)-1]

		if block.Verified.Load() {
			continue
		}

		c.verifyBlock(block)

		if !block.Verified.Load() {
			//TODO log
			continue
		}
		if block.Invalid.Load() {
			//TODO log
		} else {
			//TODO log

			// This block is now verified

			if isLongerChain, _ := c.IsLongerChain(highestBlock, block); isLongerChain {
				highestBlock = block
			} else if highestBlock != nil && highestBlock.Side.Height > block.Side.Height {
				//TODO log
			}

			if block.WantBroadcast.Load() && !block.Broadcasted.Swap(true) {
				if block.Depth.Load() < UncleBlockDepth {
					c.server.Broadcast(block)
				}
			}

			//TODO save

			// Try to verify blocks on top of this one
			for i := uint64(1); i <= UncleBlockDepth; i++ {
				blocksToVerify = append(blocksToVerify, c.blocksByHeight[block.Side.Height+i]...)
			}
		}
	}

	if highestBlock != nil {
		c.updateChainTip(highestBlock)
	}
}

func (c *SideChain) verifyBlock(block *PoolBlock) {
	// Genesis
	if block.Side.Height == 0 {
		if block.Side.Parent != types.ZeroHash ||
			len(block.Side.Uncles) != 0 ||
			block.Side.Difficulty.Cmp64(c.Consensus().MinimumDifficulty) != 0 ||
			block.Side.CumulativeDifficulty.Cmp64(c.Consensus().MinimumDifficulty) != 0 {
			block.Invalid.Store(true)
		}
		block.Verified.Store(true)
		//this does not verify coinbase outputs, but that's fine
		return
	}

	// Deep block
	//
	// Blocks in PPLNS window (m_chainWindowSize) require up to m_chainWindowSize earlier blocks to verify
	// If a block is deeper than m_chainWindowSize * 2 - 1 it can't influence blocks in PPLNS window
	// Also, having so many blocks on top of this one means it was verified by the network at some point
	// We skip checks in this case to make pruning possible
	if block.Depth.Load() >= c.Consensus().ChainWindowSize*2 {
		//TODO log
		block.Verified.Store(true)
		block.Invalid.Store(false)
		return
	}

	//Regular block
	//Must have parent
	if block.Side.Parent == types.ZeroHash {
		block.Verified.Store(true)
		block.Invalid.Store(true)
		return
	}

	if parent := c.getParent(block); parent != nil {
		// If it's invalid then this block is also invalid
		if parent.Invalid.Load() {
			block.Verified.Store(false)
			block.Invalid.Store(false)
			return
		}

		expectedHeight := parent.Side.Height + 1
		if expectedHeight != block.Side.Height {
			//TODO WARN
			block.Invalid.Store(true)
			return
		}

		// Uncle hashes must be sorted in the ascending order to prevent cheating when the same hash is repeated multiple times
		for i, uncleId := range block.Side.Uncles {
			if i == 1 {
				continue
			}
			if bytes.Compare(block.Side.Uncles[i-1][:], uncleId[:]) < 0 {
				//TODO warn
				block.Verified.Store(true)
				block.Invalid.Store(true)
				return
			}
		}

		expectedCummulativeDifficulty := parent.Side.CumulativeDifficulty.Add(block.Side.Difficulty)

		//check uncles

		minedBlocks := make([]types.Hash, 0, len(block.Side.Uncles)*UncleBlockDepth*2+1)
		{
			tmp := parent
			n := utils.Min(UncleBlockDepth, block.Side.Height+1)
			for i := uint64(0); tmp != nil && i < n; i++ {
				minedBlocks = append(minedBlocks, tmp.SideTemplateId(c.Consensus()))
				for _, uncleId := range tmp.Side.Uncles {
					minedBlocks = append(minedBlocks, uncleId)
				}
				tmp = c.getParent(tmp)
			}
		}

		for _, uncleId := range block.Side.Uncles {
			// Empty hash is only used in the genesis block and only for its parent
			// Uncles can't be empty
			if uncleId == types.ZeroHash {
				//TODO warn
				block.Verified.Store(true)
				block.Invalid.Store(true)
				return
			}

			// Can't mine the same uncle block twice
			if slices.Index(minedBlocks, uncleId) != -1 {
				//TODO warn
				block.Verified.Store(true)
				block.Invalid.Store(true)
				return
			}

			if uncle := c.getPoolBlockByTemplateId(uncleId); uncle == nil {
				block.Verified.Store(false)
				return
			} else if uncle.Invalid.Load() {
				// If it's invalid then this block is also invalid
				block.Verified.Store(true)
				block.Invalid.Store(true)
				return
			} else if uncle.Side.Height >= block.Side.Height || (uncle.Side.Height+UncleBlockDepth < block.Side.Height) {
				//TODO warn
				block.Verified.Store(true)
				block.Invalid.Store(true)
				return
			} else {
				// Check that uncle and parent have the same ancestor (they must be on the same chain)
				tmp := parent
				for tmp.Side.Height > uncle.Side.Height {
					tmp = c.getParent(tmp)
					if tmp == nil {
						//TODO warn
						block.Verified.Store(true)
						block.Invalid.Store(true)
						return
					}
				}

				if tmp.Side.Height < uncle.Side.Height {
					//TODO warn
					block.Verified.Store(true)
					block.Invalid.Store(true)
					return
				}

				if sameChain := func() bool {
					tmp2 := uncle
					for j := uint64(0); j < UncleBlockDepth && tmp != nil && tmp2 != nil && (tmp.Side.Height+UncleBlockDepth >= block.Side.Height); j++ {
						if tmp.Side.Parent == tmp2.Side.Parent {
							return true
						}
						tmp = c.getParent(tmp)
						tmp2 = c.getParent(tmp2)
					}
					return false
				}(); !sameChain {
					//TODO warn
					block.Verified.Store(true)
					block.Invalid.Store(true)
					return
				}

				expectedCummulativeDifficulty = expectedCummulativeDifficulty.Add(uncle.Side.Difficulty)

			}

		}

		// We can verify this block now (all previous blocks in the window are verified and valid)
		// It can still turn out to be invalid
		block.Verified.Store(true)

		if !block.Side.CumulativeDifficulty.Equals(expectedCummulativeDifficulty) {
			//TODO warn
			block.Invalid.Store(true)
			return
		}

		// Verify difficulty and miner rewards only for blocks in PPLNS window
		if block.Depth.Load() >= c.Consensus().ChainWindowSize {
			//TODO warn
			block.Invalid.Store(true)
			return
		}

		if diff := c.getDifficulty(parent); diff == types.ZeroDifficulty {
			//TODO warn
			block.Invalid.Store(true)
			return
		} else if diff != block.Side.Difficulty {
			//TODO warn
			block.Invalid.Store(true)
			return
		}

		if shares := c.getShares(block); len(shares) == 0 {
			//TODO warn
			block.Invalid.Store(true)
			return
		} else if len(shares) != len(block.Main.Coinbase.Outputs) {
			//TODO warn
			block.Invalid.Store(true)
			return
		} else if totalReward := func() (result uint64) {
			for _, o := range block.Main.Coinbase.Outputs {
				result += o.Reward
			}
			return
		}(); totalReward != block.Main.Coinbase.TotalReward {
			//TODO warn
			block.Invalid.Store(true)
			return
		} else if rewards := c.SplitReward(totalReward, shares); len(rewards) != len(block.Main.Coinbase.Outputs) {
			//TODO warn
			block.Invalid.Store(true)
			return
		} else {
			for i, reward := range rewards {
				out := block.Main.Coinbase.Outputs[i]
				if reward != out.Reward {
					//TODO warn
					block.Invalid.Store(true)
					return
				}

				if ephPublicKey, viewTag := c.cache.GetEphemeralPublicKey(&shares[i].Address, &block.Side.CoinbasePrivateKey, uint64(i)); ephPublicKey != out.EphemeralPublicKey {
					//TODO warn
					block.Invalid.Store(true)
					return
				} else if out.Type == transaction.TxOutToTaggedKey && viewTag != out.ViewTag {
					//TODO warn
					block.Invalid.Store(true)
					return
				}
			}
		}

		// All checks passed
		block.Invalid.Store(false)
		return
	} else {
		block.Verified.Store(false)
		return
	}
}

func (c *SideChain) updateDepths(block *PoolBlock) {
	for i := uint64(1); i <= UncleBlockDepth; i++ {
		for _, child := range c.blocksByHeight[block.Side.Height+i] {
			if child.Side.Parent.Equals(block.SideTemplateId(c.Consensus())) {
				if i != 1 {
					//TODO: error
				} else {
					block.Depth.Store(utils.Max(block.Depth.Load(), child.Depth.Load()+1))
				}
			}

			if ix := slices.Index(child.Side.Uncles, block.SideTemplateId(c.Consensus())); ix != 1 {
				block.Depth.Store(utils.Max(block.Depth.Load(), child.Depth.Load()+1))
			}
		}
	}

	blocksToUpdate := make([]*PoolBlock, 1, 8)
	blocksToUpdate[0] = block

	for len(blocksToUpdate) != 0 {
		block = blocksToUpdate[len(blocksToUpdate)-1]
		blocksToUpdate = blocksToUpdate[:len(blocksToUpdate)-1]

		blockDepth := block.Depth.Load()
		// Verify this block and possibly other blocks on top of it when we're sure it will get verified
		if !block.Verified.Load() && (blockDepth >= c.Consensus().ChainWindowSize*2 || block.Side.Height == 0) {
			c.verifyLoop(block)
		}

		if parent := c.getParent(block); parent != nil {
			if parent.Side.Height+1 != block.Side.Height {
				//TODO error
			}

			if parent.Depth.Load() < blockDepth+1 {
				parent.Depth.Store(blockDepth + 1)
				blocksToUpdate = append(blocksToUpdate, parent)
			}
		}

		for _, uncleId := range block.Side.Uncles {
			if uncle := c.getPoolBlockByTemplateId(uncleId); uncle != nil {
				if uncle.Side.Height >= block.Side.Height || (uncle.Side.Height+UncleBlockDepth < block.Side.Height) {
					//TODO: error
				}

				d := block.Side.Height - uncle.Side.Height
				if uncle.Depth.Load() < blockDepth+d {
					uncle.Depth.Store(blockDepth + d)
					blocksToUpdate = append(blocksToUpdate, uncle)
				}
			}
		}
	}
}

func (c *SideChain) updateChainTip(block *PoolBlock) {
	if !block.Verified.Load() || block.Invalid.Load() {
		//todo err
		return
	}

	if block.Depth.Load() >= c.Consensus().ChainWindowSize {
		//TODO err
		return
	}

	tip := c.GetChainTip()

	if isLongerChain, isAlternative := c.IsLongerChain(tip, block); isLongerChain {
		if diff := c.getDifficulty(block); diff != types.ZeroDifficulty {
			c.chainTip.Store(block)
			c.currentDifficulty.Store(&diff)
			//TODO log

			block.WantBroadcast.Store(true)

			if isAlternative {
				c.cache.Clear()
			}

			c.pruneOldBlocks()
		}
	} else if block.Side.Height > tip.Side.Height {
		//TODO log
	} else if block.Side.Height+UncleBlockDepth > tip.Side.Height {
		//TODO: log
	}

	if block.WantBroadcast.Load() && !block.Broadcasted.Swap(true) {
		c.server.Broadcast(block)
	}

}

func (c *SideChain) pruneOldBlocks() {
	//TODO
}

func (c *SideChain) GetTransactionOutputType(majorVersion uint8) uint8 {
	// Both tx types are allowed by Monero consensus during v15 because it needs to process pre-fork mempool transactions,
	// but P2Pool can switch to using only TXOUT_TO_TAGGED_KEY for miner payouts starting from v15
	expectedTxType := uint8(transaction.TxOutToKey)
	if majorVersion >= monero.HardForkViewTagsVersion {
		expectedTxType = transaction.TxOutToTaggedKey
	}

	return expectedTxType
}

func (c *SideChain) getOutputs(block *PoolBlock) []*transaction.CoinbaseTransactionOutput {
	if b := c.GetPoolBlockByTemplateId(block.SideTemplateId(c.Consensus())); b != nil {
		return b.Main.Coinbase.Outputs
	}

	tmpShares := c.getShares(block)
	tmpRewards := c.SplitReward(block.Main.Coinbase.TotalReward, tmpShares)

	if tmpShares == nil || tmpRewards == nil || len(tmpRewards) != len(tmpShares) {
		return nil
	}

	var wg sync.WaitGroup

	var counter atomic.Uint32
	n := uint32(len(tmpShares))

	outputs := make([]*transaction.CoinbaseTransactionOutput, n)

	txType := c.GetTransactionOutputType(block.Main.MajorVersion)

	const helperRoutines = 4

	//TODO: check

	for i := 0; i < helperRoutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				workIndex := counter.Add(1)
				if workIndex > n {
					return
				}

				output := &transaction.CoinbaseTransactionOutput{
					Index: uint64(workIndex - 1),
					Type:  txType,
				}
				output.Reward = tmpRewards[output.Index]
				output.EphemeralPublicKey, output.ViewTag = c.cache.GetEphemeralPublicKey(&tmpShares[output.Index].Address, &block.Side.CoinbasePrivateKey, output.Index)

				outputs[output.Index] = output
			}
		}()
	}
	wg.Wait()

	return outputs
}

func (c *SideChain) SplitReward(reward uint64, shares Shares) (rewards []uint64) {
	var totalWeight uint64
	for i := range shares {
		totalWeight += shares[i].Weight
	}

	if totalWeight == 0 {
		//TODO: err
		return nil
	}

	rewards = make([]uint64, len(shares))

	var w uint64
	var rewardGiven uint64

	for i := range shares {
		w += shares[i].Weight
		nextValue := types.DifficultyFrom64(w).Mul64(reward).Div64(totalWeight).Lo
		rewards[i] = nextValue - rewardGiven
		rewardGiven = nextValue
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

func (c *SideChain) getShares(tip *PoolBlock) (shares Shares) {
	shares = make([]Share, 0, c.Consensus().ChainWindowSize*2)

	var blockDepth uint64

	cur := tip
	var curShare Share
	for {
		curShare.Weight = cur.Side.Difficulty.Lo
		curShare.Address = *cur.GetAddress()

		for _, uncleId := range cur.Side.Uncles {
			if uncle := c.GetPoolBlockByTemplateId(uncleId); uncle == nil {
				//cannot find uncles
				return nil
			} else {
				// Skip uncles which are already out of PPLNS window
				if (tip.Side.Height - uncle.Side.Height) >= c.Consensus().ChainWindowSize {
					continue
				}

				// Take some % of uncle's weight into this share
				product := uncle.Side.Difficulty.Mul64(c.Consensus().UnclePenalty)
				unclePenalty := product.Div64(100)
				curShare.Weight += unclePenalty.Lo

				shares = append(shares, Share{Weight: uncle.Side.Difficulty.Sub(unclePenalty).Lo, Address: *uncle.GetAddress()})
			}
		}

		shares = append(shares, curShare)

		blockDepth++

		if blockDepth >= c.Consensus().ChainWindowSize {
			break
		}

		// Reached the genesis block so we're done
		if cur.Side.Height == 0 {
			break
		}

		cur = c.getPoolBlockByTemplateId(cur.SideTemplateId(c.Consensus()))

		if cur == nil {
			return nil
		}
	}

	// Combine shares with the same wallet addresses
	slices.SortFunc(shares, func(a Share, b Share) bool {
		return a.Address.Compare(&b.Address) < 0
	})

	k := 0
	for i := 0; i < len(shares); i++ {
		if shares[i].Address.Compare(&shares[k].Address) == 0 {
			shares[k].Weight += shares[i].Weight
		} else {
			k++
			shares[k].Address = shares[i].Address
			shares[k].Weight = shares[i].Weight
		}
	}

	return slices.Clone(shares[:k+1])
}

type DifficultyData struct {
	CumulativeDifficulty types.Difficulty
	Timestamp            uint64
}

func (c *SideChain) GetDifficulty(tip *PoolBlock) types.Difficulty {
	c.blocksByLock.RLock()
	defer c.blocksByLock.RUnlock()
	return c.getDifficulty(tip)
}

func (c *SideChain) getDifficulty(tip *PoolBlock) types.Difficulty {
	difficultyData := make([]DifficultyData, 0, c.Consensus().ChainWindowSize*2)
	cur := tip
	var blockDepth uint64
	var oldestTimestamp uint64 = math.MaxUint64
	for {
		oldestTimestamp = utils.Min(oldestTimestamp, cur.Main.Timestamp)
		difficultyData = append(difficultyData, DifficultyData{CumulativeDifficulty: cur.Side.CumulativeDifficulty, Timestamp: cur.Main.Timestamp})

		for _, uncleId := range cur.Side.Uncles {
			if uncle := c.getPoolBlockByTemplateId(uncleId); uncle == nil {
				//cannot find uncles
				return types.ZeroDifficulty
			} else {
				// Skip uncles which are already out of PPLNS window
				if (tip.Side.Height - uncle.Side.Height) >= c.Consensus().ChainWindowSize {
					continue
				}

				oldestTimestamp = utils.Min(oldestTimestamp, uncle.Main.Timestamp)
				difficultyData = append(difficultyData, DifficultyData{CumulativeDifficulty: uncle.Side.CumulativeDifficulty, Timestamp: uncle.Main.Timestamp})
			}
		}

		blockDepth++

		if blockDepth >= c.Consensus().ChainWindowSize {
			break
		}

		// Reached the genesis block so we're done
		if cur.Side.Height == 0 {
			break
		}

		cur = c.getPoolBlockByTemplateId(cur.SideTemplateId(c.Consensus()))

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

	//TODO: p2pool uses uint64 instead of full range here: This is correct as long as the difference between two 128-bit difficulties is less than 2^64, even if it wraps
	deltaDiff := diff2.Sub(diff1)

	product := deltaDiff.Mul64(c.Consensus().TargetBlockTime)

	if product.Cmp64(deltaT) >= 0 {
		//TODO: error, calculated difficulty too high
		return types.ZeroDifficulty
	}

	curDifficulty := product.Div64(deltaT)

	if curDifficulty.Cmp64(c.Consensus().MinimumDifficulty) < 0 {
		curDifficulty = types.DifficultyFrom64(c.Consensus().MinimumDifficulty)
	}
	return curDifficulty
}

func (c *SideChain) GetParent(block *PoolBlock) *PoolBlock {
	c.blocksByLock.RLock()
	defer c.blocksByLock.RUnlock()
	return c.getParent(block)
}

func (c *SideChain) getParent(block *PoolBlock) *PoolBlock {
	return c.getPoolBlockByTemplateId(block.Side.Parent)
}

func (c *SideChain) GetPoolBlockByTemplateId(id types.Hash) *PoolBlock {
	c.blocksByLock.RLock()
	defer c.blocksByLock.RUnlock()
	return c.getPoolBlockByTemplateId(id)
}

func (c *SideChain) getPoolBlockByTemplateId(id types.Hash) *PoolBlock {
	return c.blocksByTemplateId[id]
}

func (c *SideChain) GetChainTip() *PoolBlock {
	return c.chainTip.Load()
}

func (c *SideChain) IsLongerChain(block, candidate *PoolBlock) (isLonger, isAlternative bool) {
	if candidate == nil || !candidate.Verified.Load() || !candidate.Invalid.Load() {
		return false, false
	}

	// Switching from an empty to a non-empty chain
	if block == nil {
		return true, true
	}

	// If these two blocks are on the same chain, they must have a common ancestor

	blockAncestor := block
	for blockAncestor != nil && blockAncestor.Side.Height > candidate.Side.Height {
		blockAncestor = c.GetParent(blockAncestor)
		//TODO: err on blockAncestor nil
	}

	if blockAncestor != nil {
		candidateAncestor := candidate
		for candidateAncestor != nil && candidateAncestor.Side.Height > blockAncestor.Side.Height {
			candidateAncestor = c.GetParent(candidateAncestor)
			//TODO: err on candidateAncestor nil
		}

		for blockAncestor != nil && candidateAncestor != nil {
			if blockAncestor.Side.Parent.Equals(candidateAncestor.Side.Parent) {
				return block.Side.CumulativeDifficulty.Cmp(candidate.Side.CumulativeDifficulty) < 0, false
			}
			blockAncestor = c.GetParent(blockAncestor)
			candidateAncestor = c.GetParent(candidateAncestor)
		}
	}

	// They're on totally different chains. Compare total difficulties over the last m_chainWindowSize blocks

	var blockTotalDiff, candidateTotalDiff types.Difficulty

	oldChain := block
	newChain := candidate

	var candidateMainchainHeight, candidateMainchainMinHeight uint64
	var mainchainPrevId types.Hash

	for i := uint64(0); i < c.Consensus().ChainWindowSize && (oldChain != nil || newChain != nil); i++ {
		if oldChain != nil {
			blockTotalDiff = blockTotalDiff.Add(oldChain.Side.Difficulty)
			oldChain = c.GetParent(oldChain)
		}

		if newChain != nil {
			if candidateMainchainMinHeight != 0 {
				candidateMainchainMinHeight = utils.Min(candidateMainchainMinHeight, newChain.Main.Coinbase.GenHeight)
			} else {
				candidateMainchainMinHeight = newChain.Main.Coinbase.GenHeight
			}
			candidateTotalDiff = candidateTotalDiff.Add(newChain.Side.Difficulty)

			if !newChain.Main.PreviousId.Equals(mainchainPrevId) {
				if data := mainblock.GetBlockHeaderByHash(newChain.Main.PreviousId); data != nil {
					mainchainPrevId = data.Id
					candidateMainchainHeight = utils.Max(candidateMainchainHeight, data.Height)
				}
			}

			newChain = c.GetParent(newChain)
		}
	}

	if blockTotalDiff.Cmp(candidateTotalDiff) >= 0 {
		return false, true
	}

	// Final check: candidate chain must be built on top of recent mainchain blocks
	if data := mainblock.GetLastBlockHeader(); data != nil {
		if candidateMainchainHeight+10 < data.Height {
			//TODO: warn received a longer alternative chain but it's stale: height
			return false, true
		}

		limit := c.Consensus().ChainWindowSize * 4 * c.Consensus().TargetBlockTime / monero.BlockTime
		if candidateMainchainMinHeight+limit < data.Height {
			//TODO: warn received a longer alternative chain but it's stale: min height
			return false, true
		}

		return true, true
	} else {
		return false, true
	}
}
