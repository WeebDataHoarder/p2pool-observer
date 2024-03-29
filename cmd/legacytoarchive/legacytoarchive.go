package main

import (
	"encoding/hex"
	"flag"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/cache/archive"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"github.com/floatdrop/lru"
	"math"
	"os"
	"path"
)

func main() {
	inputConsensus := flag.String("consensus", "config.json", "Input config.json consensus file")
	inputFolder := flag.String("input", "", "Input legacy api folder with raw / failed blocks")
	outputArchive := flag.String("output", "", "Output path for archive database")

	flag.Parse()

	cf, err := os.ReadFile(*inputConsensus)

	consensus, err := sidechain.NewConsensusFromJSON(cf)
	if err != nil {
		utils.Panic(err)
	}

	archiveCache, err := archive.NewCache(*outputArchive, consensus, func(height uint64) types.Difficulty {
		return types.ZeroDifficulty
	})
	if err != nil {
		utils.Panic(err)
	}
	defer archiveCache.Close()

	processed := make(map[types.Hash]bool)

	totalStored := 0

	derivationCache := sidechain.NewDerivationLRUCache()

	blockCache := lru.New[types.Hash, *sidechain.PoolBlock](int(consensus.ChainWindowSize * 4 * 60))

	loadBlock := func(id types.Hash) *sidechain.PoolBlock {
		n := id.String()
		fPath := path.Join(*inputFolder, "blocks", n[:1], n)
		if buf, err := os.ReadFile(fPath); err != nil {
			return nil
		} else {
			if hexBuf, err := hex.DecodeString(string(buf)); err != nil {
				utils.Panic(err)
			} else {
				if block, err := sidechain.NewShareFromExportedBytes(hexBuf, consensus, derivationCache); err != nil {
					utils.Error("", "error decoding block %s, %s", id.String(), err)
				} else {
					block.Depth.Store(math.MaxUint64)
					return block
				}
			}
		}
		return nil
	}

	getByTemplateId := func(h types.Hash) *sidechain.PoolBlock {
		if v := blockCache.Get(h); v == nil {
			if b := loadBlock(h); b != nil {
				b.Depth.Store(math.MaxUint64)
				blockCache.Set(h, b)
				return b
			}
			return nil
		} else {
			return *v
		}
	}

	var storeBlock func(k types.Hash, b *sidechain.PoolBlock, depth uint64)
	storeBlock = func(k types.Hash, b *sidechain.PoolBlock, depth uint64) {
		if b == nil || processed[k] {
			return
		}
		if depth >= consensus.ChainWindowSize*4*30 { //avoid infinite memory growth
			return
		}
		if parent := getByTemplateId(b.Side.Parent); parent != nil {
			storeBlock(b.Side.Parent, parent, depth+1)
			topDepth := parent.Depth.Load()
			b.FillTransactionParentIndices(parent)
			if topDepth == math.MaxUint64 {
				b.Depth.Store(consensus.ChainWindowSize * 2)
			} else if topDepth == 0 {
				b.Depth.Store(0)
			} else {
				b.Depth.Store(topDepth - 1)
			}
		} else {
			b.Depth.Store(math.MaxUint64)
		}
		archiveCache.Store(b)
		totalStored++
		processed[k] = true
	}

	for i := 0; i <= 0xf; i++ {
		n := hex.EncodeToString([]byte{byte(i)})
		dPath := path.Join(*inputFolder, "blocks", n[1:])
		utils.Logf("", "Reading directory %s", dPath)
		if dir, err := os.ReadDir(dPath); err != nil {
			utils.Panic(err)
		} else {
			for _, e := range dir {
				h, _ := hex.DecodeString(e.Name())
				id := types.HashFromBytes(h)
				bb := getByTemplateId(id)
				if bb != nil && !archiveCache.ExistsByMainId(bb.MainId()) {
					storeBlock(id, bb, 0)
				}
			}
		}
	}

	for i := 0; i <= 0xf; i++ {
		n := hex.EncodeToString([]byte{byte(i)})
		dPath := path.Join(*inputFolder, "failed_blocks", n[1:])
		if dir, err := os.ReadDir(dPath); err != nil {
			utils.Panic(err)
		} else {
			for _, e := range dir {
				fPath := path.Join(dPath, e.Name())
				utils.Logf("", "Processing %s", fPath)
				if buf, err := os.ReadFile(path.Join(dPath, e.Name())); err != nil {
					utils.Panic(err)
				} else {
					if hexBuf, err := hex.DecodeString(string(buf)); err != nil {
						utils.Panic(err)
					} else {
						if block, err := sidechain.NewShareFromExportedBytes(hexBuf, consensus, derivationCache); err != nil {
							utils.Panic(err)
						} else {
							block.Depth.Store(math.MaxUint64)
							archiveCache.Store(block)
							totalStored++
						}
					}
				}
			}
		}
	}

	utils.Logf("", "total stored %d", totalStored)
}
