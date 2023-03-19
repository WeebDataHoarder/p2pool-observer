package main

import (
	"flag"
	"git.gammaspectra.live/P2Pool/p2pool-observer/database"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/randomx"
	p2poolapi "git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/api"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"log"
	"os"
	"time"
)

func blockId(b *sidechain.PoolBlock) types.Hash {
	return types.HashFromBytes(b.CoinbaseExtra(sidechain.SideTemplateId))
}

func main() {
	startFromHeight := flag.Uint64("from", 0, "Start sync from this height")
	dbString := flag.String("db", "", "")
	p2poolApiHost := flag.String("api-host", "", "Host URL for p2pool go observer consensus")
	flag.Parse()
	client.SetDefaultClientSettings(os.Getenv("MONEROD_RPC_URL"))
	db, err := database.NewDatabase(*dbString)
	if err != nil {
		log.Panic(err)
	}
	defer db.Close()
	api, err := p2poolapi.New(db, os.Getenv("API_FOLDER"))
	if err != nil {
		log.Panic(err)
	}

	p2api := p2poolapi.NewP2PoolApi(*p2poolApiHost)

	//TODO: force-insert section

	tip := db.GetChainTip()

	/*bId, _ := types.HashFromString("f36ae9e2ed3e1e2ab7b79c9ac2767692f02c41b2aaf78e16fcc123730354ce86")
	b := db.GetBlockById(bId)
	if tx, _ := client.GetDefaultClient().GetCoinbaseTransaction(b.Coinbase.Id); tx != nil {
		_ = db.SetBlockFound(b.Id, true)
		processFoundBlockWithTransaction(api, b, tx)
	}*/

	isFresh := tip == nil

	var tipHeight uint64

	if tip != nil {
		tipHeight = tip.Height
	}

	log.Printf("[CHAIN] Last known database tip is %d\n", tipHeight)

	for status := p2api.Status(); !p2api.Status().Synchronized; status = p2api.Status() {
		log.Printf("[API] Not synchronized (height %d, id %s), waiting five seconds", status.Height, status.Id)
		time.Sleep(time.Second * 5)
	}

	log.Printf("[CHAIN] Consensus id = %s\n", p2api.Consensus().Id())

	getSeedByHeight := func(height uint64) (hash types.Hash) {
		seedHeight := randomx.SeedHeight(height)
		if h := p2api.MainHeaderByHeight(seedHeight); h == nil {
			return types.ZeroHash
		} else {
			return h.Id
		}
	}

	tipChain, tipUncles := p2api.StateFromTip()

	if len(tipChain) == 0 || len(tipChain) < int(p2api.Consensus().ChainWindowSize*2) {
		log.Panicf("[CHAIN] Too small length %d", len(tipChain))
	}

	p2poolTip := tipChain[0].Side.Height

	log.Printf("[CHAIN] Last known p2pool tip is %d\n", p2poolTip)

	startFrom := utils.Max(tipHeight, tipChain[len(tipChain)-1].Side.Height)
	//TODO: archive

	if *startFromHeight != 0 {
		log.Printf("[CHAIN] Forcing start tip to %d\n", *startFromHeight)
		startFrom = *startFromHeight
	}

	if isFresh || startFrom != p2poolTip {

		if startFrom > p2poolTip {
			startFrom = p2poolTip
		}


		if isFresh {
			currentIndex := int(p2poolTip - startFrom)
			if currentIndex > (len(tipChain) - 1) {
				currentIndex = len(tipChain) - 1
			}

			for ; currentIndex >= 0; currentIndex-- {
				b := tipChain[currentIndex]

				if block, uncles, err := database.NewBlockFromBinaryBlock(getSeedByHeight, p2api.MainDifficultyByHeight, db, b, tipUncles, true); err != nil {
					log.Panicf("[CHAIN] Could not find share %s to insert at height %d. Check disk or uncles: %s\n", blockId(b).String(), b.Side.Height, err)
				} else {
					log.Printf("[CHAIN] Inserting share %s at height %d\n", blockId(b).String(), b.Side.Height)
					if err = db.InsertBlock(block, nil); err != nil {
						log.Panic(err)
					}
					for _, uncle := range uncles {
						log.Printf("[CHAIN] Inserting uncle share %s at height %d\n", uncle.Block.Id.String(), b.Side.Height)
						if err = db.InsertUncleBlock(uncle, nil); err != nil {
							log.Panic(err)
						}
					}
				}

				startFrom = b.Side.Height
			}
		}
	}

	//TODO: handle jumps in blocks (missing data)

	knownTip := startFrom

	log.Printf("[CHAIN] Starting tip from height %d\n", knownTip)

	runs := 0

	//Fix blocks without height
	for b := range db.GetBlocksByQuery("WHERE miner_main_difficulty = 'ffffffffffffffffffffffffffffffff' ORDER BY main_height ASC;") {
		cacheHeightDifficulty(b.Main.Height)

		if diff, ok := getHeightDifficulty(b.Main.Height); ok {
			log.Printf("[CHAIN] Filling main difficulty for share %d, main height %d\n", b.Height, b.Main.Height)
			_ = db.SetBlockMainDifficulty(b.Id, diff)
			b = db.GetBlockById(b.Id)

			if !b.Main.Found && b.IsProofHigherThanDifficulty() {
				log.Printf("[CHAIN] BLOCK FOUND! Main height %d, main id %s\n", b.Main.Height, b.Main.Id.String())

				if tx, _ := client.GetDefaultClient().GetCoinbaseTransaction(b.Coinbase.Id); tx != nil {
					_ = db.SetBlockFound(b.Id, true)
					processFoundBlockWithTransaction(api, b, tx)
				}
			}
		}

	}

	//Fix uncle without height
	for u := range db.GetUncleBlocksByQuery("WHERE miner_main_difficulty = 'ffffffffffffffffffffffffffffffff' ORDER BY main_height ASC;") {
		cacheHeightDifficulty(u.Block.Main.Height)

		if diff, ok := getHeightDifficulty(u.Block.Main.Height); ok {
			log.Printf("[CHAIN] Filling main difficulty for uncle share %d, main height %d\n", u.Block.Height, u.Block.Main.Height)
			_ = db.SetBlockMainDifficulty(u.Block.Id, diff)
			u = db.GetUncleById(u.Block.Id)

			if !u.Block.Main.Found && u.Block.IsProofHigherThanDifficulty() {
				log.Printf("[CHAIN] BLOCK FOUND! Main height %d, main id %s\n", u.Block.Main.Height, u.Block.Main.Id.String())

				if tx, _ := client.GetDefaultClient().GetCoinbaseTransaction(u.Block.Coinbase.Id); tx != nil {
					_ = db.SetBlockFound(u.Block.Id, true)
					processFoundBlockWithTransaction(api, u, tx)
				}
			}
		}

	}

	for {

	toStart:

		time.Sleep(time.Second * 1)
		runs++

		p2tip := p2api.Tip()
		if p2tip == nil {
			log.Panicf("[CHAIN] could not find tip, at %d", knownTip)
		}

		if p2tip.Side.Height < knownTip {
			log.Panicf("[CHAIN] tip went backwards! %d -> %d", knownTip, p2tip.Side.Height)
		} else if p2tip.Side.Height > (knownTip + 1) {
			log.Printf("[CHAIN] tip went forwards! Rolling back %d -> %d", knownTip, p2tip.Side.Height)
			for i := p2tip.Side.Height - (knownTip + 1); i > 0 && p2tip != nil; i-- {
				p2tip = p2api.ByTemplateId(p2tip.Side.Parent)
			}
		}
		if p2tip == nil {
			log.Panicf("[CHAIN] could not find tip after depth, at %d", knownTip)
		}

		dbTip := db.GetBlockByHeight(knownTip)

		if dbTip.Id == blockId(p2tip) { // no changes
			continue
		}

		if dbTip.Id != p2tip.Side.Parent { //Reorg has happened, delete old values
			log.Printf("[REORG] Reorg happened, deleting blocks to match from height %d.\n", dbTip.Height)

			diskBlock := p2tip
			for h := knownTip; h > 0; h-- {
				dbBlock := db.GetBlockByHeight(h)
				diskBlock = p2api.ByTemplateId(diskBlock.Side.Parent)
				if dbBlock.Id == blockId(diskBlock) {
					log.Printf("[REORG] Found matching head %s at height %d\n", dbBlock.PreviousId.String(), dbBlock.Height-1)
					deleted, err := db.DeleteBlockById(dbBlock.Id)
					if err != nil {
						log.Panic(err)
					}
					log.Printf("[REORG] Deleted %d shares(s).\n", deleted)
					log.Printf("[REORG] Next tip %s : %d.\n", blockId(diskBlock), diskBlock.Side.Height)
					knownTip = dbBlock.Height - 1
					break
				}
			}
			continue
		}

		tipUncles = tipUncles[:0]
		for _, uncleId := range p2tip.Side.Uncles {
			if u := p2api.ByTemplateId(uncleId); u == nil {
				goto toStart
			} else {
				tipUncles = append(tipUncles, u)
			}
		}

		diskBlock, uncles, err := database.NewBlockFromBinaryBlock(getSeedByHeight, p2api.MainDifficultyByHeight, db, p2tip, tipUncles, true)

		if err != nil {
			log.Printf("[CHAIN] Could not find share %s to insert at height %d. Check disk or uncles\n", blockId(p2tip), p2tip.Side.Height)
			continue
		}

		prevBlock := db.GetBlockByHeight(p2tip.Side.Height - 1)
		if diskBlock.PreviousId != prevBlock.Id {
			log.Printf("[CHAIN] Possible reorg occurred, aborting insertion at height %d: prev id %s != id %s\n", p2tip.Side.Height, diskBlock.PreviousId.String(), prevBlock.Id.String())
			continue
		}

		log.Printf("[CHAIN] Inserting share %s at height %d\n", diskBlock.Id.String(), diskBlock.Height)

		cacheHeightDifficulty(diskBlock.Main.Height)

		diff, ok := getHeightDifficulty(diskBlock.Main.Height)

		if ok {
			err = db.InsertBlock(diskBlock, &diff)
		} else {
			err = db.InsertBlock(diskBlock, nil)
		}

		if err == nil {
			for _, uncle := range uncles {
				log.Printf("[CHAIN] Inserting uncle %s @ %s at %d", uncle.Block.Main.Id.String(), diskBlock.Id.String(), diskBlock.Height)

				diff, ok := getHeightDifficulty(uncle.Block.Main.Height)

				if ok {
					err = db.InsertUncleBlock(uncle, &diff)
				} else {
					err = db.InsertUncleBlock(uncle, nil)
				}

				if uncle.Block.Main.Found {
					log.Printf("[CHAIN] BLOCK FOUND! (uncle) Main height %d, main id %s", uncle.Block.Main.Height, uncle.Block.Main.Id.String())

					if b := tipUncles.Get(uncle.Block.Id); b != nil {
						processFoundBlockWithTransaction(api, uncle, b.Main.Coinbase)
					}
				}

			}

			knownTip = diskBlock.Height
		}

		if diskBlock.Main.Found {
			log.Printf("[CHAIN] BLOCK FOUND! Main height %d, main id %s", diskBlock.Main.Height, diskBlock.Main.Id.String())

			if b := p2api.ByTemplateId(diskBlock.Id); b != nil {
				processFoundBlockWithTransaction(api, diskBlock, b.Main.Coinbase)
			}
		}

		if runs%10 == 0 { //Every 10 seconds or so
			for foundBlock := range db.GetAllFound(10, 0) {
				//Scan last 10 found blocks and set status accordingly if found/not found

				// Look between +1 block and +4 blocks
				if (p2tip.Main.Coinbase.GenHeight-1) > foundBlock.GetBlock().Main.Height && (p2tip.Main.Coinbase.GenHeight-5) < foundBlock.GetBlock().Main.Height || db.GetCoinbaseTransaction(foundBlock.GetBlock()) == nil {
					if tx, _ := client.GetDefaultClient().GetCoinbaseTransaction(foundBlock.GetBlock().Coinbase.Id); tx == nil {
						// If more than two minutes have passed before we get utxo, remove from found
						log.Printf("[CHAIN] Block that was found at main height %d, cannot find output, marking not found\n", foundBlock.GetBlock().Main.Height)
						_ = db.SetBlockFound(foundBlock.GetBlock().Id, false)
					} else {
						processFoundBlockWithTransaction(api, foundBlock, tx)
					}
				}
			}
		}

		if isFresh {
			//TODO: Do migration tasks
			isFresh = false
		}
	}
}
