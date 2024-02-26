package utils

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"git.gammaspectra.live/P2Pool/go-monero/pkg/rpc/daemon"
	"git.gammaspectra.live/P2Pool/p2pool-observer/cmd/index"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/block"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"github.com/floatdrop/lru"
	"log"
	"math"
)

// keep last few main blocks
var mainBlockCache = lru.New[types.Hash, *block.Block](100)

func GetMainBlock(id types.Hash, client *client.Client) (*block.Block, error) {
	if bb := mainBlockCache.Get(id); bb == nil {
		blockData, err := client.GetBlock(id, context.Background())
		if err != nil {
			return nil, err
		}
		bufData, err := hex.DecodeString(blockData.Blob)
		if err != nil {
			return nil, err
		}
		mainBlock := &block.Block{}
		err = mainBlock.UnmarshalBinary(bufData)
		if err != nil {
			return nil, err
		}
		if len(mainBlock.Coinbase.Outputs) == 0 {
			//should never happen from monero itself
			return nil, errors.New("outputs not filled")
		}
		mainBlockCache.Set(id, mainBlock)
		return mainBlock, nil
	} else {
		return *bb, nil
	}
}

func FindAndInsertMainHeader(h daemon.BlockHeader, indexDb *index.Index, storeFunc func(b *sidechain.PoolBlock), client *client.Client, difficultyByHeight block.GetDifficultyByHeightFunc, getByTemplateId sidechain.GetByTemplateIdFunc, getByMainId sidechain.GetByMainIdFunc, getByMainHeight sidechain.GetByMainHeightFunc, processBlock func(b *sidechain.PoolBlock) error) error {
	mainId, _ := types.HashFromString(h.Hash)
	coinbaseId, _ := types.HashFromString(h.MinerTxHash)
	prevHash, _ := types.HashFromString(h.PrevHash)

	var indexMain *index.MainBlock
	indexMain = indexDb.GetMainBlockByHeight(h.Height)
	if indexMain == nil {
		indexMain = &index.MainBlock{}
	}
	if indexMain.Id != mainId {
		//Zero data if they don't match to insert new height
		indexMain.Metadata = make(map[string]any)
		indexMain.SideTemplateId = types.ZeroHash
		indexMain.CoinbasePrivateKey = crypto.PrivateKeyBytes{}
	}
	indexMain.Id = mainId
	indexMain.Height = h.Height
	indexMain.Timestamp = uint64(h.Timestamp)
	indexMain.Reward = h.Reward
	indexMain.CoinbaseId = coinbaseId
	indexMain.Difficulty = h.Difficulty

	// Find possible candidates that match on main height, main previous id, header nonce, timestamp, and count of transactions to reduce full block lookups
	var candidates sidechain.UniquePoolBlockSlice
	for _, b := range getByMainHeight(h.Height) {
		if b.Main.PreviousId == prevHash && b.Main.Coinbase.TotalReward == h.Reward && b.Main.Timestamp == uint64(h.Timestamp) && len(b.Main.Transactions) == int(h.NumTxes) {
			// candidate for checking
			candidates = append(candidates, b)
		}
	}

	if len(candidates) == 0 {
		// no candidates, insert header as-is
		if err := indexDb.InsertOrUpdateMainBlock(indexMain); err != nil {
			return err
		}
		return nil
	}

	utils.Logf("checking block %s at height %d: %d candidate(s)", mainId, h.Height, len(candidates))

	mainBlock, err := GetMainBlock(mainId, client)
	if err != nil {
		utils.Logf("could not get main block: %e", err)
		// insert errored block as-is
		if err := indexDb.InsertOrUpdateMainBlock(indexMain); err != nil {
			return err
		}
		return nil
	}
	var sideTemplateId types.Hash
	var extraNonce []byte

	// try to fetch extra nonce if existing
	{
		if t := mainBlock.Coinbase.Extra.GetTag(uint8(sidechain.SideExtraNonce)); t != nil {
			if len(t.Data) >= sidechain.SideExtraNonceSize || len(t.Data) < sidechain.SideExtraNonceMaxSize {
				extraNonce = t.Data
			}
		}
	}

	// get a merge tag if existing
	{
		if t := mainBlock.Coinbase.Extra.GetTag(uint8(sidechain.SideTemplateId)); t != nil {
			if len(t.Data) == types.HashSize {
				sideTemplateId = types.HashFromBytes(t.Data)
			}
		}
	}

	if sideTemplateId == types.ZeroHash || len(extraNonce) == 0 {
		utils.Logf("checking block %s at height %d: not a p2pool block", mainId, h.Height)
		if err := indexDb.InsertOrUpdateMainBlock(indexMain); err != nil {
			return err
		}
		return nil
	}

	if t := getByTemplateId(sideTemplateId); t != nil {
		//found template
		if err := processBlock(t); err != nil {
			return fmt.Errorf("could not process template: %w", err)
		}

		if t.MainId() == mainId { //exact match, no need to re-arrange template inclusion
			sideBlock := indexDb.GetSideBlockByMainId(t.MainId())
			if sideBlock == nil {
				//found deep orphan which found block, insert parents
				cur := t
				for cur != nil && !index.QueryHasResults(indexDb.GetSideBlocksByTemplateId(cur.SideTemplateId(indexDb.Consensus()))) {
					orphanBlock, orphanUncles, err := indexDb.GetSideBlockFromPoolBlock(cur, index.InclusionOrphan)
					if err != nil {
						break
					}

					if index.QueryHasResults(indexDb.GetSideBlocksByTemplateId(orphanBlock.TemplateId)) {
						continue
					}
					if err := indexDb.InsertOrUpdateSideBlock(orphanBlock); err != nil {
						return fmt.Errorf("error inserting %s, %s at %d: %s", cur.SideTemplateId(indexDb.Consensus()), cur.MainId(), cur.Side.Height, err)
					}

					for _, orphanUncle := range orphanUncles {
						if index.QueryHasResults(indexDb.GetSideBlocksByTemplateId(orphanUncle.TemplateId)) {
							continue
						}
						if err := indexDb.InsertOrUpdateSideBlock(orphanUncle); err != nil {
							return fmt.Errorf("error inserting uncle of %s, %s; %s, %s at %d: %s", cur.SideTemplateId(indexDb.Consensus()), cur.MainId(), orphanUncle.TemplateId, orphanUncle.MainId, cur.Side.Height, err)
						}
					}

					cur = getByTemplateId(cur.Side.Parent)
					if cur == nil {
						break
					}
					if err := processBlock(cur); err != nil {
						return fmt.Errorf("could not process cur template: %w", err)
					}
				}
				sideBlock = indexDb.GetSideBlockByMainId(t.MainId())
				if sideBlock == nil {
					return errors.New("no block in database for orphan")
				}
				sideBlock.Inclusion = index.InclusionOrphan
			} else if sideBlock.Inclusion == index.InclusionAlternateInVerifiedChain {
				sideBlock.Inclusion = index.InclusionInVerifiedChain
			}

			if err := indexDb.InsertOrUpdateSideBlock(sideBlock); err != nil {
				return fmt.Errorf("error inserting %s, %s at %d: %s", sideBlock.TemplateId, sideBlock.MainId, sideBlock.SideHeight, err)
			}
			t.Depth.Store(math.MaxUint64)
			storeFunc(t)

			indexMain.SideTemplateId = sideTemplateId
			indexMain.CoinbasePrivateKey = t.Side.CoinbasePrivateKey
			if err := indexDb.InsertOrUpdateMainBlock(indexMain); err != nil {
				return err
			}
			utils.Logf("INSERTED found block %s at height %d, template id %s", mainId, h.Height, sideTemplateId)
			if err := FindAndInsertMainHeaderOutputs(indexMain, indexDb, client, difficultyByHeight, getByTemplateId, getByMainId, getByMainHeight, processBlock); err != nil {
				utils.Logf("error inserting coinbase outputs: %s", err)
			}
			return nil
		} else {
			data, _ := t.MarshalBinary()
			newBlock := &sidechain.PoolBlock{}
			if err := newBlock.UnmarshalBinary(indexDb.Consensus(), &sidechain.NilDerivationCache{}, data); err != nil {
				log.Panic(err)
			}
			copy(newBlock.CoinbaseExtra(sidechain.SideExtraNonce), extraNonce)
			newBlock.Main.Nonce = uint32(h.Nonce)
			newBlock.Depth.Store(math.MaxUint64)

			if newBlock.MainId() == mainId {
				//store into archive
				storeFunc(newBlock)
				//insert into db as well
				var sideBlock *index.SideBlock
				if indexDb.GetSideBlockByMainId(t.MainId()).Inclusion == index.InclusionOrphan {
					sideBlock, _, err = indexDb.GetSideBlockFromPoolBlock(newBlock, index.InclusionOrphan)
				} else {
					sideBlock, _, err = indexDb.GetSideBlockFromPoolBlock(newBlock, index.InclusionInVerifiedChain)
				}
				if err != nil {
					return fmt.Errorf("could not process %s, at side height %d, template id %s: %w", types.HashFromBytes(t.CoinbaseExtra(sidechain.SideTemplateId)), t.Side.Height, sideTemplateId, err)
				}

				if err := indexDb.InsertOrUpdateSideBlock(sideBlock); err != nil {
					return fmt.Errorf("error inserting %s, %s at %d: %s", sideBlock.TemplateId, sideBlock.MainId, sideBlock.SideHeight, err)
				}

				indexMain.SideTemplateId = sideTemplateId
				indexMain.CoinbasePrivateKey = newBlock.Side.CoinbasePrivateKey
				if err := indexDb.InsertOrUpdateMainBlock(indexMain); err != nil {
					return err
				}
				utils.Logf("INSERTED ALTERNATE found block %s at height %d, template id %s", mainId, h.Height, sideTemplateId)
				if err := FindAndInsertMainHeaderOutputs(indexMain, indexDb, client, difficultyByHeight, getByTemplateId, getByMainId, getByMainHeight, processBlock); err != nil {
					utils.Logf("error inserting coinbase outputs: %s", err)
				}
				return nil
			}
		}
	}

	utils.Logf("checking block %s at height %d, template id %s: could not find matching template id", mainId, h.Height, sideTemplateId)

	// could not find template, maybe it's other pool?

	// fill template id for future reference
	// TODO: do this for all blocks
	indexMain.SetMetadata("merge_mining_tag", sideTemplateId)
	indexMain.SetMetadata("extra_nonce", binary.LittleEndian.Uint32(extraNonce))

	if err := indexDb.InsertOrUpdateMainBlock(indexMain); err != nil {
		return err
	}
	return nil
}

func FindAndInsertMainHeaderOutputs(mb *index.MainBlock, indexDb *index.Index, client *client.Client, difficultyByHeight block.GetDifficultyByHeightFunc, getByTemplateId sidechain.GetByTemplateIdFunc, getByMainId sidechain.GetByMainIdFunc, getByMainHeight sidechain.GetByMainHeightFunc, processBlock func(b *sidechain.PoolBlock) error) error {
	if !index.QueryHasResults(indexDb.GetMainCoinbaseOutputs(mb.CoinbaseId)) {
		//fill information
		utils.Logf("inserting coinbase outputs for %s, template id %s, coinbase id %s", mb.Id, mb.SideTemplateId, mb.CoinbaseId)
		var t *sidechain.PoolBlock
		if t = getByTemplateId(mb.SideTemplateId); t == nil || t.MainId() != mb.Id {
			t = nil
		}
		if t == nil {
			t = getByMainId(mb.Id)
		}
		if t != nil {
			if err := processBlock(t); err != nil {
				return fmt.Errorf("could not process block: %s", err)
			}
			if mb.CoinbaseId != t.CoinbaseId() {
				return fmt.Errorf("not matching coinbase id: %s vs %s", mb.CoinbaseId, t.CoinbaseId())
			}
			indexes, err := client.GetOutputIndexes(mb.CoinbaseId)
			if err != nil {
				return fmt.Errorf("error getting output indexes: %w", err)
			}
			if len(indexes) != len(t.Main.Coinbase.Outputs) {
				return fmt.Errorf("not matching indexes vs coinbase output len: %d vs %d", len(indexes), len(t.Main.Coinbase.Outputs))
			}
			preAllocatedShares := sidechain.PreAllocateShares(indexDb.Consensus().ChainWindowSize * 2)
			shares, _ := sidechain.GetShares(t, indexDb.Consensus(), difficultyByHeight, getByTemplateId, preAllocatedShares)
			if len(shares) != len(t.Main.Coinbase.Outputs) {
				return fmt.Errorf("not matching shares vs coinbase output len: %d vs %d", len(shares), len(t.Main.Coinbase.Outputs))
			}
			outputs := make(index.MainCoinbaseOutputs, 0, len(indexes))
			for _, o := range t.Main.Coinbase.Outputs {
				output := index.MainCoinbaseOutput{
					Id:                mb.CoinbaseId,
					Index:             uint32(o.Index),
					GlobalOutputIndex: indexes[o.Index],
					Miner:             indexDb.GetOrCreateMinerPackedAddress(shares[o.Index].Address).Id(),
					Value:             o.Reward,
				}
				outputs = append(outputs, output)
			}
			if err := indexDb.InsertOrUpdateMainCoinbaseOutputs(outputs); err != nil {
				return err
			}
		} else {
			return errors.New("nil template")
		}
	}
	return nil
}
