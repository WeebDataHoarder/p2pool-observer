package stratum

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/block"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/transaction"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/mempool"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	p2pooltypes "git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	gojson "github.com/goccy/go-json"
	"golang.org/x/exp/slices"
	"log"
	"math"
	unsafeRandom "math/rand"
	"net"
	"net/netip"
	"sync"
	"time"
)

type ephemeralPubKeyCacheKey [crypto.PublicKeySize*2 + 8]byte

type ephemeralPubKeyCacheEntry struct {
	PublicKey crypto.PublicKeyBytes
	ViewTag   uint8
}

type NewTemplateData struct {
	PreviousTemplateId        types.Hash
	SideHeight                uint64
	Difficulty                types.Difficulty
	CumulativeDifficulty      types.Difficulty
	TransactionPrivateKeySeed types.Hash
	// TransactionPrivateKey Generated from TransactionPrivateKeySeed
	TransactionPrivateKey  crypto.PrivateKeyBytes
	TransactionPublicKey   crypto.PublicKeySlice
	Timestamp              uint64
	TotalReward            uint64
	Transactions           []types.Hash
	MaxRewardAmountsWeight uint64
	ShareVersion           sidechain.ShareVersion
	Uncles                 []types.Hash
	Ready                  bool
	Window                 struct {
		ReservedShareIndex   int
		Shares               sidechain.Shares
		ShuffleMapping       [2][]int
		EphemeralPubKeyCache map[ephemeralPubKeyCacheKey]*ephemeralPubKeyCacheEntry
	}
}

type Server struct {
	SubmitFunc func(block *sidechain.PoolBlock) error

	minerData       *p2pooltypes.MinerData
	tip             *sidechain.PoolBlock
	newTemplateData NewTemplateData
	lock            sync.RWMutex
	sidechain       *sidechain.SideChain

	mempool            mempool.Mempool
	lastMempoolRefresh time.Time

	preAllocatedDifficultyData        []sidechain.DifficultyData
	preAllocatedDifficultyDifferences []uint32
	preAllocatedSharesPool            *sidechain.PreAllocatedSharesPool

	preAllocatedBufferLock sync.Mutex
	preAllocatedBuffer     []byte

	minersLock sync.RWMutex
	miners     map[address.PackedAddress]*MinerTrackingEntry

	bansLock sync.RWMutex
	bans     map[[16]byte]BanEntry

	clientsLock sync.RWMutex
	clients     []*Client

	incomingChanges chan func() bool
}

type Client struct {
	Lock     sync.RWMutex
	Conn     *net.TCPConn
	encoder  *gojson.Encoder
	decoder  *gojson.Decoder
	Agent    string
	Login    bool
	Address  address.PackedAddress
	Password string
	RigId    string
	buf      []byte
	RpcId    uint32
}

func (c *Client) Write(b []byte) (int, error) {
	if err := c.Conn.SetWriteDeadline(time.Now().Add(time.Second * 5)); err != nil {
		return 0, err
	}
	return c.Conn.Write(b)
}

func NewServer(s *sidechain.SideChain, submitFunc func(block *sidechain.PoolBlock) error) *Server {
	server := &Server{
		SubmitFunc:                        submitFunc,
		sidechain:                         s,
		preAllocatedDifficultyData:        make([]sidechain.DifficultyData, s.Consensus().ChainWindowSize*2),
		preAllocatedDifficultyDifferences: make([]uint32, s.Consensus().ChainWindowSize*2),
		preAllocatedSharesPool:            sidechain.NewPreAllocatedSharesPool(s.Consensus().ChainWindowSize * 2),
		preAllocatedBuffer:                make([]byte, 0, sidechain.PoolBlockMaxTemplateSize),
		miners:                            make(map[address.PackedAddress]*MinerTrackingEntry),
		// buffer 4 at a time for non-blocking source
		incomingChanges: make(chan func() bool, 4),
	}
	return server
}

func (s *Server) CleanupMiners() {
	s.minersLock.Lock()
	defer s.minersLock.Unlock()

	cleanupTime := time.Now()
	for k, e := range s.miners {
		if cleanupTime.Sub(e.LastJob) > time.Minute*5 {
			delete(s.miners, k)
		} else {
			if len(e.Templates) > 0 {
				var templateSideHeight uint64
				for _, tpl := range e.Templates {
					if tpl.SideHeight > templateSideHeight {
						templateSideHeight = tpl.SideHeight
					}
				}

				tipHeight := uint64(0)
				if templateSideHeight > sidechain.UncleBlockDepth {
					tipHeight = templateSideHeight - sidechain.UncleBlockDepth + 1
				}
				//Delete old templates further than uncle depth
				for key, tpl := range e.Templates {
					if tpl.SideHeight < tipHeight {
						delete(e.Templates, key)
					}
				}

				// TODO: Prevent long-term leaks by re-allocating templates if capacity grew too much
			}
		}
	}
}

func (s *Server) fillNewTemplateData(currentDifficulty types.Difficulty) error {

	s.newTemplateData.Ready = false

	if s.minerData == nil {
		return errors.New("no main data present")
	}

	if s.minerData.MajorVersion > monero.HardForkSupportedVersion {
		return fmt.Errorf("unsupported hardfork version %d", s.minerData.MajorVersion)
	}

	oldPubKeyCache := s.newTemplateData.Window.EphemeralPubKeyCache

	s.newTemplateData.Timestamp = uint64(time.Now().Unix())

	s.newTemplateData.ShareVersion = sidechain.P2PoolShareVersion(s.sidechain.Consensus(), s.newTemplateData.Timestamp)

	// Do not allow mining on old chains, as they are not optimal for CPU usage
	if s.newTemplateData.ShareVersion < sidechain.ShareVersion_V2 {
		return errors.New("unsupported sidechain version")
	}

	if s.tip != nil {
		s.newTemplateData.PreviousTemplateId = s.tip.SideTemplateId(s.sidechain.Consensus())
		s.newTemplateData.SideHeight = s.tip.Side.Height + 1

		oldSeed := s.newTemplateData.TransactionPrivateKeySeed

		s.newTemplateData.TransactionPrivateKeySeed = s.tip.Side.CoinbasePrivateKeySeed
		if s.tip.Main.PreviousId != s.minerData.PrevId {
			s.newTemplateData.TransactionPrivateKeySeed = s.tip.CalculateTransactionPrivateKeySeed()
		}

		//TODO: check this
		if s.newTemplateData.TransactionPrivateKeySeed != oldSeed {
			oldPubKeyCache = nil
		}

		if currentDifficulty != types.ZeroDifficulty {
			//difficulty is set from caller
			s.newTemplateData.Difficulty = currentDifficulty
			oldPubKeyCache = nil
		}

		s.newTemplateData.CumulativeDifficulty = s.tip.Side.CumulativeDifficulty.Add(s.newTemplateData.Difficulty)

		s.newTemplateData.Uncles = s.sidechain.GetPossibleUncles(s.tip, s.newTemplateData.SideHeight)
	} else {
		s.newTemplateData.PreviousTemplateId = types.ZeroHash
		s.newTemplateData.TransactionPrivateKeySeed = s.sidechain.Consensus().Id
		s.newTemplateData.Difficulty = types.DifficultyFrom64(s.sidechain.Consensus().MinimumDifficulty)
		s.newTemplateData.CumulativeDifficulty = types.DifficultyFrom64(s.sidechain.Consensus().MinimumDifficulty)
	}

	kP := s.sidechain.DerivationCache().GetDeterministicTransactionKey(s.newTemplateData.TransactionPrivateKeySeed, s.minerData.PrevId)
	s.newTemplateData.TransactionPrivateKey = kP.PrivateKey.AsBytes()
	s.newTemplateData.TransactionPublicKey = kP.PublicKey.AsSlice()

	fakeTemplateTipBlock := &sidechain.PoolBlock{
		Main: block.Block{
			MajorVersion: s.minerData.MajorVersion,
			MinorVersion: monero.HardForkSupportedVersion,
			Timestamp:    s.newTemplateData.Timestamp,
			PreviousId:   s.minerData.PrevId,
			Nonce:        0,
			Coinbase: &transaction.CoinbaseTransaction{
				GenHeight: s.minerData.Height,
			},
			//TODO:
			Transactions: nil,
		},
		Side: sidechain.SideData{
			//Zero Spend/View key
			PublicSpendKey:         crypto.PublicKeyBytes{},
			PublicViewKey:          crypto.PublicKeyBytes{},
			CoinbasePrivateKeySeed: s.newTemplateData.TransactionPrivateKeySeed,
			CoinbasePrivateKey:     s.newTemplateData.TransactionPrivateKey,
			Parent:                 s.newTemplateData.PreviousTemplateId,
			Uncles:                 s.newTemplateData.Uncles,
			Height:                 s.newTemplateData.SideHeight,
			Difficulty:             s.newTemplateData.Difficulty,
			CumulativeDifficulty:   s.newTemplateData.CumulativeDifficulty,
		},
		CachedShareVersion: s.newTemplateData.ShareVersion,
	}

	preAllocatedShares := s.preAllocatedSharesPool.Get()
	defer s.preAllocatedSharesPool.Put(preAllocatedShares)
	shares, _ := sidechain.GetSharesOrdered(fakeTemplateTipBlock, s.sidechain.Consensus(), s.sidechain.Server().GetDifficultyByHeight, s.sidechain.GetPoolBlockByTemplateId, preAllocatedShares)

	if shares == nil {
		return errors.New("could not get outputs")
	}

	s.newTemplateData.Window.Shares = slices.Clone(shares)
	s.newTemplateData.Window.ReservedShareIndex = s.newTemplateData.Window.Shares.Index(fakeTemplateTipBlock.GetAddress())

	if s.newTemplateData.Window.ReservedShareIndex == -1 {
		return errors.New("could not find reserved share index")
	}

	baseReward := block.GetBaseReward(s.minerData.AlreadyGeneratedCoins)

	totalWeight, totalFees := s.mempool.WeightAndFees()

	maxReward := baseReward + totalFees

	rewards := sidechain.SplitReward(maxReward, s.newTemplateData.Window.Shares)

	s.newTemplateData.MaxRewardAmountsWeight = uint64(utils.UVarInt64SliceSize(rewards))

	tx, err := s.createCoinbaseTransaction(fakeTemplateTipBlock.GetTransactionOutputType(), s.newTemplateData.Window.Shares, rewards, s.newTemplateData.MaxRewardAmountsWeight, false)
	if err != nil {
		return err
	}
	coinbaseTransactionWeight := uint64(tx.BufferLength())

	var selectedMempool mempool.Mempool

	if totalWeight+coinbaseTransactionWeight <= s.minerData.MedianWeight {
		// if a block doesn't get into the penalty zone, just pick all transactions
		selectedMempool = s.mempool
	} else {
		selectedMempool = s.mempool.Pick(baseReward, coinbaseTransactionWeight, s.minerData.MedianWeight)
	}

	s.newTemplateData.Transactions = make([]types.Hash, len(selectedMempool))

	for i, entry := range selectedMempool {
		s.newTemplateData.Transactions[i] = entry.Id
	}

	finalReward := mempool.GetBlockReward(baseReward, s.minerData.MedianWeight, selectedMempool.Fees(), coinbaseTransactionWeight+selectedMempool.Weight())

	if finalReward < baseReward {
		return errors.New("final reward < base reward, should never happen")
	}
	s.newTemplateData.TotalReward = finalReward

	s.newTemplateData.Window.ShuffleMapping = BuildShuffleMapping(len(s.newTemplateData.Window.Shares), s.newTemplateData.ShareVersion, s.newTemplateData.TransactionPrivateKeySeed)

	s.newTemplateData.Window.EphemeralPubKeyCache = make(map[ephemeralPubKeyCacheKey]*ephemeralPubKeyCacheEntry)

	txPrivateKeySlice := s.newTemplateData.TransactionPrivateKey.AsSlice()
	txPrivateKeyScalar := s.newTemplateData.TransactionPrivateKey.AsScalar()

	//TODO: parallelize this
	hasher := crypto.GetKeccak256Hasher()
	defer crypto.PutKeccak256Hasher(hasher)
	for i, m := range PossibleIndicesForShuffleMapping(s.newTemplateData.Window.ShuffleMapping) {
		if i == 0 {
			// Skip zero key
			continue
		}
		share := s.newTemplateData.Window.Shares[i]
		var k ephemeralPubKeyCacheKey
		copy(k[:], share.Address.Bytes())

		for _, index := range m {
			if index == -1 {
				continue
			}
			binary.LittleEndian.PutUint64(k[crypto.PublicKeySize*2:], uint64(index))
			if e, ok := oldPubKeyCache[k]; ok {
				s.newTemplateData.Window.EphemeralPubKeyCache[k] = e
			} else {
				var e ephemeralPubKeyCacheEntry
				e.PublicKey, e.ViewTag = s.sidechain.DerivationCache().GetEphemeralPublicKey(&share.Address, txPrivateKeySlice, txPrivateKeyScalar, uint64(index), hasher)
				s.newTemplateData.Window.EphemeralPubKeyCache[k] = &e
			}
		}
	}

	s.newTemplateData.Ready = true

	return nil

}

func BuildShuffleMapping(n int, shareVersion sidechain.ShareVersion, transactionPrivateKeySeed types.Hash) (mappings [2][]int) {
	if n <= 1 {
		return [2][]int{{0}, {0}}
	}
	shuffleSequence1 := make([]int, n)
	for i := range shuffleSequence1 {
		shuffleSequence1[i] = i
	}
	shuffleSequence2 := make([]int, n-1)
	for i := range shuffleSequence2 {
		shuffleSequence2[i] = i
	}

	sidechain.ShuffleSequence(shareVersion, transactionPrivateKeySeed, n, func(i, j int) {
		shuffleSequence1[i], shuffleSequence1[j] = shuffleSequence1[j], shuffleSequence1[i]
	})
	sidechain.ShuffleSequence(shareVersion, transactionPrivateKeySeed, n-1, func(i, j int) {
		shuffleSequence2[i], shuffleSequence2[j] = shuffleSequence2[j], shuffleSequence2[i]
	})

	mappings[0] = make([]int, n)
	mappings[1] = make([]int, n-1)

	//Flip
	for i := range shuffleSequence1 {
		mappings[0][shuffleSequence1[i]] = i
	}
	for i := range shuffleSequence2 {
		mappings[1][shuffleSequence2[i]] = i
	}

	return mappings
}

func ApplyShuffleMapping[T any](v []T, mappings [2][]int) []T {
	n := len(v)

	result := make([]T, n)

	if n == len(mappings[0]) {
		for i := range v {
			result[mappings[0][i]] = v[i]
		}
	} else if n == len(mappings[1]) {
		for i := range v {
			result[mappings[1][i]] = v[i]
		}
	}
	return result
}

func PossibleIndicesForShuffleMapping(mappings [2][]int) [][3]int {
	n := len(mappings[0])
	result := make([][3]int, n)
	for i := 0; i < n; i++ {
		// Count with all + miner
		result[i][0] = mappings[0][i]
		if i > 0 {
			// Count with all + miner shifted to a slot before
			result[i][1] = mappings[0][i-1]

			// Count with all miners minus one
			result[i][2] = mappings[1][i-1]
		} else {
			result[i][1] = -1
			result[i][2] = -1
		}
	}

	return result
}

func (s *Server) BuildTemplate(addr address.PackedAddress, forceNewTemplate bool) (tpl *Template, jobCounter uint64, difficultyTarget types.Difficulty, seedHash types.Hash, err error) {

	var zeroAddress address.PackedAddress
	if addr == zeroAddress {
		return nil, 0, types.ZeroDifficulty, types.ZeroHash, errors.New("nil address")
	}

	e, ok := func() (*MinerTrackingEntry, bool) {
		s.minersLock.RLock()
		defer s.minersLock.RUnlock()
		e, ok := s.miners[addr]
		return e, ok
	}()

	tpl, jobCounter, targetDiff, seedHash, err := func() (tpl *Template, jobCounter uint64, difficultyTarget types.Difficulty, seedHash types.Hash, err error) {
		s.lock.RLock()
		defer s.lock.RUnlock()

		if s.minerData == nil {
			return nil, 0, types.ZeroDifficulty, types.ZeroHash, errors.New("nil miner data")
		}

		if !s.newTemplateData.Ready {
			return nil, 0, types.ZeroDifficulty, types.ZeroHash, errors.New("template data not ready")
		}

		if !forceNewTemplate && e != nil && ok {
			if tpl, jobCounter := func() (*Template, uint64) {
				e.Lock.RLock()
				defer e.Lock.RUnlock()

				jobCounter := e.LastTemplate.Load()

				if tpl, ok := e.Templates[jobCounter]; ok && tpl.SideParent == s.newTemplateData.PreviousTemplateId && tpl.MainParent == s.minerData.PrevId {
					return tpl, jobCounter
				}
				return nil, 0
			}(); tpl != nil {
				e.Lock.Lock()
				defer e.Lock.Unlock()
				e.LastJob = time.Now()

				targetDiff := tpl.SideDifficulty
				if s.minerData.Difficulty.Cmp(targetDiff) < 0 {
					targetDiff = s.minerData.Difficulty
				}

				return tpl, jobCounter, targetDiff, s.minerData.SeedHash, nil
			}
		}

		blockTemplate := &sidechain.PoolBlock{
			Main: block.Block{
				MajorVersion: s.minerData.MajorVersion,
				MinorVersion: monero.HardForkSupportedVersion,
				Timestamp:    s.newTemplateData.Timestamp,
				PreviousId:   s.minerData.PrevId,
				Nonce:        0,
				Transactions: s.newTemplateData.Transactions,
			},
			Side: sidechain.SideData{
				PublicSpendKey:         *addr.SpendPublicKey(),
				PublicViewKey:          *addr.ViewPublicKey(),
				CoinbasePrivateKeySeed: s.newTemplateData.TransactionPrivateKeySeed,
				CoinbasePrivateKey:     s.newTemplateData.TransactionPrivateKey,
				Parent:                 s.newTemplateData.PreviousTemplateId,
				Uncles:                 s.newTemplateData.Uncles,
				Height:                 s.newTemplateData.SideHeight,
				Difficulty:             s.newTemplateData.Difficulty,
				CumulativeDifficulty:   s.newTemplateData.CumulativeDifficulty,
				ExtraBuffer: struct {
					SoftwareId          p2pooltypes.SoftwareId
					SoftwareVersion     p2pooltypes.SoftwareVersion
					RandomNumber        uint32
					SideChainExtraNonce uint32
				}{
					SoftwareId:          p2pooltypes.CurrentSoftwareId,
					SoftwareVersion:     p2pooltypes.CurrentSoftwareVersion,
					RandomNumber:        0,
					SideChainExtraNonce: 0,
				},
			},
			CachedShareVersion: s.newTemplateData.ShareVersion,
		}

		preAllocatedShares := s.preAllocatedSharesPool.Get()
		defer s.preAllocatedSharesPool.Put(preAllocatedShares)

		shares := s.newTemplateData.Window.Shares.Clone()

		// It exists, replace
		if i := shares.Index(addr); i != -1 {
			shares[i] = &sidechain.Share{
				Address: addr,
				Weight:  shares[i].Weight.Add(shares[s.newTemplateData.Window.ReservedShareIndex].Weight),
			}
			shares = slices.Delete(shares, s.newTemplateData.Window.ReservedShareIndex, s.newTemplateData.Window.ReservedShareIndex+1)
		} else {
			// Replace reserved address
			shares[s.newTemplateData.Window.ReservedShareIndex] = &sidechain.Share{
				Weight:  shares[s.newTemplateData.Window.ReservedShareIndex].Weight,
				Address: addr,
			}
		}
		shares = shares.Compact()

		// Apply consensus shuffle
		shares = ApplyShuffleMapping(shares, s.newTemplateData.Window.ShuffleMapping)

		// Allocate rewards
		{
			preAllocatedRewards := make([]uint64, 0, len(shares))
			rewards := sidechain.SplitRewardNoAllocate(preAllocatedRewards, s.newTemplateData.TotalReward, shares)

			if rewards == nil || len(rewards) != len(shares) {
				return nil, 0, types.ZeroDifficulty, types.ZeroHash, errors.New("could not calculate rewards")
			}

			if blockTemplate.Main.Coinbase, err = s.createCoinbaseTransaction(blockTemplate.GetTransactionOutputType(), shares, rewards, s.newTemplateData.MaxRewardAmountsWeight, true); err != nil {
				return nil, 0, types.ZeroDifficulty, types.ZeroHash, err
			}
		}

		tpl, err = TemplateFromPoolBlock(blockTemplate)
		if err != nil {
			return nil, 0, types.ZeroDifficulty, types.ZeroHash, err
		}

		targetDiff := tpl.SideDifficulty
		if s.minerData.Difficulty.Cmp(targetDiff) < 0 {
			targetDiff = s.minerData.Difficulty
		}

		return tpl, 0, targetDiff, s.minerData.SeedHash, nil
	}()

	if err != nil {
		return nil, 0, types.ZeroDifficulty, types.ZeroHash, err
	}

	if forceNewTemplate || jobCounter != 0 {
		return tpl, jobCounter, targetDiff, seedHash, nil
	}

	if e != nil && ok {
		e.Lock.Lock()
		defer e.Lock.Unlock()
		var newJobCounter uint64
		for newJobCounter == 0 {
			newJobCounter = e.Counter.Add(1)
		}
		e.Templates[newJobCounter] = tpl
		e.LastTemplate.Store(newJobCounter)
		e.LastJob = time.Now()
		return tpl, newJobCounter, targetDiff, seedHash, nil
	} else {
		s.minersLock.Lock()
		defer s.minersLock.Unlock()

		e = &MinerTrackingEntry{
			Templates: make(map[uint64]*Template),
		}
		var newJobCounter uint64
		for newJobCounter == 0 {
			newJobCounter = e.Counter.Add(1)
		}
		e.Templates[newJobCounter] = tpl
		e.LastTemplate.Store(newJobCounter)
		e.LastJob = time.Now()
		s.miners[addr] = e
		return tpl, newJobCounter, targetDiff, seedHash, nil
	}
}

func (s *Server) createCoinbaseTransaction(txType uint8, shares sidechain.Shares, rewards []uint64, maxRewardsAmountsWeight uint64, final bool) (tx *transaction.CoinbaseTransaction, err error) {
	tx = &transaction.CoinbaseTransaction{
		Version:    2,
		UnlockTime: s.minerData.Height + monero.MinerRewardUnlockTime,
		InputCount: 1,
		InputType:  transaction.TxInGen,
		GenHeight:  s.minerData.Height,
		TotalReward: func() (v uint64) {
			for i := range rewards {
				v += rewards[i]
			}
			return
		}(),
		Extra: transaction.ExtraTags{
			transaction.ExtraTag{
				Tag:    transaction.TxExtraTagPubKey,
				VarInt: 0,
				Data:   types.Bytes(s.newTemplateData.TransactionPublicKey),
			},
			transaction.ExtraTag{
				Tag:       transaction.TxExtraTagNonce,
				VarInt:    sidechain.SideExtraNonceSize,
				HasVarInt: true,
				Data:      make(types.Bytes, sidechain.SideExtraNonceSize),
			},
			transaction.ExtraTag{
				Tag:       transaction.TxExtraTagMergeMining,
				VarInt:    32,
				HasVarInt: true,
				Data:      slices.Clone(types.ZeroHash[:]),
			},
		},
		ExtraBaseRCT: 0,
	}

	tx.Outputs = make(transaction.Outputs, len(shares))

	if final {
		txPrivateKeySlice := s.newTemplateData.TransactionPrivateKey.AsSlice()
		txPrivateKeyScalar := s.newTemplateData.TransactionPrivateKey.AsScalar()

		hasher := crypto.GetKeccak256Hasher()
		defer crypto.PutKeccak256Hasher(hasher)

		var k ephemeralPubKeyCacheKey
		for i := range tx.Outputs {
			outputIndex := uint64(i)
			tx.Outputs[outputIndex].Index = outputIndex
			tx.Outputs[outputIndex].Type = txType
			tx.Outputs[outputIndex].Reward = rewards[outputIndex]
			copy(k[:], shares[outputIndex].Address.Bytes())
			binary.LittleEndian.PutUint64(k[crypto.PublicKeySize*2:], outputIndex)
			if e, ok := s.newTemplateData.Window.EphemeralPubKeyCache[k]; ok {
				tx.Outputs[outputIndex].EphemeralPublicKey, tx.Outputs[outputIndex].ViewTag = e.PublicKey, e.ViewTag
			} else {
				tx.Outputs[outputIndex].EphemeralPublicKey, tx.Outputs[outputIndex].ViewTag = s.sidechain.DerivationCache().GetEphemeralPublicKey(&shares[outputIndex].Address, txPrivateKeySlice, txPrivateKeyScalar, outputIndex, hasher)
			}
		}
	} else {
		for i := range tx.Outputs {
			outputIndex := uint64(i)
			tx.Outputs[outputIndex].Index = outputIndex
			tx.Outputs[outputIndex].Type = txType
			tx.Outputs[outputIndex].Reward = rewards[outputIndex]
		}
	}

	rewardAmountsWeight := uint64(utils.UVarInt64SliceSize(rewards))

	if !final {
		if rewardAmountsWeight != maxRewardsAmountsWeight {
			return nil, fmt.Errorf("incorrect miner rewards during the dry run, %d != %d", rewardAmountsWeight, maxRewardsAmountsWeight)
		}
	} else if rewardAmountsWeight > maxRewardsAmountsWeight {
		return nil, fmt.Errorf("incorrect miner rewards during the dry run, %d > %d", rewardAmountsWeight, maxRewardsAmountsWeight)
	}

	correctedExtraNonceSize := sidechain.SideExtraNonceSize + maxRewardsAmountsWeight - rewardAmountsWeight

	if correctedExtraNonceSize > sidechain.SideExtraNonceSize {
		if correctedExtraNonceSize > sidechain.SideExtraNonceMaxSize {
			return nil, fmt.Errorf("corrected extra_nonce size is too large, %d > %d", correctedExtraNonceSize, sidechain.SideExtraNonceMaxSize)
		}
		//Increase size to maintain transaction weight
		tx.Extra[1].Data = make(types.Bytes, correctedExtraNonceSize)
		tx.Extra[1].VarInt = correctedExtraNonceSize
	}

	return tx, nil
}

func (s *Server) HandleMempoolData(data mempool.Mempool) {
	s.incomingChanges <- func() bool {
		s.lock.Lock()
		defer s.lock.Unlock()

		s.mempool = append(s.mempool, data...)

		// Refresh if 10 seconds have passed between templates and new transactions arrived
		if time.Now().Sub(s.lastMempoolRefresh) >= time.Second*10 {
			s.lastMempoolRefresh = time.Now()
			if err := s.fillNewTemplateData(types.ZeroDifficulty); err != nil {
				log.Printf("[Stratum] Error building new template data: %s", err)
				return false
			}
			return true
		}
		return false
	}
}

func (s *Server) HandleMinerData(minerData *p2pooltypes.MinerData) {
	s.incomingChanges <- func() bool {
		s.lock.Lock()
		defer s.lock.Unlock()

		if s.minerData == nil || s.minerData.Height <= minerData.Height {
			s.minerData = minerData
			s.mempool = minerData.TxBacklog
			s.lastMempoolRefresh = time.Now()
			if err := s.fillNewTemplateData(types.ZeroDifficulty); err != nil {
				log.Printf("[Stratum] Error building new template data: %s", err)
				return false
			}
			return true
		}
		return false
	}
}

func (s *Server) HandleTip(tip *sidechain.PoolBlock) {
	currentDifficulty := s.sidechain.Difficulty()
	s.incomingChanges <- func() bool {
		s.lock.Lock()
		defer s.lock.Unlock()

		if s.tip == nil || s.tip.Side.Height <= tip.Side.Height {
			s.tip = tip
			s.lastMempoolRefresh = time.Now()
			if err := s.fillNewTemplateData(currentDifficulty); err != nil {
				log.Printf("[Stratum] Error building new template data: %s", err)
				return false
			}
			return true
		}
		return false
	}
}

func (s *Server) HandleBroadcast(block *sidechain.PoolBlock) {
	s.incomingChanges <- func() bool {
		s.lock.Lock()
		defer s.lock.Unlock()

		if s.tip != nil && block != s.tip && block.Side.Height <= s.tip.Side.Height {
			//probably a new block was added as alternate
			if err := s.fillNewTemplateData(types.ZeroDifficulty); err != nil {
				log.Printf("[Stratum] Error building new template data: %s", err)
				return false
			}
			return true
		}
		return false
	}
}

func (s *Server) Update() {
	var closeClients []*Client
	defer func() {
		for _, c := range closeClients {
			s.CloseClient(c)
		}
	}()
	s.clientsLock.RLock()
	defer s.clientsLock.RUnlock()

	if len(s.clients) > 0 {
		log.Printf("[Stratum] Sending new job to %d connection(s)", len(s.clients))
		for _, c := range s.clients {
			if err := s.SendTemplate(c); err != nil {
				log.Printf("[Stratum] Closing connection %s: %s", c.Conn.RemoteAddr().String(), err)
				closeClients = append(closeClients, c)
			}
		}
	}
}

type BanEntry struct {
	Expiration uint64
	Error      error
}

func (s *Server) CleanupBanList() {
	s.bansLock.Lock()
	defer s.bansLock.Unlock()

	currentTime := uint64(time.Now().Unix())

	for k, b := range s.bans {
		if currentTime >= b.Expiration {
			delete(s.bans, k)
		}
	}
}

func (s *Server) IsBanned(ip netip.Addr) (bool, *BanEntry) {
	if ip.IsLoopback() {
		return false, nil
	}
	ip = ip.Unmap()
	var prefix netip.Prefix
	if ip.Is6() {
		//ban the /64
		prefix, _ = ip.Prefix(64)
	} else if ip.Is4() {
		//ban only a single ip, /32
		prefix, _ = ip.Prefix(32)
	}

	if !prefix.IsValid() {
		return false, nil
	}

	k := prefix.Addr().As16()

	if b, ok := func() (entry BanEntry, ok bool) {
		s.bansLock.RLock()
		defer s.bansLock.RUnlock()
		entry, ok = s.bans[k]
		return entry, ok
	}(); ok == false {
		return false, nil
	} else if uint64(time.Now().Unix()) >= b.Expiration {
		return false, nil
	} else {
		return true, &b
	}
}

func (s *Server) Ban(ip netip.Addr, duration time.Duration, err error) {
	if ok, _ := s.IsBanned(ip); ok {
		return
	}

	log.Printf("[Stratum] Banned %s for %s: %s", ip.String(), duration.String(), err.Error())
	if !ip.IsLoopback() {
		ip = ip.Unmap()
		var prefix netip.Prefix
		if ip.Is6() {
			//ban the /64
			prefix, _ = ip.Prefix(64)
		} else if ip.Is4() {
			//ban only a single ip, /32
			prefix, _ = ip.Prefix(32)
		}

		if prefix.IsValid() {
			func() {
				s.bansLock.Lock()
				defer s.bansLock.Unlock()
				s.bans[prefix.Addr().As16()] = BanEntry{
					Error:      err,
					Expiration: uint64(time.Now().Unix()) + uint64(duration.Seconds()),
				}
			}()
		}
	}

}

func (s *Server) processIncoming() {
	go func() {
		defer close(s.incomingChanges)

		ctx := s.sidechain.Server().Context()
		for {
			select {
			case <-ctx.Done():
				return
			case f := <-s.incomingChanges:
				if f() {
					s.Update()
				}
			}
		}
	}()
}

func (s *Server) Listen(listen string) error {

	ctx := s.sidechain.Server().Context()
	go func() {
		for range utils.ContextTick(ctx, time.Second*15) {
			s.CleanupMiners()
		}
	}()

	s.processIncoming()

	if listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", listen); err != nil {
		return err
	} else if tcpListener, ok := listener.(*net.TCPListener); !ok {
		return errors.New("not a tcp listener")
	} else {
		defer tcpListener.Close()

		addressNetwork, _ := s.sidechain.Consensus().NetworkType.AddressNetwork()

		for {
			if conn, err := tcpListener.AcceptTCP(); err != nil {
				return err
			} else {
				if err = func() error {
					if addrPort, err := netip.ParseAddrPort(conn.RemoteAddr().String()); err != nil {
						return err
					} else if !addrPort.Addr().IsLoopback() {
						addr := addrPort.Addr().Unmap()

						if ok, b := s.IsBanned(addr); ok {
							return fmt.Errorf("peer is banned: %w", b.Error)
						}
					}

					return nil
				}(); err != nil {
					go func() {
						defer conn.Close()
						log.Printf("[Stratum] Connection from %s rejected (%s)", conn.RemoteAddr().String(), err.Error())
					}()
					continue
				}

				func() {
					log.Printf("[Stratum] Incoming connection from %s", conn.RemoteAddr().String())

					var rpcId uint32
					for rpcId == 0 {
						rpcId = unsafeRandom.Uint32()
					}
					client := &Client{
						RpcId:   rpcId,
						Conn:    conn,
						decoder: utils.NewJSONDecoder(conn),
						Address: address.FromBase58(types.DonationAddress).ToPackedAddress(),
					}
					// Use deadline
					client.encoder = utils.NewJSONEncoder(client)

					func() {
						s.clientsLock.Lock()
						defer s.clientsLock.Unlock()
						s.clients = append(s.clients, client)
					}()
					go func() {
						var err error
						defer s.CloseClient(client)
						defer func() {
							if err != nil {
								log.Printf("[Stratum] Connection %s closed with error: %s", client.Conn.RemoteAddr().String(), err)
							} else {
								log.Printf("[Stratum] Connection %s closed", client.Conn.RemoteAddr().String())
							}
						}()
						defer func() {
							if e := recover(); e != nil {
								err = errors.New("panic called")
								log.Print(e)
								s.CloseClient(client)
							}
						}()

						for client.decoder.More() {
							var msg JsonRpcMessage
							if err = client.decoder.Decode(&msg); err != nil {
								return
							}

							switch msg.Method {
							case "login":

								if err = func() error {
									client.Lock.Lock()
									defer client.Lock.Unlock()
									if client.Login {
										return errors.New("already logged in")
									}
									if m, ok := msg.Params.(map[string]any); ok {
										if str, ok := m["agent"].(string); ok {
											client.Agent = str
										}
										if str, ok := m["pass"].(string); ok {
											client.Password = str
										}
										if str, ok := m["rig-id"].(string); ok {
											client.RigId = str
										}
										if str, ok := m["login"].(string); ok {
											a := address.FromBase58(str)
											if a != nil && a.Network == addressNetwork {
												client.Address = a.ToPackedAddress()
											} else {
												return errors.New("invalid address in user")
											}
										}
										var hasRx0 bool
										if algos, ok := m["algo"].([]any); ok {
											for _, v := range algos {
												if str, ok := v.(string); !ok {
													return errors.New("invalid algo")
												} else if str == "rx/0" {
													hasRx0 = true
													break
												}
											}
										}

										if !hasRx0 {
											return errors.New("algo rx/0 not found")
										}

										log.Printf("[Stratum] Connection %s address = %s, agent = \"%s\", pass = \"%s\"", client.Conn.RemoteAddr().String(), client.Address.ToAddress(addressNetwork).ToBase58(), client.Agent, client.Password)

										client.Login = true
										return nil
									} else {
										return errors.New("could not read login params")
									}
								}(); err != nil {
									_ = client.encoder.Encode(JsonRpcResult{
										Id:             msg.Id,
										JsonRpcVersion: "2.0",
										Error: map[string]any{
											"code":    int(-1),
											"message": err.Error(),
										},
									})
									return
								} else if err = s.SendTemplateResponse(client, msg.Id); err != nil {
									_ = client.encoder.Encode(JsonRpcResult{
										Id:             msg.Id,
										JsonRpcVersion: "2.0",
										Error: map[string]any{
											"code":    int(-1),
											"message": err.Error(),
										},
									})
								}

							case "submit":
								if submitError, ban := func() (error, bool) {
									client.Lock.RLock()
									defer client.Lock.RUnlock()
									if !client.Login {
										return errors.New("unauthenticated"), true
									}
									var err error
									var jobId JobIdentifier
									var resultHash types.Hash
									var nonce uint32
									if m, ok := msg.Params.(map[string]any); ok {
										if str, ok := m["job_id"].(string); ok {
											if jobId, err = JobIdentifierFromString(str); err != nil {
												return err, true
											}
										} else {
											return errors.New("no job_id specified"), true
										}
										if str, ok := m["nonce"].(string); ok {
											var nonceBuf []byte
											if nonceBuf, err = hex.DecodeString(str); err != nil {
												return err, true
											}
											if len(nonceBuf) != 4 {
												return errors.New("invalid nonce size"), true
											}
											nonce = binary.LittleEndian.Uint32(nonceBuf)
										} else {
											return errors.New("no nonce specified"), true
										}
										if str, ok := m["result"].(string); ok {
											if resultHash, err = types.HashFromString(str); err != nil {
												return err, true
											}
										} else {
											return errors.New("no result specified"), true
										}

										if err, ban := func() (error, bool) {
											if e, ok := func() (*MinerTrackingEntry, bool) {
												s.minersLock.RLock()
												defer s.minersLock.RUnlock()
												e, ok := s.miners[client.Address]
												return e, ok
											}(); ok {
												b := &sidechain.PoolBlock{}
												if blob := e.GetJobBlob(jobId, nonce); blob == nil {
													return errors.New("invalid job id"), true
												} else if err := b.UnmarshalBinary(s.sidechain.Consensus(), s.sidechain.DerivationCache(), blob); err != nil {
													return err, true
												} else {
													if b.Side.Difficulty.CheckPoW(resultHash) {
														//passes difficulty
														if err := s.SubmitFunc(b); err != nil {
															return fmt.Errorf("submit error: %w", err), true
														}
													} else {

														return errors.New("low difficulty share"), true
													}
												}
											} else {
												return errors.New("unknown miner"), true
											}
											return nil, false
										}(); err != nil {
											return err, ban
										}
										return nil, false
									} else {
										return errors.New("could not read submit params"), true
									}
								}(); submitError != nil {
									err = client.encoder.Encode(JsonRpcResult{
										Id:             msg.Id,
										JsonRpcVersion: "2.0",
										Error: map[string]any{
											"code":    int(-1),
											"message": submitError.Error(),
										},
									})
									if err != nil || ban {
										return
									}
								} else {
									if err = client.encoder.Encode(JsonRpcResult{
										Id:             msg.Id,
										JsonRpcVersion: "2.0",
										Error:          nil,
										Result: map[string]any{
											"status": "OK",
										},
									}); err != nil {
										return
									}
								}
							case "keepalived":
								if err = client.encoder.Encode(JsonRpcResult{
									Id:             msg.Id,
									JsonRpcVersion: "2.0",
									Error:          nil,
									Result: map[string]any{
										"status": "KEEPALIVED",
									},
								}); err != nil {
									return
								}
							default:
								err = fmt.Errorf("unknown command %s", msg.Method)
								_ = client.encoder.Encode(JsonRpcResult{
									Id:             msg.Id,
									JsonRpcVersion: "2.0",
									Error: map[string]any{
										"code":    int(-1),
										"message": err.Error(),
									},
								})
								return
							}
						}
					}()
				}()
			}

		}
	}

	return nil
}

func (s *Server) SendTemplate(c *Client) (err error) {
	c.Lock.Lock()
	defer c.Lock.Unlock()
	tpl, jobCounter, targetDifficulty, seedHash, err := s.BuildTemplate(c.Address, false)

	if err != nil {
		return err
	}

	job := copyBaseJob()
	bufLen := tpl.HashingBlobBufferLength()
	if cap(c.buf) < bufLen {
		c.buf = make([]byte, 0, bufLen)
	}

	sideRandomNumber := unsafeRandom.Uint32()
	extraNonce := unsafeRandom.Uint32()
	sideExtraNonce := extraNonce

	hasher := crypto.GetKeccak256Hasher()
	defer crypto.PutKeccak256Hasher(hasher)

	var templateId types.Hash
	tpl.TemplateId(hasher, c.buf, s.sidechain.Consensus(), sideRandomNumber, sideExtraNonce, &templateId)

	job.Params.Blob = hex.EncodeToString(tpl.HashingBlob(hasher, c.buf, 0, extraNonce, templateId))

	jobId := JobIdentifierFromValues(jobCounter, extraNonce, sideRandomNumber, sideExtraNonce, templateId)

	job.Params.JobId = jobId.String()

	target := targetDifficulty.Target()
	job.Params.Target = TargetHex(target)
	job.Params.Height = tpl.MainHeight
	job.Params.SeedHash = seedHash

	if err = c.encoder.EncodeWithOption(job, utils.JsonEncodeOptions...); err != nil {
		return
	}
	return nil
}

func (s *Server) SendTemplateResponse(c *Client, id any) (err error) {
	c.Lock.Lock()
	defer c.Lock.Unlock()
	tpl, jobCounter, targetDifficulty, seedHash, err := s.BuildTemplate(c.Address, false)

	if err != nil {
		return
	}

	job := copyBaseResponseJob()
	bufLen := tpl.HashingBlobBufferLength()
	if cap(c.buf) < bufLen {
		c.buf = make([]byte, 0, bufLen)
	}
	var hexBuf [4]byte
	binary.LittleEndian.PutUint32(hexBuf[:], c.RpcId)

	sideRandomNumber := unsafeRandom.Uint32()
	extraNonce := unsafeRandom.Uint32()
	sideExtraNonce := extraNonce

	hasher := crypto.GetKeccak256Hasher()
	defer crypto.PutKeccak256Hasher(hasher)

	var templateId types.Hash
	tpl.TemplateId(hasher, c.buf, s.sidechain.Consensus(), sideRandomNumber, sideExtraNonce, &templateId)

	job.Id = id
	job.Result.Id = hex.EncodeToString(hexBuf[:])
	job.Result.Job.Blob = hex.EncodeToString(tpl.HashingBlob(hasher, c.buf, 0, extraNonce, templateId))

	jobId := JobIdentifierFromValues(jobCounter, extraNonce, sideRandomNumber, sideExtraNonce, templateId)

	job.Result.Job.JobId = jobId.String()

	target := targetDifficulty.Target()
	job.Result.Job.Target = TargetHex(target)
	job.Result.Job.Height = tpl.MainHeight
	job.Result.Job.SeedHash = seedHash

	if err = c.encoder.EncodeWithOption(job, utils.JsonEncodeOptions...); err != nil {
		return
	}
	return nil
}

func (s *Server) CloseClient(c *Client) {
	c.Conn.Close()

	s.clientsLock.Lock()
	defer s.clientsLock.Unlock()
	if i := slices.Index(s.clients, c); i != -1 {
		s.clients = slices.Delete(s.clients, i, i+1)
	}
}

// Target4BytesLimit Use short target format (4 bytes) for diff <= 4 million
const Target4BytesLimit = math.MaxUint64 / 4000001

func TargetHex(target uint64) string {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], target)
	result := hex.EncodeToString(buf[:])
	if target >= Target4BytesLimit {
		return result[4*2:]
	} else {
		return result
	}
}
