package sidechain

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	mainblock "git.gammaspectra.live/P2Pool/p2pool-observer/monero/block"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/randomx"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/transaction"
	p2poolcrypto "git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"io"
	"slices"
	"sync/atomic"
	"unsafe"
)

type CoinbaseExtraTag int

const SideExtraNonceSize = 4
const SideExtraNonceMaxSize = SideExtraNonceSize + 10

const (
	SideCoinbasePublicKey = transaction.TxExtraTagPubKey
	SideExtraNonce        = transaction.TxExtraTagNonce
	SideTemplateId        = transaction.TxExtraTagMergeMining
)

// PoolBlockMaxTemplateSize Max P2P message size (128 KB) minus BLOCK_RESPONSE header (5 bytes)
const PoolBlockMaxTemplateSize = 128*1024 - (1 + 4)

type ShareVersion int

func (v ShareVersion) String() string {
	switch v {
	case ShareVersion_None:
		return "none"
	default:
		return fmt.Sprintf("v%d", v)
	}
}

const (
	ShareVersion_None ShareVersion = 0
	ShareVersion_V1   ShareVersion = 1
	ShareVersion_V2   ShareVersion = 2
)

type UniquePoolBlockSlice []*PoolBlock

func (s UniquePoolBlockSlice) Get(id types.Hash) *PoolBlock {
	if i := slices.IndexFunc(s, func(p *PoolBlock) bool {
		return bytes.Compare(p.CoinbaseExtra(SideTemplateId), id[:]) == 0
	}); i != -1 {
		return s[i]
	}
	return nil
}

func (s UniquePoolBlockSlice) GetHeight(height uint64) (result UniquePoolBlockSlice) {
	for _, b := range s {
		if b.Side.Height == height {
			result = append(result, b)
		}
	}
	return result
}

type PoolBlock struct {
	Main mainblock.Block `json:"main"`

	Side SideData `json:"side"`

	//Temporary data structures
	cache    poolBlockCache
	Depth    atomic.Uint64 `json:"-"`
	Verified atomic.Bool   `json:"-"`
	Invalid  atomic.Bool   `json:"-"`

	WantBroadcast atomic.Bool `json:"-"`
	Broadcasted   atomic.Bool `json:"-"`

	LocalTimestamp     uint64       `json:"-"`
	CachedShareVersion ShareVersion `json:"share_version"`
}

// NewShareFromExportedBytes
// Deprecated
func NewShareFromExportedBytes(buf []byte, consensus *Consensus, cacheInterface DerivationCacheInterface) (*PoolBlock, error) {
	b := &PoolBlock{}

	if len(buf) < 32 {
		return nil, errors.New("invalid block data")
	}

	reader := bytes.NewReader(buf)

	var (
		err     error
		version uint64

		mainDataSize uint64
		mainData     []byte

		sideDataSize uint64
		sideData     []byte
	)

	if err = binary.Read(reader, binary.BigEndian, &version); err != nil {
		return nil, err
	}

	switch version {
	case 1:

		var mainId types.Hash

		if _, err = io.ReadFull(reader, mainId[:]); err != nil {
			return nil, err
		}

		var h types.Hash
		// Read PoW hash
		if _, err = io.ReadFull(reader, h[:]); err != nil {
			return nil, err
		}

		var mainDifficulty types.Difficulty

		if err = binary.Read(reader, binary.BigEndian, &mainDifficulty.Hi); err != nil {
			return nil, err
		}
		if err = binary.Read(reader, binary.BigEndian, &mainDifficulty.Lo); err != nil {
			return nil, err
		}

		mainDifficulty.ReverseBytes()

		_ = mainDifficulty

		if err = binary.Read(reader, binary.BigEndian, &mainDataSize); err != nil {
			return nil, err
		}
		mainData = make([]byte, mainDataSize)
		if _, err = io.ReadFull(reader, mainData); err != nil {
			return nil, err
		}

		if err = binary.Read(reader, binary.BigEndian, &sideDataSize); err != nil {
			return nil, err
		}
		sideData = make([]byte, sideDataSize)
		if _, err = io.ReadFull(reader, sideData); err != nil {
			return nil, err
		}

		/*
			//Ignore error when unable to read peer
			_ = func() error {
				var peerSize uint64

				if err = binary.Read(reader, binary.BigEndian, &peerSize); err != nil {
					return err
				}
				b.Extra.Peer = make([]byte, peerSize)
				if _, err = io.ReadFull(reader, b.Extra.Peer); err != nil {
					return err
				}

				return nil
			}()
		*/

	case 0:
		if err = binary.Read(reader, binary.BigEndian, &mainDataSize); err != nil {
			return nil, err
		}
		mainData = make([]byte, mainDataSize)
		if _, err = io.ReadFull(reader, mainData); err != nil {
			return nil, err
		}
		if sideData, err = io.ReadAll(reader); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown block version %d", version)
	}

	if err = b.Main.UnmarshalBinary(mainData); err != nil {
		return nil, err
	}

	if expectedMajorVersion := NetworkMajorVersion(consensus, b.Main.Coinbase.GenHeight); expectedMajorVersion != b.Main.MajorVersion {
		return nil, fmt.Errorf("expected major version %d at height %d, got %d", expectedMajorVersion, b.Main.Coinbase.GenHeight, b.Main.MajorVersion)
	}

	b.CachedShareVersion = b.CalculateShareVersion(consensus)

	//TODO: this is to comply with non-standard p2pool serialization, see https://github.com/SChernykh/p2pool/issues/249
	if t := b.Main.Coinbase.Extra.GetTag(transaction.TxExtraTagMergeMining); t == nil && t.VarInt != 32 {
		return nil, errors.New("wrong merge mining tag depth")
	}

	if err = b.Side.UnmarshalBinary(sideData, b.ShareVersion()); err != nil {
		return nil, err
	}

	b.FillPrivateKeys(cacheInterface)

	//zero cache as it can be wrong
	b.cache.templateId.Store(nil)
	b.cache.powHash.Store(nil)

	return b, nil
}

func (b *PoolBlock) NeedsCompactTransactionFilling() bool {
	return len(b.Main.TransactionParentIndices) > 0 && len(b.Main.TransactionParentIndices) == len(b.Main.Transactions) && slices.Index(b.Main.Transactions, types.ZeroHash) != -1
}

func (b *PoolBlock) FillTransactionsFromTransactionParentIndices(parent *PoolBlock) error {
	if b.NeedsCompactTransactionFilling() {
		if parent != nil && types.HashFromBytes(parent.CoinbaseExtra(SideTemplateId)) == b.Side.Parent {
			for i, parentIndex := range b.Main.TransactionParentIndices {
				if parentIndex != 0 {
					// p2pool stores coinbase transaction hash as well, decrease
					actualIndex := parentIndex - 1
					if actualIndex > uint64(len(parent.Main.Transactions)) {
						return errors.New("index of parent transaction out of bounds")
					}
					b.Main.Transactions[i] = parent.Main.Transactions[actualIndex]
				}
			}
		} else if parent == nil {
			return errors.New("parent is nil")
		}
	}

	return nil
}

func (b *PoolBlock) FillTransactionParentIndices(parent *PoolBlock) bool {
	if len(b.Main.Transactions) != len(b.Main.TransactionParentIndices) {
		if parent != nil && types.HashFromBytes(parent.CoinbaseExtra(SideTemplateId)) == b.Side.Parent {
			b.Main.TransactionParentIndices = make([]uint64, len(b.Main.Transactions))
			//do not fail if not found
			for i, txHash := range b.Main.Transactions {
				if parentIndex := slices.Index(parent.Main.Transactions, txHash); parentIndex != -1 {
					//increase as p2pool stores tx hash as well
					b.Main.TransactionParentIndices[i] = uint64(parentIndex + 1)
				}
			}
			return true
		}

		return false
	}

	return true
}

func (b *PoolBlock) CalculateShareVersion(consensus *Consensus) ShareVersion {
	// P2Pool forks to v2 at 2023-03-18 21:00 UTC
	// Different miners can have different timestamps,
	// so a temporary mix of v1 and v2 blocks is allowed
	return P2PoolShareVersion(consensus, b.Main.Timestamp)
}

func (b *PoolBlock) ShareVersion() ShareVersion {
	return b.CachedShareVersion
}

func (b *PoolBlock) ShareVersionSignaling() ShareVersion {
	if b.ShareVersion() == ShareVersion_V1 && (b.ExtraNonce()&0xFF000000 == 0xFF000000) {
		return ShareVersion_V2
	}
	return ShareVersion_None
}

func (b *PoolBlock) ExtraNonce() uint32 {
	extraNonce := b.CoinbaseExtra(SideExtraNonce)
	if len(extraNonce) < SideExtraNonceSize {
		return 0
	}
	return binary.LittleEndian.Uint32(extraNonce)
}

func (b *PoolBlock) CoinbaseExtra(tag CoinbaseExtraTag) []byte {
	switch tag {
	case SideExtraNonce:
		if t := b.Main.Coinbase.Extra.GetTag(uint8(tag)); t != nil {
			if len(t.Data) < SideExtraNonceSize || len(t.Data) > SideExtraNonceMaxSize {
				return nil
			}
			return t.Data
		}
	case SideTemplateId:
		if t := b.Main.Coinbase.Extra.GetTag(uint8(tag)); t != nil {
			if len(t.Data) != types.HashSize {
				return nil
			}
			return t.Data
		}
	case SideCoinbasePublicKey:
		if t := b.Main.Coinbase.Extra.GetTag(uint8(tag)); t != nil {
			if len(t.Data) != crypto.PublicKeySize {
				return nil
			}
			return t.Data
		}
	}

	return nil
}

func (b *PoolBlock) MainId() types.Hash {
	return b.Main.Id()
}

func (b *PoolBlock) FullId() FullId {
	var buf FullId
	sidechainId := b.CoinbaseExtra(SideTemplateId)
	copy(buf[:], sidechainId[:])
	binary.LittleEndian.PutUint32(buf[types.HashSize:], b.Main.Nonce)
	copy(buf[types.HashSize+unsafe.Sizeof(b.Main.Nonce):], b.CoinbaseExtra(SideExtraNonce)[:SideExtraNonceSize])
	return buf
}

const FullIdSize = int(types.HashSize + unsafe.Sizeof(uint32(0)) + SideExtraNonceSize)

var zeroFullId FullId

type FullId [FullIdSize]byte

func FullIdFromString(s string) (FullId, error) {
	var h FullId
	if buf, err := hex.DecodeString(s); err != nil {
		return h, err
	} else {
		if len(buf) != FullIdSize {
			return h, errors.New("wrong hash size")
		}
		copy(h[:], buf)
		return h, nil
	}
}

func (id FullId) TemplateId() (h types.Hash) {
	return types.Hash(id[:types.HashSize])
}

func (id FullId) Nonce() uint32 {
	return binary.LittleEndian.Uint32(id[types.HashSize:])
}

func (id FullId) ExtraNonce() uint32 {
	return binary.LittleEndian.Uint32(id[types.HashSize+unsafe.Sizeof(uint32(0)):])
}

func (id FullId) String() string {
	return hex.EncodeToString(id[:])
}

func (b *PoolBlock) CalculateFullId(consensus *Consensus) FullId {
	var buf FullId
	sidechainId := b.SideTemplateId(consensus)
	copy(buf[:], sidechainId[:])
	binary.LittleEndian.PutUint32(buf[types.HashSize:], b.Main.Nonce)
	copy(buf[types.HashSize+unsafe.Sizeof(b.Main.Nonce):], b.CoinbaseExtra(SideExtraNonce)[:SideExtraNonceSize])
	return buf
}

func (b *PoolBlock) MainDifficulty(f mainblock.GetDifficultyByHeightFunc) types.Difficulty {
	return b.Main.Difficulty(f)
}

func (b *PoolBlock) SideTemplateId(consensus *Consensus) types.Hash {
	if h := b.cache.templateId.Load(); h != nil {
		return *h
	} else {
		hash := consensus.CalculateSideTemplateId(b)
		if hash == types.ZeroHash {
			return types.ZeroHash
		}
		b.cache.templateId.Store(&hash)
		return hash
	}
}

func (b *PoolBlock) PowHash(hasher randomx.Hasher, f mainblock.GetSeedByHeightFunc) types.Hash {
	h, _ := b.PowHashWithError(hasher, f)
	return h
}

func (b *PoolBlock) PowHashWithError(hasher randomx.Hasher, f mainblock.GetSeedByHeightFunc) (powHash types.Hash, err error) {
	if h := b.cache.powHash.Load(); h != nil {
		powHash = *h
	} else {
		powHash, err = b.Main.PowHashWithError(hasher, f)
		if powHash == types.ZeroHash {
			return types.ZeroHash, err
		}
		b.cache.powHash.Store(&powHash)
	}

	return powHash, nil
}

func (b *PoolBlock) UnmarshalBinary(consensus *Consensus, derivationCache DerivationCacheInterface, data []byte) error {
	if len(data) > PoolBlockMaxTemplateSize {
		return errors.New("buffer too large")
	}
	reader := bytes.NewReader(data)
	return b.FromReader(consensus, derivationCache, reader)
}

func (b *PoolBlock) BufferLength() int {
	return b.Main.BufferLength() + b.Side.BufferLength()
}

func (b *PoolBlock) MarshalBinary() ([]byte, error) {
	return b.MarshalBinaryFlags(false, false)
}

func (b *PoolBlock) MarshalBinaryFlags(pruned, compact bool) ([]byte, error) {
	return b.AppendBinaryFlags(make([]byte, 0, b.BufferLength()), pruned, compact)
}

func (b *PoolBlock) AppendBinaryFlags(preAllocatedBuf []byte, pruned, compact bool) (buf []byte, err error) {
	buf = preAllocatedBuf

	if buf, err = b.Main.AppendBinaryFlags(buf, pruned, compact); err != nil {
		return nil, err
	} else if buf, err = b.Side.AppendBinary(buf, b.ShareVersion()); err != nil {
		return nil, err
	} else {
		if len(buf) > PoolBlockMaxTemplateSize {
			return nil, errors.New("buffer too large")
		}
		return buf, nil
	}
}

func (b *PoolBlock) FromReader(consensus *Consensus, derivationCache DerivationCacheInterface, reader utils.ReaderAndByteReader) (err error) {
	if err = b.Main.FromReader(reader); err != nil {
		return err
	}

	if expectedMajorVersion := NetworkMajorVersion(consensus, b.Main.Coinbase.GenHeight); expectedMajorVersion != b.Main.MajorVersion {
		return fmt.Errorf("expected major version %d at height %d, got %d", expectedMajorVersion, b.Main.Coinbase.GenHeight, b.Main.MajorVersion)
	}

	//TODO: this is to comply with non-standard p2pool serialization, see https://github.com/SChernykh/p2pool/issues/249
	if t := b.Main.Coinbase.Extra.GetTag(transaction.TxExtraTagMergeMining); t == nil && t.VarInt != 32 {
		return errors.New("wrong merge mining tag depth")
	}

	b.CachedShareVersion = b.CalculateShareVersion(consensus)

	if err = b.Side.FromReader(reader, b.ShareVersion()); err != nil {
		return err
	}

	b.FillPrivateKeys(derivationCache)

	return nil
}

// FromCompactReader used in Protocol 1.1 and above
func (b *PoolBlock) FromCompactReader(consensus *Consensus, derivationCache DerivationCacheInterface, reader utils.ReaderAndByteReader) (err error) {
	if err = b.Main.FromCompactReader(reader); err != nil {
		return err
	}

	if expectedMajorVersion := NetworkMajorVersion(consensus, b.Main.Coinbase.GenHeight); expectedMajorVersion != b.Main.MajorVersion {
		return fmt.Errorf("expected major version %d at height %d, got %d", expectedMajorVersion, b.Main.Coinbase.GenHeight, b.Main.MajorVersion)
	}

	//TODO: this is to comply with non-standard p2pool serialization, see https://github.com/SChernykh/p2pool/issues/249
	if t := b.Main.Coinbase.Extra.GetTag(transaction.TxExtraTagMergeMining); t == nil && t.VarInt != 32 {
		return errors.New("wrong merge mining tag depth")
	}

	b.CachedShareVersion = b.CalculateShareVersion(consensus)

	if err = b.Side.FromReader(reader, b.ShareVersion()); err != nil {
		return err
	}

	b.FillPrivateKeys(derivationCache)

	return nil
}

// PreProcessBlock processes and fills the block data from either pruned or compact modes
func (b *PoolBlock) PreProcessBlock(consensus *Consensus, derivationCache DerivationCacheInterface, preAllocatedShares Shares, difficultyByHeight mainblock.GetDifficultyByHeightFunc, getTemplateById GetByTemplateIdFunc) (missingBlocks []types.Hash, err error) {

	getTemplateByIdFillingTx := func(h types.Hash) *PoolBlock {
		chain := make(UniquePoolBlockSlice, 0, 1)

		cur := getTemplateById(h)
		for ; cur != nil; cur = getTemplateById(cur.Side.Parent) {
			chain = append(chain, cur)
			if !cur.NeedsCompactTransactionFilling() {
				break
			}
			if len(chain) > 1 {
				if chain[len(chain)-2].FillTransactionsFromTransactionParentIndices(chain[len(chain)-1]) == nil {
					if !chain[len(chain)-2].NeedsCompactTransactionFilling() {
						//early abort if it can all be filled
						chain = chain[:len(chain)-1]
						break
					}
				}
			}
		}
		if len(chain) == 0 {
			return nil
		}
		//skips last entry
		for i := len(chain) - 2; i >= 0; i-- {
			if err := chain[i].FillTransactionsFromTransactionParentIndices(chain[i+1]); err != nil {
				return nil
			}
		}
		return chain[0]
	}

	var parent *PoolBlock
	if b.NeedsCompactTransactionFilling() {
		parent = getTemplateByIdFillingTx(b.Side.Parent)
		if parent == nil {
			missingBlocks = append(missingBlocks, b.Side.Parent)
			return missingBlocks, errors.New("parent does not exist in compact block")
		}
		if err := b.FillTransactionsFromTransactionParentIndices(parent); err != nil {
			return nil, fmt.Errorf("error filling transactions for block: %w", err)
		}
	}

	if len(b.Main.Transactions) != len(b.Main.TransactionParentIndices) {
		if parent == nil {
			parent = getTemplateByIdFillingTx(b.Side.Parent)
		}
		b.FillTransactionParentIndices(parent)
	}

	if len(b.Main.Coinbase.Outputs) == 0 {
		if outputs, _ := CalculateOutputs(b, consensus, difficultyByHeight, getTemplateById, derivationCache, preAllocatedShares); outputs == nil {
			return nil, errors.New("error filling outputs for block: nil outputs")
		} else {
			b.Main.Coinbase.Outputs = outputs
		}

		if outputBlob, err := b.Main.Coinbase.Outputs.AppendBinary(make([]byte, 0, b.Main.Coinbase.Outputs.BufferLength())); err != nil {
			return nil, fmt.Errorf("error filling outputs for block: %s", err)
		} else if uint64(len(outputBlob)) != b.Main.Coinbase.OutputsBlobSize {
			return nil, fmt.Errorf("error filling outputs for block: invalid output blob size, got %d, expected %d", b.Main.Coinbase.OutputsBlobSize, len(outputBlob))
		}
	}

	return nil, nil
}

func (b *PoolBlock) NeedsPreProcess() bool {
	return b.NeedsCompactTransactionFilling() || len(b.Main.Transactions) != len(b.Main.TransactionParentIndices) || len(b.Main.Coinbase.Outputs) == 0
}

func (b *PoolBlock) FillPrivateKeys(derivationCache DerivationCacheInterface) {
	if b.ShareVersion() > ShareVersion_V1 {
		if bytes.Compare(b.Side.CoinbasePrivateKey.AsSlice(), types.ZeroHash[:]) == 0 {
			//Fill Private Key
			kP := derivationCache.GetDeterministicTransactionKey(b.GetPrivateKeySeed(), b.Main.PreviousId)
			b.Side.CoinbasePrivateKey = kP.PrivateKey.AsBytes()
		}
	} else {
		b.Side.CoinbasePrivateKeySeed = b.GetPrivateKeySeed()
	}
}

func (b *PoolBlock) IsProofHigherThanMainDifficulty(hasher randomx.Hasher, difficultyFunc mainblock.GetDifficultyByHeightFunc, seedFunc mainblock.GetSeedByHeightFunc) bool {
	r, _ := b.IsProofHigherThanMainDifficultyWithError(hasher, difficultyFunc, seedFunc)
	return r
}

func (b *PoolBlock) IsProofHigherThanMainDifficultyWithError(hasher randomx.Hasher, difficultyFunc mainblock.GetDifficultyByHeightFunc, seedFunc mainblock.GetSeedByHeightFunc) (bool, error) {
	if mainDifficulty := b.MainDifficulty(difficultyFunc); mainDifficulty == types.ZeroDifficulty {
		return false, errors.New("could not get main difficulty")
	} else if powHash, err := b.PowHashWithError(hasher, seedFunc); err != nil {
		return false, err
	} else {
		return mainDifficulty.CheckPoW(powHash), nil
	}
}

func (b *PoolBlock) IsProofHigherThanDifficulty(hasher randomx.Hasher, f mainblock.GetSeedByHeightFunc) bool {
	r, _ := b.IsProofHigherThanDifficultyWithError(hasher, f)
	return r
}

func (b *PoolBlock) IsProofHigherThanDifficultyWithError(hasher randomx.Hasher, f mainblock.GetSeedByHeightFunc) (bool, error) {
	if powHash, err := b.PowHashWithError(hasher, f); err != nil {
		return false, err
	} else {
		return b.Side.Difficulty.CheckPoW(powHash), nil
	}
}

func (b *PoolBlock) GetPrivateKeySeed() types.Hash {
	if b.ShareVersion() > ShareVersion_V1 {
		return b.Side.CoinbasePrivateKeySeed
	}

	oldSeed := types.Hash(b.Side.PublicSpendKey.AsBytes())
	if b.Main.MajorVersion < monero.HardForkViewTagsVersion && p2poolcrypto.GetDeterministicTransactionPrivateKey(oldSeed, b.Main.PreviousId).AsBytes() != b.Side.CoinbasePrivateKey {
		return types.ZeroHash
	}

	return oldSeed
}

func (b *PoolBlock) CalculateTransactionPrivateKeySeed() types.Hash {
	if b.ShareVersion() > ShareVersion_V1 {
		preAllocatedMainData := make([]byte, 0, b.Main.BufferLength())
		preAllocatedSideData := make([]byte, 0, b.Side.BufferLength())
		mainData, _ := b.Main.SideChainHashingBlob(preAllocatedMainData, false)
		sideData, _ := b.Side.AppendBinary(preAllocatedSideData, b.ShareVersion())
		return p2poolcrypto.CalculateTransactionPrivateKeySeed(
			mainData,
			sideData,
		)
	}

	return types.Hash(b.Side.PublicSpendKey.AsBytes())
}

func (b *PoolBlock) GetAddress() address.PackedAddress {
	return address.NewPackedAddressFromBytes(b.Side.PublicSpendKey, b.Side.PublicViewKey)
}

func (b *PoolBlock) GetTransactionOutputType() uint8 {
	// Both tx types are allowed by Monero consensus during v15 because it needs to process pre-fork mempool transactions,
	// but P2Pool can switch to using only TXOUT_TO_TAGGED_KEY for miner payouts starting from v15
	expectedTxType := uint8(transaction.TxOutToKey)
	if b.Main.MajorVersion >= monero.HardForkViewTagsVersion {
		expectedTxType = transaction.TxOutToTaggedKey
	}

	return expectedTxType
}

type poolBlockCache struct {
	templateId atomic.Pointer[types.Hash]
	powHash    atomic.Pointer[types.Hash]
}
