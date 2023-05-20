package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"git.gammaspectra.live/P2Pool/go-monero/pkg/rpc/daemon"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/block"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/randomx"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/transaction"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/cache/archive"
	crypto2 "git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/mempool"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"git.gammaspectra.live/P2Pool/sha3"
	"github.com/floatdrop/lru"
	"golang.org/x/exp/slices"
	"log"
	"math"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
)

func main() {
	inputConsensus := flag.String("consensus", "config.json", "Input config.json consensus file")
	inputArchive := flag.String("input", "", "Input path for archive database")
	moneroHost := flag.String("host", "127.0.0.1", "IP address of your Monero node")
	moneroRpcPort := flag.Uint("rpc-port", 18081, "monerod RPC API port number")
	inputTemplate := flag.String("template", "", "Template data for share recovery")

	flag.Parse()

	client.SetDefaultClientSettings(fmt.Sprintf("http://%s:%d", *moneroHost, *moneroRpcPort))

	cf, err := os.ReadFile(*inputConsensus)
	if err != nil {
		log.Panic(err)
	}
	inputTemplateData, err := os.ReadFile(*inputTemplate)
	if err != nil {
		log.Panic(err)
	}

	jsonTemplate, err := JSONFromTemplate(inputTemplateData)
	if err != nil {
		log.Panic(err)
	}

	consensus, err := sidechain.NewConsensusFromJSON(cf)
	if err != nil {
		log.Panic(err)
	}

	var headerCacheLock sync.RWMutex
	headerByHeightCache := make(map[uint64]*daemon.BlockHeader)

	getHeaderByHeight := func(height uint64) *daemon.BlockHeader {
		if v := func() *daemon.BlockHeader {
			headerCacheLock.RLock()
			defer headerCacheLock.RUnlock()
			return headerByHeightCache[height]
		}(); v == nil {
			if r, err := client.GetDefaultClient().GetBlockHeaderByHeight(height, context.Background()); err == nil {
				headerCacheLock.Lock()
				defer headerCacheLock.Unlock()
				headerByHeightCache[r.BlockHeader.Height] = &r.BlockHeader
				return &r.BlockHeader
			}
			return nil
		} else {
			return v
		}
	}

	getDifficultyByHeight := func(height uint64) types.Difficulty {
		if v := getHeaderByHeight(height); v != nil {
			return types.DifficultyFrom64(v.Difficulty)
		}
		return types.ZeroDifficulty
	}

	getSeedByHeight := func(height uint64) (hash types.Hash) {
		seedHeight := randomx.SeedHeight(height)
		if v := getHeaderByHeight(seedHeight); v != nil {
			h, _ := types.HashFromString(v.Hash)
			return h
		}
		return types.ZeroHash
	}

	_ = getSeedByHeight

	archiveCache, err := archive.NewCache(*inputArchive, consensus, getDifficultyByHeight)
	if err != nil {
		log.Panic(err)
	}
	defer archiveCache.Close()

	blockCache := lru.New[types.Hash, *sidechain.PoolBlock](int(consensus.ChainWindowSize * 4))

	derivationCache := sidechain.NewDerivationLRUCache()
	getByTemplateIdDirect := func(h types.Hash) *sidechain.PoolBlock {
		if v := blockCache.Get(h); v == nil {
			if bs := archiveCache.LoadByTemplateId(h); len(bs) != 0 {
				blockCache.Set(h, bs[0])
				return bs[0]
			} else {
				return nil
			}
		} else {
			return *v
		}
	}

	processBlock := func(b *sidechain.PoolBlock) error {
		var preAllocatedShares sidechain.Shares
		if len(b.Main.Coinbase.Outputs) == 0 {
			//cannot use SideTemplateId() as it might not be proper to calculate yet. fetch from coinbase only here
			if b2 := getByTemplateIdDirect(types.HashFromBytes(b.CoinbaseExtra(sidechain.SideTemplateId))); b2 != nil && len(b2.Main.Coinbase.Outputs) != 0 {
				b.Main.Coinbase.Outputs = b2.Main.Coinbase.Outputs
			} else {
				preAllocatedShares = sidechain.PreAllocateShares(consensus.ChainWindowSize * 2)
			}
		}

		_, err := b.PreProcessBlock(consensus, derivationCache, preAllocatedShares, getDifficultyByHeight, getByTemplateIdDirect)
		return err
	}

	getByTemplateId := func(h types.Hash) *sidechain.PoolBlock {
		if v := getByTemplateIdDirect(h); v != nil {
			if processBlock(v) != nil {
				return nil
			}
			return v
		} else {
			return nil
		}
	}

	var poolBlock *sidechain.PoolBlock
	var expectedMainId types.Hash
	var expectedTemplateId types.Hash
	var expectedCoinbaseId types.Hash
	var baseMainReward, deltaReward uint64

	getTransactionsEntries := func(h ...types.Hash) (r mempool.Mempool) {
		if data, jsonTx, err := client.GetDefaultClient().GetTransactions(h...); err != nil {
			log.Printf("could not get txids: %s", err)
			return nil
		} else {
			r = make(mempool.Mempool, len(jsonTx))
			for i, tx := range jsonTx {
				r[i] = mempool.NewEntryFromRPCData(h[i], data[i], tx)
			}

			return r
		}
	}

	if v2, ok := jsonTemplate.(JsonBlock2); ok {
		parentBlock := getByTemplateId(v2.PrevId)

		keyPair := crypto.NewKeyPairFromPrivate(&v2.CoinbasePriv)
		expectedMainId = v2.MainId
		expectedCoinbaseId = v2.CoinbaseId
		expectedTemplateId = v2.Id

		header := getHeaderByHeight(v2.MainHeight)

		var nextBlock *sidechain.PoolBlock
		for _, t2 := range archiveCache.LoadBySideChainHeight(v2.Height + 1) {
			if t2.Side.Parent == expectedTemplateId {
				if err := processBlock(t2); err == nil {
					nextBlock = t2
					break
				}
			}
		}

		if parentBlock == nil {
			parentBlock = getByTemplateIdDirect(v2.PrevId)
		}
		if parentBlock == nil {
			//use forward method
		} else {

			if parentBlock.Main.Coinbase.GenHeight == v2.MainHeight {
				baseMainReward = parentBlock.Main.Coinbase.TotalReward
				for _, tx := range getTransactionsEntries(parentBlock.Main.Transactions...) {
					baseMainReward -= tx.Fee
				}

				deltaReward = v2.CoinbaseReward - baseMainReward
			} else if nextBlock != nil && nextBlock.Main.Coinbase.GenHeight == v2.MainHeight {
				for _, tx := range getTransactionsEntries(nextBlock.Main.Transactions...) {
					baseMainReward -= tx.Fee
				}

				deltaReward = v2.CoinbaseReward - baseMainReward
			} else {
				//fallback method can fail on very full blocks
				headerId, _ := types.HashFromString(header.Hash)

				if b, err := client.GetDefaultClient().GetBlock(headerId, context.Background()); err != nil {
					log.Panic(err)
				} else {
					//generalization, actual reward could be different in some cases
					ij, _ := b.InnerJSON()
					baseMainReward = b.BlockHeader.Reward
					var txs []types.Hash
					for _, txid := range ij.TxHashes {
						h, _ := types.HashFromString(txid)
						txs = append(txs, h)
					}
					for _, tx := range getTransactionsEntries(txs...) {
						baseMainReward -= tx.Fee
					}
				}

				deltaReward = v2.CoinbaseReward - baseMainReward
			}

			log.Printf("pool delta reward: %d (%s), base %d (%s), expected %d (%s)", deltaReward, utils.XMRUnits(deltaReward), baseMainReward, utils.XMRUnits(baseMainReward), v2.CoinbaseReward, utils.XMRUnits(v2.CoinbaseReward))

			poolBlock = &sidechain.PoolBlock{
				Main: block.Block{
					MajorVersion: uint8(header.MajorVersion),
					MinorVersion: uint8(header.MinorVersion),
					Timestamp:    v2.Ts,
					PreviousId:   v2.MinerMainId,
					Coinbase: &transaction.CoinbaseTransaction{
						Version:     2,
						UnlockTime:  v2.MainHeight + monero.MinerRewardUnlockTime,
						InputCount:  1,
						InputType:   transaction.TxInGen,
						GenHeight:   v2.MainHeight,
						TotalReward: v2.CoinbaseReward,
						Extra: transaction.ExtraTags{
							transaction.ExtraTag{
								Tag:    transaction.TxExtraTagPubKey,
								VarInt: 0,
								Data:   types.Bytes(keyPair.PublicKey.AsSlice()),
							},
							transaction.ExtraTag{
								Tag:       transaction.TxExtraTagNonce,
								VarInt:    4,
								HasVarInt: true,
								Data:      make(types.Bytes, 4), //TODO: expand nonce size as needed
							},
							transaction.ExtraTag{
								Tag:       transaction.TxExtraTagMergeMining,
								VarInt:    32,
								HasVarInt: true,
								Data:      v2.Id[:],
							},
						},
						ExtraBaseRCT: 0,
					},
					Transactions: nil,
				},
				Side: sidechain.SideData{
					PublicSpendKey:     v2.Wallet.SpendPublicKey().AsBytes(),
					PublicViewKey:      v2.Wallet.ViewPublicKey().AsBytes(),
					CoinbasePrivateKey: keyPair.PrivateKey.AsBytes(),
					Parent:             v2.PrevId,
					Uncles: func() (result []types.Hash) {
						for _, u := range v2.Uncles {
							result = append(result, u.Id)
						}
						return result
					}(),
					Height:               v2.Height,
					Difficulty:           v2.Diff,
					CumulativeDifficulty: parentBlock.Side.CumulativeDifficulty.Add(v2.Diff),
					// no extrabuffer
				},
			}
			poolBlock.CachedShareVersion = poolBlock.CalculateShareVersion(consensus)
		}

		poolBlock.Depth.Store(math.MaxUint64)

		if poolBlock.ShareVersion() > sidechain.ShareVersion_V1 {
			poolBlock.Side.CoinbasePrivateKeySeed = parentBlock.Side.CoinbasePrivateKeySeed
			if parentBlock.Main.PreviousId != poolBlock.Main.PreviousId {
				poolBlock.Side.CoinbasePrivateKeySeed = parentBlock.CalculateTransactionPrivateKeySeed()
			}
		} else {
			expectedSeed := poolBlock.CalculateTransactionPrivateKeySeed()
			kP := crypto.NewKeyPairFromPrivate(crypto2.GetDeterministicTransactionPrivateKey(expectedSeed, poolBlock.Main.PreviousId))

			if bytes.Compare(poolBlock.CoinbaseExtra(sidechain.SideCoinbasePublicKey), kP.PublicKey.AsSlice()) == 0 && bytes.Compare(kP.PrivateKey.AsSlice(), poolBlock.Side.CoinbasePrivateKey[:]) == 0 {
				poolBlock.Side.CoinbasePrivateKeySeed = expectedSeed
			} else {
				log.Printf("not deterministic private key")
			}
		}

		currentOutputs, _ := sidechain.CalculateOutputs(poolBlock, consensus, getDifficultyByHeight, getByTemplateIdDirect, derivationCache, sidechain.PreAllocateShares(consensus.ChainWindowSize*2))

		if currentOutputs == nil {
			log.Panic("could not calculate outputs blob")
		}
		poolBlock.Main.Coinbase.Outputs = currentOutputs

		if blob, err := currentOutputs.MarshalBinary(); err != nil {
			log.Panic(err)
		} else {
			poolBlock.Main.Coinbase.OutputsBlobSize = uint64(len(blob))
		}
		log.Printf("expected main id %s, template id %s, coinbase id %s", expectedMainId, expectedTemplateId, expectedCoinbaseId)

		rctHash := crypto.Keccak256([]byte{0})
		type partialBlobWork struct {
			Hashers       [2]*sha3.HasherState
			Tx            *transaction.CoinbaseTransaction
			EncodedBuffer []byte
			EncodedOffset int
			TempHash      types.Hash
		}
		var stop atomic.Bool

		var foundExtraNonce atomic.Uint32
		var foundExtraNonceSize atomic.Uint32

		minerTxBlob, _ := poolBlock.Main.Coinbase.MarshalBinary()

		searchForNonces := func(nonceSize int, max uint64) {
			coinbases := make([]*partialBlobWork, runtime.NumCPU())
			utils.SplitWork(0, max, func(workIndex uint64, routineIndex int) error {
				if stop.Load() {
					return errors.New("found nonce")
				}
				w := coinbases[routineIndex]
				if workIndex%(1024*256) == 0 {
					log.Printf("try %d/%d @ %d ~%.2f%%", workIndex, max, nonceSize, (float64(workIndex)/math.MaxUint32)*100)
				}
				binary.LittleEndian.PutUint32(w.EncodedBuffer[w.EncodedOffset:], uint32(workIndex))
				idHasher := w.Hashers[0]
				txHasher := w.Hashers[1]

				txHasher.Write(w.EncodedBuffer)
				crypto.HashFastSum(txHasher, w.TempHash[:])

				idHasher.Write(w.TempHash[:])
				// Base RCT, single 0 byte in miner tx
				idHasher.Write(rctHash[:])
				// Prunable RCT, empty in miner tx
				idHasher.Write(types.ZeroHash[:])
				crypto.HashFastSum(idHasher, w.TempHash[:])

				if w.TempHash == expectedCoinbaseId {
					foundExtraNonce.Store(uint32(workIndex))
					foundExtraNonceSize.Store(uint32(nonceSize))
					//FOUND!
					stop.Store(true)
					return errors.New("found nonce")
				}

				idHasher.Reset()
				txHasher.Reset()

				return nil
			}, func(routines, routineIndex int) error {
				if len(coinbases) < routines {
					coinbases = slices.Grow(coinbases, routines)
				}

				tx := &transaction.CoinbaseTransaction{}
				if err := tx.UnmarshalBinary(minerTxBlob); err != nil {
					return err
				}
				tx.Extra[1].VarInt = uint64(nonceSize)
				tx.Extra[1].Data = make([]byte, nonceSize)

				buf, _ := tx.MarshalBinary()

				coinbases[routineIndex] = &partialBlobWork{
					Hashers:       [2]*sha3.HasherState{crypto.GetKeccak256Hasher(), crypto.GetKeccak256Hasher()},
					Tx:            tx,
					EncodedBuffer: buf[:len(buf)-1], /* remove RCT */
					EncodedOffset: len(buf) - 1 - (types.HashSize + 1 + 1 /*Merge mining tag*/) - nonceSize,
				}

				return nil
			}, nil)
		}
		nonceSize := sidechain.SideExtraNonceSize

		//do quick search first
		for ; nonceSize <= sidechain.SideExtraNonceMaxSize; nonceSize++ {
			if stop.Load() {
				break
			}
			searchForNonces(nonceSize, math.MaxUint16)
		}
		//do deeper search next
		for nonceSize = sidechain.SideExtraNonceSize; nonceSize <= sidechain.SideExtraNonceMaxSize; nonceSize++ {
			if stop.Load() {
				break
			}
			searchForNonces(nonceSize, math.MaxUint32)
		}

		log.Printf("found extra nonce %d, size %d", foundExtraNonce.Load(), foundExtraNonceSize.Load())
		poolBlock.Main.Coinbase.Extra[1].VarInt = uint64(foundExtraNonceSize.Load())
		poolBlock.Main.Coinbase.Extra[1].Data = make([]byte, foundExtraNonceSize.Load())
		binary.LittleEndian.PutUint32(poolBlock.Main.Coinbase.Extra[1].Data, foundExtraNonce.Load())

		if poolBlock.Main.Coinbase.CalculateId() != expectedCoinbaseId {
			log.Panic()
		}

		log.Printf("got coinbase id %s", poolBlock.Main.Coinbase.CalculateId())
		minerTxBlob, _ = poolBlock.Main.Coinbase.MarshalBinary()
		log.Printf("raw coinbase: %s", hex.EncodeToString(minerTxBlob))

		var collectedTransactions mempool.Mempool

		collectTxs := func(hashes ...types.Hash) {
			var txs []types.Hash
			for _, h := range hashes {
				if slices.ContainsFunc(collectedTransactions, func(entry *mempool.MempoolEntry) bool {
					return entry.Id == h
				}) {
					continue
				}
				txs = append(txs, h)
			}

			collectedTransactions = append(collectedTransactions, getTransactionsEntries(txs...)...)
		}

		collectTxsHex := func(hashes ...string) {
			var txs []types.Hash
			for _, txid := range hashes {
				h, _ := types.HashFromString(txid)
				if slices.ContainsFunc(collectedTransactions, func(entry *mempool.MempoolEntry) bool {
					return entry.Id == h
				}) {
					continue
				}
				txs = append(txs, h)
			}

			collectedTransactions = append(collectedTransactions, getTransactionsEntries(txs...)...)
		}

		collectTxs(parentBlock.Main.Transactions...)
		if nextBlock != nil {
			collectTxs(nextBlock.Main.Transactions...)
		}

		if bh, err := client.GetDefaultClient().GetBlockByHeight(poolBlock.Main.Coinbase.GenHeight, context.Background()); err == nil {
			ij, _ := bh.InnerJSON()
			collectTxsHex(ij.TxHashes...)
		}

		/*if bh, err := client.GetDefaultClient().GetBlockByHeight(poolBlock.Main.Coinbase.GenHeight+1, context.Background()); err == nil {
			ij, _ := bh.InnerJSON()
			collectTxsHex(ij.TxHashes...)
		}*/

		for _, uncleId := range poolBlock.Side.Uncles {
			if u := getByTemplateId(uncleId); u != nil {
				if u.Main.Coinbase.GenHeight == poolBlock.Main.Coinbase.GenHeight {
					collectTxs(u.Main.Transactions...)
				}
			}
		}

		if bh, err := client.GetDefaultClient().GetBlockByHeight(poolBlock.Main.Coinbase.GenHeight-1, context.Background()); err == nil {
			ij, _ := bh.InnerJSON()
			for _, txH := range ij.TxHashes {
				//remove mined tx
				txid, _ := types.HashFromString(txH)
				if i := slices.IndexFunc(collectedTransactions, func(entry *mempool.MempoolEntry) bool {
					return entry.Id == txid
				}); i != -1 {
					collectedTransactions = slices.Delete(collectedTransactions, i, i+1)
				}
			}
		}

		minerTxWeight := uint64(len(minerTxBlob))
		totalTxWeight := collectedTransactions.Weight()

		medianWeight := getHeaderByHeight(poolBlock.Main.Coinbase.GenHeight).LongTermWeight

		log.Printf("collected transaction candidates: %d", len(collectedTransactions))

		if totalTxWeight+minerTxWeight <= medianWeight {
			//special case
			for solution := range collectedTransactions.PerfectSum(deltaReward) {
				log.Printf("got %d, %d (%s)", solution.Weight(), solution.Fees(), utils.XMRUnits(solution.Fees()))
			}
		} else {
			//sort in preference order
			pickedTxs := collectedTransactions.Pick(baseMainReward, minerTxWeight, medianWeight)

			log.Printf("got %d, %d (%s)", pickedTxs.Weight(), pickedTxs.Fees(), utils.XMRUnits(pickedTxs.Fees()))
		}

		//TODO: nonce
	}

}
