package main

import (
	"encoding/hex"
	"flag"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/cache/archive"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"log"
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
		log.Panic(err)
	}

	archiveCache, err := archive.NewCache(*outputArchive, consensus, func(height uint64) types.Difficulty {
		return types.ZeroDifficulty
	})
	if err != nil {
		log.Panic(err)
	}
	defer archiveCache.Close()

	processed := make(map[types.Hash]bool)

	totalStored := 0

	derivationCache := sidechain.NewDerivationCache()

	loadBlock := func(id types.Hash) *sidechain.PoolBlock {
		n := id.String()
		fPath := path.Join(*inputFolder, "blocks", n[:1], n)
		if buf, err := os.ReadFile(fPath); err != nil {
			return nil
		} else {
			if hexBuf, err := hex.DecodeString(string(buf)); err != nil {
				log.Panic(err)
			} else {
				if block, err := sidechain.NewShareFromExportedBytes(hexBuf, consensus.NetworkType, derivationCache); err != nil {
					log.Printf("error decoding block %s, %s", id.String(), err)
				} else {
					return block
				}
			}
		}
		return nil
	}

	var storeBlock func(k types.Hash, b *sidechain.PoolBlock, depth uint64)
	storeBlock = func(k types.Hash, b *sidechain.PoolBlock, depth uint64) {
		if b == nil || processed[k] {
			return
		}
		if depth >= consensus.ChainWindowSize*4*30 { //avoid infinite memory growth
			return
		}
		if parent := loadBlock(b.Side.Parent); parent != nil {
			b.FillTransactionParentIndices(parent)
			storeBlock(b.Side.Parent, parent, depth+1)
		}
		b.Depth.Store(math.MaxUint64)
		archiveCache.Store(b)
		totalStored++
		processed[k] = true
	}

	for i := 0; i <= 0xf; i++ {
		n := hex.EncodeToString([]byte{byte(i)})
		dPath := path.Join(*inputFolder, "blocks", n[1:])
		log.Printf("Reading directory %s", dPath)
		if dir, err := os.ReadDir(dPath); err != nil {
			log.Panic(err)
		} else {
			for _, e := range dir {
				h, _ := hex.DecodeString(e.Name())
				id := types.HashFromBytes(h)
				bb := loadBlock(id)
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
			log.Panic(err)
		} else {
			for _, e := range dir {
				fPath := path.Join(dPath, e.Name())
				log.Printf("Processing %s", fPath)
				if buf, err := os.ReadFile(path.Join(dPath, e.Name())); err != nil {
					log.Panic(err)
				} else {
					if hexBuf, err := hex.DecodeString(string(buf)); err != nil {
						log.Panic(err)
					} else {
						if block, err := sidechain.NewShareFromExportedBytes(hexBuf, consensus.NetworkType, derivationCache); err != nil {
							log.Panic(err)
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

	log.Printf("total stored %d", totalStored)
}
