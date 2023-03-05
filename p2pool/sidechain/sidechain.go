package sidechain

import (
	"bytes"
	"errors"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	mainblock "git.gammaspectra.live/P2Pool/p2pool-observer/monero/block"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/randomx"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/transaction"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"golang.org/x/exp/slices"
	"log"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

type Cache interface {
	GetBlob(key []byte) (blob []byte, err error)
	SetBlob(key, blob []byte) (err error)
	RemoveBlob(key []byte) (err error)
}

type P2PoolInterface interface {
	ConsensusProvider
	Cache
	Broadcast(block *PoolBlock)
	ClientRPC() *client.Client
	GetChainMainByHeight(height uint64) *ChainMain
	GetChainMainByHash(hash types.Hash) *ChainMain
	GetMinimalBlockHeaderByHeight(height uint64) *mainblock.Header
	GetMinimalBlockHeaderByHash(hash types.Hash) *mainblock.Header
	GetDifficultyByHeight(height uint64) types.Difficulty
	UpdateBlockFound(data *ChainMain, block *PoolBlock)
	SubmitBlock(block *mainblock.Block)
	GetChainMainTip() *ChainMain
}

type ChainMain struct {
	Difficulty types.Difficulty
	Height     uint64
	Timestamp  uint64
	Reward     uint64
	Id         types.Hash
}

type SideChain struct {
	derivationCache *DerivationCache
	server          P2PoolInterface

	sidechainLock sync.RWMutex

	watchBlock            *ChainMain
	watchBlockSidechainId types.Hash

	sharesCache Shares

	blocksByTemplateId map[types.Hash]*PoolBlock
	blocksByHeight     map[uint64][]*PoolBlock

	chainTip          atomic.Pointer[PoolBlock]
	currentDifficulty atomic.Pointer[types.Difficulty]

	precalcFinished atomic.Bool
}

func NewSideChain(server P2PoolInterface) *SideChain {
	return &SideChain{
		derivationCache:    NewDerivationCache(),
		server:             server,
		blocksByTemplateId: make(map[types.Hash]*PoolBlock),
		blocksByHeight:     make(map[uint64][]*PoolBlock),
		sharesCache:        make(Shares, 0, server.Consensus().ChainWindowSize*2),
	}
}

func (c *SideChain) Consensus() *Consensus {
	return c.server.Consensus()
}

func (c *SideChain) PreCalcFinished() bool {
	return c.precalcFinished.Load()
}

func (c *SideChain) PreprocessBlock(block *PoolBlock) (missingBlocks []types.Hash, err error) {
	c.sidechainLock.RLock()
	defer c.sidechainLock.RUnlock()

	if len(block.Main.Coinbase.Outputs) == 0 {
		if outputs := c.getOutputs(block); outputs == nil {
			return nil, errors.New("nil transaction outputs")
		} else {
			block.Main.Coinbase.Outputs = outputs
		}

		if outputBlob, err := block.Main.Coinbase.OutputsBlob(); err != nil {
			return nil, err
		} else if uint64(len(outputBlob)) != block.Main.Coinbase.OutputsBlobSize {
			return nil, fmt.Errorf("invalid output blob size, got %d, expected %d", block.Main.Coinbase.OutputsBlobSize, len(outputBlob))
		}
	}

	if len(block.Main.TransactionParentIndices) > 0 && len(block.Main.TransactionParentIndices) == len(block.Main.Transactions) {
		if slices.Index(block.Main.Transactions, types.ZeroHash) != -1 { //only do this when zero hashes exist
			parent := c.getParent(block)
			if parent == nil {
				missingBlocks = append(missingBlocks, block.Side.Parent)
				return missingBlocks, errors.New("parent does not exist in compact block")
			}
			for i, parentIndex := range block.Main.TransactionParentIndices {
				if parentIndex != 0 {
					// p2pool stores coinbase transaction hash as well, decrease
					actualIndex := parentIndex - 1
					if actualIndex > uint64(len(parent.Main.Transactions)) {
						return nil, errors.New("index of parent transaction out of bounds")

					}
					block.Main.Transactions[i] = parent.Main.Transactions[actualIndex]
				}
			}
		}
	} else {
		// fill if not received from network
		c.fillPoolBlockTransactionParentIndices(block)
	}

	return nil, nil
}

func (c *SideChain) fillPoolBlockTransactionParentIndices(block *PoolBlock) {
	if len(block.Main.Transactions) != len(block.Main.TransactionParentIndices) {

		parent := c.getParent(block)
		if parent != nil {
			block.Main.TransactionParentIndices = make([]uint64, len(block.Main.Transactions))
			//do not fail if not found
			for i, txHash := range block.Main.Transactions {
				if parentIndex := slices.Index(parent.Main.Transactions, txHash); parentIndex != -1 {
					//increase as p2pool stores tx hash as well
					block.Main.TransactionParentIndices[i] = uint64(parentIndex + 1)
				}
			}
		}
	}

}

func (c *SideChain) isPoolBlockTransactionKeyIsDeterministic(block *PoolBlock) bool {
	kP := c.derivationCache.GetDeterministicTransactionKey(block.GetAddress(), block.Main.PreviousId)
	return bytes.Compare(block.CoinbaseExtra(SideCoinbasePublicKey), kP.PublicKey.AsSlice()) == 0 && bytes.Compare(block.Side.CoinbasePrivateKey[:], kP.PrivateKey.AsSlice()) == 0
}

func (c *SideChain) getSeedByHeightFunc() mainblock.GetSeedByHeightFunc {
	return func(height uint64) (hash types.Hash) {
		seedHeight := randomx.SeedHeight(height)
		if h := c.server.GetMinimalBlockHeaderByHeight(seedHeight); h != nil {
			return h.Id
		} else {
			return types.ZeroHash
		}
	}
}

func (c *SideChain) AddPoolBlockExternal(block *PoolBlock) (missingBlocks []types.Hash, err error) {
	// Technically some p2pool node could keep stuffing block with transactions until reward is less than 0.6 XMR
	// But default transaction picking algorithm never does that. It's better to just ban such nodes
	if block.Main.Coinbase.TotalReward < monero.TailEmissionReward {
		return nil, errors.New("block reward too low")
	}

	// Enforce deterministic tx keys starting from v15
	if block.Main.MajorVersion >= monero.HardForkViewTagsVersion {
		if !c.isPoolBlockTransactionKeyIsDeterministic(block) {
			return nil, errors.New("invalid deterministic transaction keys")
		}
	}
	// Both tx types are allowed by Monero consensus during v15 because it needs to process pre-fork mempool transactions,
	// but P2Pool can switch to using only TXOUT_TO_TAGGED_KEY for miner payouts starting from v15
	expectedTxType := c.GetTransactionOutputType(block.Main.MajorVersion)

	if missingBlocks, err = c.PreprocessBlock(block); err != nil {
		return missingBlocks, err
	}
	for _, o := range block.Main.Coinbase.Outputs {
		if o.Type != expectedTxType {
			return nil, errors.New("unexpected transaction type")
		}
	}

	templateId := c.Consensus().CalculateSideTemplateId(&block.Main, &block.Side)
	if templateId != block.SideTemplateId(c.Consensus()) {
		return nil, fmt.Errorf("invalid template id %s, expected %s", templateId.String(), block.SideTemplateId(c.Consensus()).String())
	}

	if block.Side.Difficulty.Cmp64(c.Consensus().MinimumDifficulty) < 0 {
		return nil, fmt.Errorf("block mined by %s has invalid difficulty %s, expected >= %d", block.GetAddress().ToBase58(), block.Side.Difficulty.StringNumeric(), c.Consensus().MinimumDifficulty)
	}

	//TODO: cache?
	//expectedDifficulty := c.GetDifficulty(block)
	//tooLowDiff := block.Side.Difficulty.Cmp(expectedDifficulty) < 0
	tooLowDiff := false
	var expectedDifficulty types.Difficulty

	if otherBlock := c.GetPoolBlockByTemplateId(templateId); otherBlock != nil {
		//already added
		//TODO: specifically check Main id for nonce changes! p2pool does not do this
		return nil, nil
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
		return nil, fmt.Errorf("block mined by %s has too low difficulty %s, expected >= %s", block.GetAddress().ToBase58(), block.Side.Difficulty.StringNumeric(), expectedDifficulty.StringNumeric())
	}

	// This check is not always possible to perform because of mainchain reorgs
	//TODO: cache current miner data?
	if data := c.server.GetChainMainByHash(block.Main.PreviousId); data != nil {
		if data.Height+1 != block.Main.Coinbase.GenHeight {
			return nil, fmt.Errorf("wrong mainchain height %d, expected %d", block.Main.Coinbase.GenHeight, data.Height+1)
		}
	} else {
		//TODO warn unknown block, reorg
	}

	if _, err := block.PowHashWithError(c.getSeedByHeightFunc()); err != nil {
		return nil, err
	} else {
		if isHigherMainChain, err := block.IsProofHigherThanMainDifficultyWithError(c.server.GetDifficultyByHeight, c.getSeedByHeightFunc()); err != nil {
			log.Printf("[SideChain] add_external_block: couldn't get mainchain difficulty for height = %d: %s", block.Main.Coinbase.GenHeight, err)
		} else if isHigherMainChain {
			log.Printf("[SideChain]: add_external_block: block %s has enough PoW for Monero height %d, submitting it", templateId.String(), block.Main.Coinbase.GenHeight)
			c.server.SubmitBlock(&block.Main)
		}
		if isHigher, err := block.IsProofHigherThanDifficultyWithError(c.getSeedByHeightFunc()); err != nil {
			return nil, err
		} else if !isHigher {
			return nil, fmt.Errorf("not enough PoW for height = %d, mainchain height %d", block.Side.Height, block.Main.Coinbase.GenHeight)
		}
	}

	//TODO: block found section

	return func() []types.Hash {
		c.sidechainLock.RLock()
		defer c.sidechainLock.RUnlock()
		missing := make([]types.Hash, 0, 4)
		if block.Side.Parent != types.ZeroHash && c.getPoolBlockByTemplateId(block.Side.Parent) == nil {
			missing = append(missing, block.Side.Parent)
		}

		for _, uncleId := range block.Side.Uncles {
			if uncleId != types.ZeroHash && c.getPoolBlockByTemplateId(uncleId) == nil {
				missing = append(missing, uncleId)
			}
		}
		return missing
	}(), c.AddPoolBlock(block)
}

func (c *SideChain) AddPoolBlock(block *PoolBlock) (err error) {

	c.sidechainLock.Lock()
	defer c.sidechainLock.Unlock()
	if _, ok := c.blocksByTemplateId[block.SideTemplateId(c.Consensus())]; ok {
		//already inserted
		//TODO WARN
		return nil
	}
	c.blocksByTemplateId[block.SideTemplateId(c.Consensus())] = block

	log.Printf("[SideChain] add_block: height = %d, id = %s, mainchain height = %d, verified = %t, total = %d", block.Side.Height, block.SideTemplateId(c.Consensus()), block.Main.Coinbase.GenHeight, block.Verified.Load(), len(c.blocksByTemplateId))

	if block.SideTemplateId(c.Consensus()) == c.watchBlockSidechainId {
		c.server.UpdateBlockFound(c.watchBlock, block)
		c.watchBlockSidechainId = types.ZeroHash
	}

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

		return nil
	} else {
		return c.verifyLoop(block)
	}
}

func (c *SideChain) verifyLoop(blockToVerify *PoolBlock) (err error) {
	// PoW is already checked at this point

	blocksToVerify := make([]*PoolBlock, 1, 8)
	blocksToVerify[0] = blockToVerify
	var highestBlock *PoolBlock
	for len(blocksToVerify) != 0 {
		block := blocksToVerify[len(blocksToVerify)-1]
		blocksToVerify = blocksToVerify[:len(blocksToVerify)-1]

		if block.Verified.Load() {
			continue
		}

		if verification, invalid := c.verifyBlock(block); invalid != nil {
			log.Printf("[SideChain] block at height = %d, id = %s, mainchain height = %d, mined by %s is invalid: %s", block.Side.Height, block.SideTemplateId(c.Consensus()), block.Main.Coinbase.GenHeight, block.GetAddress().ToBase58(), invalid.Error())
			block.Invalid.Store(true)
			block.Verified.Store(verification == nil)
			if block == blockToVerify {
				//Save error for return
				err = invalid
			}
		} else if verification != nil {
			//log.Printf("[SideChain] can't verify block at height = %d, id = %s, mainchain height = %d, mined by %s: %s", block.Side.Height, block.SideTemplateId(c.Consensus()), block.Main.Coinbase.GenHeight, block.GetAddress().ToBase58(), verification.Error())
			block.Verified.Store(false)
			block.Invalid.Store(false)
		} else {
			block.Verified.Store(true)
			block.Invalid.Store(false)
			log.Printf("[SideChain] verified block at height = %d, depth = %d, id = %s, mainchain height = %d, mined by %s", block.Side.Height, block.Depth.Load(), block.SideTemplateId(c.Consensus()), block.Main.Coinbase.GenHeight, block.GetAddress().ToBase58())

			// This block is now verified

			if isLongerChain, _ := c.isLongerChain(highestBlock, block); isLongerChain {
				highestBlock = block
			} else if highestBlock != nil && highestBlock.Side.Height > block.Side.Height {
				log.Printf("[SideChain] block at height = %d, id = %s, is not a longer chain than height = %d, id = %s", block.Side.Height, block.SideTemplateId(c.Consensus()), highestBlock.Side.Height, highestBlock.SideTemplateId(c.Consensus()))
			}

			c.fillPoolBlockTransactionParentIndices(block)

			if block.WantBroadcast.Load() && !block.Broadcasted.Swap(true) {
				if block.Depth.Load() < UncleBlockDepth {
					c.server.Broadcast(block)
				}
			}

			//store for faster startup
			c.saveBlock(block)

			// Try to verify blocks on top of this one
			for i := uint64(1); i <= UncleBlockDepth; i++ {
				blocksToVerify = append(blocksToVerify, c.blocksByHeight[block.Side.Height+i]...)
			}
		}
	}

	if highestBlock != nil {
		c.updateChainTip(highestBlock)
	}

	return
}

func (c *SideChain) verifyBlock(block *PoolBlock) (verification error, invalid error) {
	// Genesis
	if block.Side.Height == 0 {
		if block.Side.Parent != types.ZeroHash ||
			len(block.Side.Uncles) != 0 ||
			block.Side.Difficulty.Cmp64(c.Consensus().MinimumDifficulty) != 0 ||
			block.Side.CumulativeDifficulty.Cmp64(c.Consensus().MinimumDifficulty) != 0 {
			return nil, errors.New("genesis block has invalid parameters")
		}
		//this does not verify coinbase outputs, but that's fine
		return nil, nil
	}

	// Deep block
	//
	// Blocks in PPLNS window (m_chainWindowSize) require up to m_chainWindowSize earlier blocks to verify
	// If a block is deeper than m_chainWindowSize * 2 - 1 it can't influence blocks in PPLNS window
	// Also, having so many blocks on top of this one means it was verified by the network at some point
	// We skip checks in this case to make pruning possible
	if block.Depth.Load() >= c.Consensus().ChainWindowSize*2 {
		log.Printf("[SideChain] block at height = %d, id = %s skipped verification", block.Side.Height, block.SideTemplateId(c.Consensus()))
		return nil, nil
	}

	//Regular block
	//Must have parent
	if block.Side.Parent == types.ZeroHash {
		return nil, errors.New("block must have a parent")
	}

	if parent := c.getParent(block); parent != nil {
		// If it's invalid then this block is also invalid
		if !parent.Verified.Load() {
			return errors.New("parent is not verified"), nil
		}
		if parent.Invalid.Load() {
			return nil, errors.New("parent is invalid")
		}

		expectedHeight := parent.Side.Height + 1
		if expectedHeight != block.Side.Height {
			return nil, fmt.Errorf("wrong height, expected %d", expectedHeight)
		}

		// Uncle hashes must be sorted in the ascending order to prevent cheating when the same hash is repeated multiple times
		for i, uncleId := range block.Side.Uncles {
			if i == 0 {
				continue
			}
			if block.Side.Uncles[i-1].Compare(uncleId) != -1 {
				return nil, errors.New("invalid uncle order")
			}
		}

		expectedCumulativeDifficulty := parent.Side.CumulativeDifficulty.Add(block.Side.Difficulty)

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
				return nil, errors.New("empty uncle hash")
			}

			// Can't mine the same uncle block twice
			if slices.Index(minedBlocks, uncleId) != -1 {
				return nil, fmt.Errorf("uncle %s has already been mined", uncleId.String())
			}

			if uncle := c.getPoolBlockByTemplateId(uncleId); uncle == nil {
				return errors.New("uncle does not exist"), nil
			} else if !uncle.Verified.Load() {
				// If it's invalid then this block is also invalid
				return errors.New("uncle is not verified"), nil
			} else if uncle.Invalid.Load() {
				// If it's invalid then this block is also invalid
				return nil, errors.New("uncle is invalid")
			} else if uncle.Side.Height >= block.Side.Height || (uncle.Side.Height+UncleBlockDepth < block.Side.Height) {
				return nil, fmt.Errorf("uncle at the wrong height (%d)", uncle.Side.Height)
			} else {
				// Check that uncle and parent have the same ancestor (they must be on the same chain)
				tmp := parent
				for tmp.Side.Height > uncle.Side.Height {
					tmp = c.getParent(tmp)
					if tmp == nil {
						return nil, errors.New("uncle from different chain (check 1)")
					}
				}

				if tmp.Side.Height < uncle.Side.Height {
					return nil, errors.New("uncle from different chain (check 2)")
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
					return nil, errors.New("uncle from different chain (check 3)")
				}

				expectedCumulativeDifficulty = expectedCumulativeDifficulty.Add(uncle.Side.Difficulty)

			}

		}

		// We can verify this block now (all previous blocks in the window are verified and valid)
		// It can still turn out to be invalid

		if !block.Side.CumulativeDifficulty.Equals(expectedCumulativeDifficulty) {
			return nil, fmt.Errorf("wrong cumulative difficulty, got %s, expected %s", block.Side.CumulativeDifficulty.StringNumeric(), expectedCumulativeDifficulty.StringNumeric())
		}

		// Verify difficulty and miner rewards only for blocks in PPLNS window
		if block.Depth.Load() >= c.Consensus().ChainWindowSize {
			log.Printf("[SideChain] block at height = %d, id = %s skipped diff/reward verification", block.Side.Height, block.SideTemplateId(c.Consensus()))
			return
		}

		if diff := c.getDifficulty(parent); diff == types.ZeroDifficulty {
			return nil, errors.New("could not get difficulty")
		} else if diff != block.Side.Difficulty {
			return nil, fmt.Errorf("wrong difficulty, got %s, expected %s", block.Side.Difficulty.StringNumeric(), diff.StringNumeric())
		}

		if c.sharesCache = c.getShares(block, c.sharesCache); len(c.sharesCache) == 0 {
			return nil, errors.New("could not get outputs")
		} else if len(c.sharesCache) != len(block.Main.Coinbase.Outputs) {
			return nil, fmt.Errorf("invalid number of outputs, got %d, expected %d", len(block.Main.Coinbase.Outputs), len(c.sharesCache))
		} else if totalReward := func() (result uint64) {
			for _, o := range block.Main.Coinbase.Outputs {
				result += o.Reward
			}
			return
		}(); totalReward != block.Main.Coinbase.TotalReward {
			return nil, fmt.Errorf("invalid total reward, got %d, expected %d", block.Main.Coinbase.TotalReward, totalReward)
		} else if rewards := c.SplitReward(totalReward, c.sharesCache); len(rewards) != len(block.Main.Coinbase.Outputs) {
			return nil, fmt.Errorf("invalid number of outputs, got %d, expected %d", len(block.Main.Coinbase.Outputs), len(rewards))
		} else {

			//prevent multiple allocations
			txPrivateKeySlice := block.Side.CoinbasePrivateKey.AsSlice()
			txPrivateKeyScalar := block.Side.CoinbasePrivateKey.AsScalar()

			results := utils.SplitWork(-2, uint64(len(rewards)), func(workIndex uint64, workerIndex int) error {
				out := block.Main.Coinbase.Outputs[workIndex]
				if rewards[workIndex] != out.Reward {
					return fmt.Errorf("has invalid reward at index %d, got %d, expected %d", workIndex, out.Reward, rewards[workIndex])
				}

				if ephPublicKey, viewTag := c.derivationCache.GetEphemeralPublicKey(&c.sharesCache[workIndex].Address, txPrivateKeySlice, txPrivateKeyScalar, workIndex); ephPublicKey != out.EphemeralPublicKey {
					return fmt.Errorf("has incorrect eph_public_key at index %d, got %s, expected %s", workIndex, out.EphemeralPublicKey.String(), ephPublicKey.String())
				} else if out.Type == transaction.TxOutToTaggedKey && viewTag != out.ViewTag {
					return fmt.Errorf("has incorrect view tag at index %d, got %d, expected %d", workIndex, out.ViewTag, viewTag)
				}
				return nil
			})

			for i := range results {
				if results[i] != nil {
					return nil, results[i]
				}
			}
		}

		// All checks passed
		return nil, nil
	} else {
		return errors.New("parent does not exist"), nil
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

	if isLongerChain, isAlternative := c.isLongerChain(tip, block); isLongerChain {
		if diff := c.getDifficulty(block); diff != types.ZeroDifficulty {
			c.chainTip.Store(block)
			c.currentDifficulty.Store(&diff)
			//TODO log

			block.WantBroadcast.Store(true)

			if isAlternative {
				c.derivationCache.Clear()

				log.Printf("[SideChain] SYNCHRONIZED to tip %s", block.SideTemplateId(c.Consensus()))
			}

			c.pruneOldBlocks()
		}
	} else if block.Side.Height > tip.Side.Height {
		log.Printf("[SideChain] block %s, height = %d, is not a longer chain than %s, height = %d", block.SideTemplateId(c.Consensus()), block.Side.Height, tip.SideTemplateId(c.Consensus()), tip.Side.Height)
	} else if block.Side.Height+UncleBlockDepth > tip.Side.Height {
		log.Printf("[SideChain] possible uncle block: id = %s, height = %d", block.SideTemplateId(c.Consensus()), block.Side.Height)
	}

	if block.WantBroadcast.Load() && !block.Broadcasted.Swap(true) {
		c.server.Broadcast(block)
	}

}

func (c *SideChain) pruneOldBlocks() {

	// Leave 2 minutes worth of spare blocks in addition to 2xPPLNS window for lagging nodes which need to sync
	pruneDistance := c.Consensus().ChainWindowSize*2 + monero.BlockTime/c.Consensus().TargetBlockTime

	curTime := uint64(time.Now().Unix())

	// Remove old blocks from alternative unconnected chains after long enough time
	pruneDelay := c.Consensus().ChainWindowSize * 4 * c.Consensus().TargetBlockTime

	tip := c.GetChainTip()
	if tip == nil || tip.Side.Height < pruneDistance {
		return
	}

	h := tip.Side.Height - pruneDistance

	numBlocksPruned := 0
	for height, v := range c.blocksByHeight {
		if height > h {
			continue
		}

		// loop backwards for proper deletions
		for i := len(v) - 1; i >= 0; i-- {
			block := v[i]
			if block.Depth.Load() >= pruneDistance || (curTime >= (block.LocalTimestamp + pruneDelay)) {
				if _, ok := c.blocksByTemplateId[block.SideTemplateId(c.Consensus())]; ok {
					delete(c.blocksByTemplateId, block.SideTemplateId(c.Consensus()))
					numBlocksPruned++
				} else {
					log.Printf("[SideChain] blocksByHeight and blocksByTemplateId are inconsistent at height = %d, id = %s", height, block.SideTemplateId(c.Consensus()))
				}
				v = slices.Delete(v, i, i+1)
			}
		}

		if len(v) == 0 {
			delete(c.blocksByHeight, height)
		} else {
			c.blocksByHeight[height] = v
		}
	}

	if numBlocksPruned > 0 {
		log.Printf("[SideChain] pruned %d old blocks at heights <= %d", numBlocksPruned, h)
		if !c.precalcFinished.Swap(true) {
			c.derivationCache.Clear()
		}
	}
}

func (c *SideChain) GetMissingBlocks() []types.Hash {
	c.sidechainLock.RLock()
	defer c.sidechainLock.RUnlock()

	missingBlocks := make([]types.Hash, 0)

	for _, b := range c.blocksByTemplateId {
		if b.Verified.Load() {
			continue
		}

		if !b.Side.Parent.Equals(types.ZeroHash) && c.getPoolBlockByTemplateId(b.Side.Parent) == nil {
			missingBlocks = append(missingBlocks, b.Side.Parent)
		}

		missingUncles := 0

		for _, uncleId := range b.Side.Uncles {
			if !uncleId.Equals(types.ZeroHash) && c.getPoolBlockByTemplateId(uncleId) == nil {
				missingBlocks = append(missingBlocks, uncleId)
				missingUncles++

				// Get no more than 2 first missing uncles at a time from each block
				// Blocks with more than 2 uncles are very rare and they will be processed in several steps
				if missingUncles >= 2 {
					break
				}
			}
		}
	}

	return missingBlocks
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

func (c *SideChain) getOutputs(block *PoolBlock) transaction.Outputs {
	//cannot use SideTemplateId() as it might not be proper to calculate yet. fetch from coinbase only here
	if b := c.getPoolBlockByTemplateId(types.HashFromBytes(block.CoinbaseExtra(SideTemplateId))); b != nil {
		return b.Main.Coinbase.Outputs
	}

	return c.calculateOutputs(block)
}

func (c *SideChain) calculateOutputs(block *PoolBlock) transaction.Outputs {
	//TODO: buffer
	tmpShares := c.getShares(block, make(Shares, 0, c.Consensus().ChainWindowSize*2))
	tmpRewards := c.SplitReward(block.Main.Coinbase.TotalReward, tmpShares)

	if tmpShares == nil || tmpRewards == nil || len(tmpRewards) != len(tmpShares) {
		return nil
	}

	n := uint64(len(tmpShares))

	outputs := make(transaction.Outputs, n)

	txType := c.GetTransactionOutputType(block.Main.MajorVersion)

	txPrivateKeySlice := block.Side.CoinbasePrivateKey.AsSlice()
	txPrivateKeyScalar := block.Side.CoinbasePrivateKey.AsScalar()

	_ = utils.SplitWork(-2, n, func(workIndex uint64, workerIndex int) error {
		output := transaction.Output{
			Index: workIndex,
			Type:  txType,
		}
		output.Reward = tmpRewards[output.Index]
		output.EphemeralPublicKey, output.ViewTag = c.derivationCache.GetEphemeralPublicKey(&tmpShares[output.Index].Address, txPrivateKeySlice, txPrivateKeyScalar, output.Index)

		outputs[output.Index] = output

		return nil
	})

	return outputs
}

func (c *SideChain) SplitReward(reward uint64, shares Shares) (rewards []uint64) {
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

func (c *SideChain) getShares(tip *PoolBlock, preAllocatedShares Shares) Shares {

	var blockDepth uint64

	cur := tip

	index := 0
	l := len(preAllocatedShares)
	insert := func(weight types.Difficulty, a *address.PackedAddress) {
		if index < l {
			preAllocatedShares[index].Weight, preAllocatedShares[index].Address = weight, *a
		} else {
			preAllocatedShares = append(preAllocatedShares, &Share{Weight: weight, Address: *a})
		}
		index++
	}

	for {
		curWeight := cur.Side.Difficulty

		for _, uncleId := range cur.Side.Uncles {
			if uncle := c.getPoolBlockByTemplateId(uncleId); uncle == nil {
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
				curWeight = curWeight.Add(unclePenalty)

				insert(uncle.Side.Difficulty.Sub(unclePenalty), uncle.GetAddress())
			}
		}

		insert(curWeight, cur.GetAddress())

		blockDepth++

		if blockDepth >= c.Consensus().ChainWindowSize {
			break
		}

		// Reached the genesis block so we're done
		if cur.Side.Height == 0 {
			break
		}

		cur = c.getParent(cur)

		if cur == nil {
			return nil
		}
	}

	shares := preAllocatedShares[:index]

	// Combine shares with the same wallet addresses
	slices.SortFunc(shares, func(a *Share, b *Share) bool {
		return a.Address.Compare(&b.Address) < 0
	})

	k := 0
	for i := 1; i < len(shares); i++ {
		if shares[i].Address.Compare(&shares[k].Address) == 0 {
			shares[k].Weight = shares[k].Weight.Add(shares[i].Weight)
		} else {
			k++
			shares[k].Address = shares[i].Address
			shares[k].Weight = shares[i].Weight
		}
	}

	return shares[:k+1]
}

type DifficultyData struct {
	CumulativeDifficulty types.Difficulty
	Timestamp            uint64
}

func (c *SideChain) GetDifficulty(tip *PoolBlock) types.Difficulty {
	c.sidechainLock.RLock()
	defer c.sidechainLock.RUnlock()
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

		cur = c.getParent(cur)

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

	curDifficulty := deltaDiff.Mul64(c.Consensus().TargetBlockTime).Div64(deltaT)

	if curDifficulty.Cmp64(c.Consensus().MinimumDifficulty) < 0 {
		curDifficulty = types.DifficultyFrom64(c.Consensus().MinimumDifficulty)
	}
	return curDifficulty
}

func (c *SideChain) GetParent(block *PoolBlock) *PoolBlock {
	c.sidechainLock.RLock()
	defer c.sidechainLock.RUnlock()
	return c.getParent(block)
}

func (c *SideChain) getParent(block *PoolBlock) *PoolBlock {
	return c.getPoolBlockByTemplateId(block.Side.Parent)
}

func (c *SideChain) GetPoolBlockByTemplateId(id types.Hash) *PoolBlock {
	c.sidechainLock.RLock()
	defer c.sidechainLock.RUnlock()
	return c.getPoolBlockByTemplateId(id)
}

func (c *SideChain) getPoolBlockByTemplateId(id types.Hash) *PoolBlock {
	return c.blocksByTemplateId[id]
}

func (c *SideChain) GetPoolBlocksByHeight(height uint64) []*PoolBlock {
	c.sidechainLock.RLock()
	defer c.sidechainLock.RUnlock()
	return slices.Clone(c.getPoolBlocksByHeight(height))
}

func (c *SideChain) getPoolBlocksByHeight(height uint64) []*PoolBlock {
	return c.blocksByHeight[height]
}

func (c *SideChain) GetPoolBlockCount() int {
	c.sidechainLock.RLock()
	defer c.sidechainLock.RUnlock()
	return len(c.blocksByTemplateId)
}

func (c *SideChain) WatchMainChainBlock(mainData *ChainMain, possibleId types.Hash) {
	c.sidechainLock.Lock()
	defer c.sidechainLock.Unlock()

	c.watchBlock = mainData
	c.watchBlockSidechainId = possibleId
}

func (c *SideChain) GetChainTip() *PoolBlock {
	return c.chainTip.Load()
}

func (c *SideChain) IsLongerChain(block, candidate *PoolBlock) (isLonger, isAlternative bool) {
	c.sidechainLock.RLock()
	defer c.sidechainLock.RUnlock()
	return c.isLongerChain(block, candidate)
}

func (c *SideChain) isLongerChain(block, candidate *PoolBlock) (isLonger, isAlternative bool) {
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
		blockAncestor = c.getParent(blockAncestor)
		//TODO: err on blockAncestor nil
	}

	if blockAncestor != nil {
		candidateAncestor := candidate
		for candidateAncestor != nil && candidateAncestor.Side.Height > blockAncestor.Side.Height {
			candidateAncestor = c.getParent(candidateAncestor)
			//TODO: err on candidateAncestor nil
		}

		for blockAncestor != nil && candidateAncestor != nil {
			if blockAncestor.Side.Parent.Equals(candidateAncestor.Side.Parent) {
				return block.Side.CumulativeDifficulty.Cmp(candidate.Side.CumulativeDifficulty) < 0, false
			}
			blockAncestor = c.getParent(blockAncestor)
			candidateAncestor = c.getParent(candidateAncestor)
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
			oldChain = c.getParent(oldChain)
		}

		if newChain != nil {
			if candidateMainchainMinHeight != 0 {
				candidateMainchainMinHeight = utils.Min(candidateMainchainMinHeight, newChain.Main.Coinbase.GenHeight)
			} else {
				candidateMainchainMinHeight = newChain.Main.Coinbase.GenHeight
			}
			candidateTotalDiff = candidateTotalDiff.Add(newChain.Side.Difficulty)

			if !newChain.Main.PreviousId.Equals(mainchainPrevId) {
				if data := c.server.GetMinimalBlockHeaderByHash(newChain.Main.PreviousId); data != nil {
					mainchainPrevId = data.Id
					candidateMainchainHeight = utils.Max(candidateMainchainHeight, data.Height)
				}
			}

			newChain = c.getParent(newChain)
		}
	}

	if blockTotalDiff.Cmp(candidateTotalDiff) >= 0 {
		return false, true
	}

	// Final check: candidate chain must be built on top of recent mainchain blocks
	if headerTip := c.server.GetChainMainTip(); headerTip != nil {
		if candidateMainchainHeight+10 < headerTip.Height {
			//TODO: warn received a longer alternative chain but it's stale: height
			return false, true
		}

		limit := c.Consensus().ChainWindowSize * 4 * c.Consensus().TargetBlockTime / monero.BlockTime
		if candidateMainchainMinHeight+limit < headerTip.Height {
			//TODO: warn received a longer alternative chain but it's stale: min height
			return false, true
		}

		return true, true
	} else {
		return false, true
	}
}
