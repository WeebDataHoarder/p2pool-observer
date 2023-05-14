package sidechain

import (
	"bytes"
	"errors"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero"
	mainblock "git.gammaspectra.live/P2Pool/p2pool-observer/monero/block"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/randomx"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/transaction"
	p2pooltypes "git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"git.gammaspectra.live/P2Pool/sha3"
	"golang.org/x/exp/slices"
	"log"
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
	UpdateTip(tip *PoolBlock)
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
	GetMinerDataTip() *p2pooltypes.MinerData
	Store(block *PoolBlock)
	ClearCachedBlocks()
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

	blocksByTemplateId map[types.Hash]*PoolBlock
	blocksByHeight     map[uint64][]*PoolBlock

	syncTip           atomic.Pointer[PoolBlock]
	chainTip          atomic.Pointer[PoolBlock]
	currentDifficulty atomic.Pointer[types.Difficulty]

	precalcFinished atomic.Bool

	preAllocatedShares                Shares
	preAllocatedSharesPool            sync.Pool
	preAllocatedDifficultyData        []DifficultyData
	preAllocatedDifficultyDifferences []uint32
}

func NewSideChain(server P2PoolInterface) *SideChain {
	s := &SideChain{
		derivationCache:                   NewDerivationCache(),
		server:                            server,
		blocksByTemplateId:                make(map[types.Hash]*PoolBlock),
		blocksByHeight:                    make(map[uint64][]*PoolBlock),
		preAllocatedShares:                PreAllocateShares(server.Consensus().ChainWindowSize * 2),
		preAllocatedDifficultyData:        make([]DifficultyData, server.Consensus().ChainWindowSize*2),
		preAllocatedDifficultyDifferences: make([]uint32, server.Consensus().ChainWindowSize*2),
	}
	s.preAllocatedSharesPool.New = func() any {
		return PreAllocateShares(s.Consensus().ChainWindowSize * 2)
	}
	minDiff := types.DifficultyFrom64(server.Consensus().MinimumDifficulty)
	s.currentDifficulty.Store(&minDiff)
	return s
}

func (c *SideChain) Consensus() *Consensus {
	return c.server.Consensus()
}

func (c *SideChain) DerivationCache() *DerivationCache {
	return c.derivationCache
}

func (c *SideChain) Difficulty() types.Difficulty {
	return *c.currentDifficulty.Load()
}

func (c *SideChain) PreCalcFinished() bool {
	return c.precalcFinished.Load()
}

func (c *SideChain) PreprocessBlock(block *PoolBlock) (missingBlocks []types.Hash, err error) {
	var preAllocatedShares Shares
	if len(block.Main.Coinbase.Outputs) == 0 {
		//cannot use SideTemplateId() as it might not be proper to calculate yet. fetch from coinbase only here
		if b := c.GetPoolBlockByTemplateId(types.HashFromBytes(block.CoinbaseExtra(SideTemplateId))); b != nil {
			block.Main.Coinbase.Outputs = b.Main.Coinbase.Outputs
		} else {
			preAllocatedShares = c.preAllocatedSharesPool.Get().(Shares)
			defer c.preAllocatedSharesPool.Put(preAllocatedShares)
		}
	}

	return block.PreProcessBlock(c.Consensus(), c.derivationCache, preAllocatedShares, c.server.GetDifficultyByHeight, c.GetPoolBlockByTemplateId)
}

func (c *SideChain) fillPoolBlockTransactionParentIndices(block *PoolBlock) {
	block.FillTransactionParentIndices(c.getParent(block))
}

func (c *SideChain) isPoolBlockTransactionKeyIsDeterministic(block *PoolBlock) bool {
	kP := c.derivationCache.GetDeterministicTransactionKey(block.GetPrivateKeySeed(), block.Main.PreviousId)
	return bytes.Compare(block.CoinbaseExtra(SideCoinbasePublicKey), kP.PublicKey.AsSlice()) == 0 && bytes.Compare(kP.PrivateKey.AsSlice(), block.Side.CoinbasePrivateKey[:]) == 0
}

func (c *SideChain) getSeedByHeightFunc() mainblock.GetSeedByHeightFunc {
	//TODO: do not make this return a function
	return func(height uint64) (hash types.Hash) {
		seedHeight := randomx.SeedHeight(height)
		if h := c.server.GetMinimalBlockHeaderByHeight(seedHeight); h != nil {
			return h.Id
		} else {
			return types.ZeroHash
		}
	}
}

func (c *SideChain) AddPoolBlockExternal(block *PoolBlock) (missingBlocks []types.Hash, err error, ban bool) {
	// Technically some p2pool node could keep stuffing block with transactions until reward is less than 0.6 XMR
	// But default transaction picking algorithm never does that. It's better to just ban such nodes
	if block.Main.Coinbase.TotalReward < monero.TailEmissionReward {
		return nil, errors.New("block reward too low"), true
	}

	// Enforce deterministic tx keys starting from v15
	if block.Main.MajorVersion >= monero.HardForkViewTagsVersion {
		if !c.isPoolBlockTransactionKeyIsDeterministic(block) {
			return nil, errors.New("invalid deterministic transaction keys"), true
		}
	}

	// Both tx types are allowed by Monero consensus during v15 because it needs to process pre-fork mempool transactions,
	// but P2Pool can switch to using only TXOUT_TO_TAGGED_KEY for miner payouts starting from v15
	expectedTxType := block.GetTransactionOutputType()

	if missingBlocks, err = c.PreprocessBlock(block); err != nil {
		return missingBlocks, err, true
	}
	for _, o := range block.Main.Coinbase.Outputs {
		if o.Type != expectedTxType {
			return nil, errors.New("unexpected transaction type"), true
		}
	}

	templateId := c.Consensus().CalculateSideTemplateId(block)
	if templateId != block.SideTemplateId(c.Consensus()) {
		return nil, fmt.Errorf("invalid template id %s, expected %s", templateId.String(), block.SideTemplateId(c.Consensus()).String()), true
	}

	if block.Side.Difficulty.Cmp64(c.Consensus().MinimumDifficulty) < 0 {
		return nil, fmt.Errorf("block mined by %s has invalid difficulty %s, expected >= %d", block.GetAddress().Reference().ToBase58(), block.Side.Difficulty.StringNumeric(), c.Consensus().MinimumDifficulty), true
	}

	//TODO: cache?
	expectedDifficulty := c.Difficulty()
	tooLowDiff := block.Side.Difficulty.Cmp(expectedDifficulty) < 0

	if otherBlock := c.GetPoolBlockByTemplateId(templateId); otherBlock != nil {
		//already added
		//TODO: specifically check Main id for nonce changes! p2pool does not do this
		return nil, nil, false
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

	if tooLowDiff {
		return nil, fmt.Errorf("block mined by %s has too low difficulty %s, expected >= %s", block.GetAddress().Reference().ToBase58(), block.Side.Difficulty.StringNumeric(), expectedDifficulty.StringNumeric()), false
	}

	// This check is not always possible to perform because of mainchain reorgs
	if data := c.server.GetChainMainByHash(block.Main.PreviousId); data != nil {
		if (data.Height + 1) != block.Main.Coinbase.GenHeight {
			return nil, fmt.Errorf("wrong mainchain height %d, expected %d", block.Main.Coinbase.GenHeight, data.Height+1), true
		}
	} else {
		//TODO warn unknown block, reorg
	}

	if _, err := block.PowHashWithError(c.Consensus().GetHasher(), c.getSeedByHeightFunc()); err != nil {
		return nil, err, false
	} else {
		if isHigherMainChain, err := block.IsProofHigherThanMainDifficultyWithError(c.Consensus().GetHasher(), c.server.GetDifficultyByHeight, c.getSeedByHeightFunc()); err != nil {
			log.Printf("[SideChain] add_external_block: couldn't get mainchain difficulty for height = %d: %s", block.Main.Coinbase.GenHeight, err)
		} else if isHigherMainChain {
			log.Printf("[SideChain]: add_external_block: block %s has enough PoW for Monero height %d, submitting it", templateId.String(), block.Main.Coinbase.GenHeight)
			c.server.SubmitBlock(&block.Main)
		}
		if isHigher, err := block.IsProofHigherThanDifficultyWithError(c.Consensus().GetHasher(), c.getSeedByHeightFunc()); err != nil {
			return nil, err, true
		} else if !isHigher {
			return nil, fmt.Errorf("not enough PoW for id %s, height = %d, mainchain height %d", templateId.String(), block.Side.Height, block.Main.Coinbase.GenHeight), true
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
	}(), c.AddPoolBlock(block), true
}

func (c *SideChain) AddPoolBlock(block *PoolBlock) (err error) {

	c.sidechainLock.Lock()
	defer c.sidechainLock.Unlock()
	if _, ok := c.blocksByTemplateId[block.SideTemplateId(c.Consensus())]; ok {
		//already inserted
		//TODO WARN
		return nil
	}

	if _, err := block.MarshalBinary(); err != nil {
		return fmt.Errorf("encoding block error: %w", err)
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

	defer func() {
		if !block.Invalid.Load() && block.Depth.Load() == 0 {
			c.syncTip.Store(block)
		}
	}()

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
			log.Printf("[SideChain] block at height = %d, id = %s, mainchain height = %d, mined by %s is invalid: %s", block.Side.Height, block.SideTemplateId(c.Consensus()), block.Main.Coinbase.GenHeight, block.GetAddress().Reference().ToBase58(), invalid.Error())
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

			if block.ShareVersion() > ShareVersion_V1 {
				log.Printf("[SideChain] verified block at height = %d, depth = %d, id = %s, mainchain height = %d, mined by %s via %s %s", block.Side.Height, block.Depth.Load(), block.SideTemplateId(c.Consensus()), block.Main.Coinbase.GenHeight, block.GetAddress().Reference().ToBase58(), block.Side.ExtraBuffer.SoftwareId, block.Side.ExtraBuffer.SoftwareVersion)
			} else {
				if signalingVersion := block.ShareVersionSignaling(); signalingVersion > ShareVersion_None {
					log.Printf("[SideChain] verified block at height = %d, depth = %d, id = %s, mainchain height = %d, mined by %s, signaling v%d", block.Side.Height, block.Depth.Load(), block.SideTemplateId(c.Consensus()), block.Main.Coinbase.GenHeight, block.GetAddress().Reference().ToBase58(), signalingVersion)
				} else {
					log.Printf("[SideChain] verified block at height = %d, depth = %d, id = %s, mainchain height = %d, mined by %s", block.Side.Height, block.Depth.Load(), block.SideTemplateId(c.Consensus()), block.Main.Coinbase.GenHeight, block.GetAddress().Reference().ToBase58())
				}
			}

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
			block.Side.CumulativeDifficulty.Cmp64(c.Consensus().MinimumDifficulty) != 0 ||
			(block.ShareVersion() > ShareVersion_V1 && block.Side.CoinbasePrivateKeySeed == types.ZeroHash) {
			return nil, errors.New("genesis block has invalid parameters")
		}
		//this does not verify coinbase outputs, but that's fine
		return nil, nil
	}

	// Deep block
	//
	// Blocks in PPLNS window (m_chainWindowSize) require up to m_chainWindowSize earlier blocks to verify
	// If a block is deeper than (m_chainWindowSize - 1) * 2 + UNCLE_BLOCK_DEPTH it can't influence blocks in PPLNS window
	// Also, having so many blocks on top of this one means it was verified by the network at some point
	// We skip checks in this case to make pruning possible
	if block.Depth.Load() > ((c.Consensus().ChainWindowSize-1)*2 + UncleBlockDepth) {
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

		if block.ShareVersion() > ShareVersion_V1 {
			expectedSeed := parent.Side.CoinbasePrivateKeySeed
			if parent.Main.PreviousId != block.Main.PreviousId {
				expectedSeed = parent.CalculateTransactionPrivateKeySeed()
			}
			if block.Side.CoinbasePrivateKeySeed != expectedSeed {
				return nil, fmt.Errorf("invalid tx key seed: expected %s, got %s", expectedSeed.String(), block.Side.CoinbasePrivateKeySeed.String())
			}
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

		var diff types.Difficulty

		if parent == c.GetChainTip() {
			// built on top of the current chain tip, using current difficulty for verification
			diff = c.Difficulty()
		} else if diff, verification, invalid = c.getDifficulty(parent); verification != nil || invalid != nil {
			return verification, invalid
		} else if diff == types.ZeroDifficulty {
			return nil, errors.New("could not get difficulty")
		}
		if diff != block.Side.Difficulty {
			return nil, fmt.Errorf("wrong difficulty, got %s, expected %s", block.Side.Difficulty.StringNumeric(), diff.StringNumeric())
		}

		if shares, _ := c.getShares(block, c.preAllocatedShares); len(shares) == 0 {
			return nil, errors.New("could not get outputs")
		} else if len(shares) != len(block.Main.Coinbase.Outputs) {
			return nil, fmt.Errorf("invalid number of outputs, got %d, expected %d", len(block.Main.Coinbase.Outputs), len(shares))
		} else if totalReward := func() (result uint64) {
			for _, o := range block.Main.Coinbase.Outputs {
				result += o.Reward
			}
			return
		}(); totalReward != block.Main.Coinbase.TotalReward {
			return nil, fmt.Errorf("invalid total reward, got %d, expected %d", block.Main.Coinbase.TotalReward, totalReward)
		} else if rewards := SplitReward(totalReward, shares); len(rewards) != len(block.Main.Coinbase.Outputs) {
			return nil, fmt.Errorf("invalid number of outputs, got %d, expected %d", len(block.Main.Coinbase.Outputs), len(rewards))
		} else {

			//prevent multiple allocations
			txPrivateKeySlice := block.Side.CoinbasePrivateKey.AsSlice()
			txPrivateKeyScalar := block.Side.CoinbasePrivateKey.AsScalar()

			var hashers []*sha3.HasherState

			results := utils.SplitWork(-2, uint64(len(rewards)), func(workIndex uint64, workerIndex int) error {
				out := block.Main.Coinbase.Outputs[workIndex]
				if rewards[workIndex] != out.Reward {
					return fmt.Errorf("has invalid reward at index %d, got %d, expected %d", workIndex, out.Reward, rewards[workIndex])
				}

				if ephPublicKey, viewTag := c.derivationCache.GetEphemeralPublicKey(&shares[workIndex].Address, txPrivateKeySlice, txPrivateKeyScalar, workIndex, hashers[workerIndex]); ephPublicKey != out.EphemeralPublicKey {
					return fmt.Errorf("has incorrect eph_public_key at index %d, got %s, expected %s", workIndex, out.EphemeralPublicKey.String(), ephPublicKey.String())
				} else if out.Type == transaction.TxOutToTaggedKey && viewTag != out.ViewTag {
					return fmt.Errorf("has incorrect view tag at index %d, got %d, expected %d", workIndex, out.ViewTag, viewTag)
				}
				return nil
			}, func(routines, routineIndex int) error {
				hashers = append(hashers, crypto.GetKeccak256Hasher())
				return nil
			})

			defer func() {
				for _, h := range hashers {
					crypto.PutKeccak256Hasher(h)
				}
			}()

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

	if block == tip {
		log.Printf("[SideChain] Trying to update chain tip to the same block again. Ignoring it.")
		return
	}

	if isLongerChain, isAlternative := c.isLongerChain(tip, block); isLongerChain {
		if diff, _, _ := c.getDifficulty(block); diff != types.ZeroDifficulty {
			c.chainTip.Store(block)
			c.syncTip.Store(block)
			c.currentDifficulty.Store(&diff)
			//TODO log

			block.WantBroadcast.Store(true)
			c.server.UpdateTip(block)

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

func (c *SideChain) calculateOutputs(block *PoolBlock) (outputs transaction.Outputs, bottomHeight uint64) {
	preAllocatedShares := c.preAllocatedSharesPool.Get().(Shares)
	defer c.preAllocatedSharesPool.Put(preAllocatedShares)
	return CalculateOutputs(block, c.Consensus(), c.server.GetDifficultyByHeight, c.getPoolBlockByTemplateId, c.derivationCache, preAllocatedShares)
}

func (c *SideChain) getShares(tip *PoolBlock, preAllocatedShares Shares) (shares Shares, bottomHeight uint64) {
	return GetShares(tip, c.Consensus(), c.server.GetDifficultyByHeight, c.getPoolBlockByTemplateId, preAllocatedShares)
}

type DifficultyData struct {
	CumulativeDifficulty types.Difficulty
	Timestamp            uint64
}

func (c *SideChain) GetDifficulty(tip *PoolBlock) (difficulty types.Difficulty, verifyError, invalidError error) {
	c.sidechainLock.RLock()
	defer c.sidechainLock.RUnlock()
	return c.getDifficulty(tip)
}

func (c *SideChain) getDifficulty(tip *PoolBlock) (difficulty types.Difficulty, verifyError, invalidError error) {
	return GetDifficulty(tip, c.Consensus(), c.getPoolBlockByTemplateId, c.preAllocatedDifficultyData, c.preAllocatedDifficultyDifferences)
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

func (c *SideChain) GetPoolBlocksFromTip(id types.Hash) (chain, uncles UniquePoolBlockSlice) {
	chain = make([]*PoolBlock, 0, c.Consensus().ChainWindowSize*2+monero.BlockTime/c.Consensus().TargetBlockTime)
	uncles = make([]*PoolBlock, 0, len(chain)/20)

	c.sidechainLock.RLock()
	defer c.sidechainLock.RUnlock()
	for cur := c.getPoolBlockByTemplateId(id); cur != nil; cur = c.getPoolBlockByTemplateId(cur.Side.Parent) {
		for i, uncleId := range cur.Side.Uncles {
			if u := c.getPoolBlockByTemplateId(uncleId); u == nil {
				//return few uncles than necessary
				return chain, uncles[:len(uncles)-i]
			} else {
				uncles = append(uncles, u)
			}
		}
		chain = append(chain, cur)
	}

	return chain, uncles
}

func (c *SideChain) GetPoolBlocksFromTipWithDepth(id types.Hash, depth uint64) (chain, uncles UniquePoolBlockSlice) {
	chain = make([]*PoolBlock, 0, utils.Min(depth, c.Consensus().ChainWindowSize*2+monero.BlockTime/c.Consensus().TargetBlockTime))
	uncles = make([]*PoolBlock, 0, len(chain)/20)

	c.sidechainLock.RLock()
	defer c.sidechainLock.RUnlock()
	for cur := c.getPoolBlockByTemplateId(id); cur != nil && len(chain) < int(depth); cur = c.getPoolBlockByTemplateId(cur.Side.Parent) {
		for i, uncleId := range cur.Side.Uncles {
			if u := c.getPoolBlockByTemplateId(uncleId); u == nil {
				//return few uncles than necessary
				return chain, uncles[:len(uncles)-i]
			} else {
				uncles = append(uncles, u)
			}
		}
		chain = append(chain, cur)
	}

	return chain, uncles
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

func (c *SideChain) GetHighestKnownTip() *PoolBlock {
	if t := c.chainTip.Load(); t != nil {
		return t
	}
	return c.syncTip.Load()
}

func (c *SideChain) GetChainTip() *PoolBlock {
	return c.chainTip.Load()
}

func (c *SideChain) LastUpdated() uint64 {
	if tip := c.chainTip.Load(); tip != nil {
		return tip.LocalTimestamp
	}
	return 0
}

func (c *SideChain) IsLongerChain(block, candidate *PoolBlock) (isLonger, isAlternative bool) {
	c.sidechainLock.RLock()
	defer c.sidechainLock.RUnlock()
	return c.isLongerChain(block, candidate)
}

func (c *SideChain) isLongerChain(block, candidate *PoolBlock) (isLonger, isAlternative bool) {
	return IsLongerChain(block, candidate, c.Consensus(), c.getPoolBlockByTemplateId, func(h types.Hash) *ChainMain {
		if h == types.ZeroHash {
			return c.server.GetChainMainTip()
		}
		return c.server.GetChainMainByHash(h)
	})
}
