package main

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/database"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/api"
	"log"
	"os"
	"time"
)

func main() {
	client.SetClientSettings(os.Getenv("MONEROD_RPC_URL"))
	db, err := database.NewDatabase(os.Args[1])
	if err != nil {
		log.Panic(err)
	}
	defer db.Close()
	api, err := api.New(db, os.Getenv("API_FOLDER"))
	if err != nil {
		log.Panic(err)
	}

	//TODO: force-insert section

	tip := db.GetChainTip()

	isFresh := tip == nil

	tipHeight := uint64(1)

	if tip != nil {
		tipHeight = tip.Height
	}

	log.Printf("[CHAIN] Last known database tip is %d\n", tipHeight)

	poolStats, err := api.GetPoolStats()
	if err != nil {
		log.Panic(err)
	}

	diskTip := poolStats.PoolStatistics.Height

	log.Printf("[CHAIN] Last known disk tip is %d\n", diskTip)

	startFrom := tipHeight

	if diskTip > tipHeight && !api.BlockExists(tipHeight+1) {
		for i := diskTip; api.BlockExists(i); i-- {
			startFrom = i
		}
	}

	if isFresh || startFrom != tipHeight {
		block, _, err := api.GetShareEntry(startFrom)
		if err != nil {
			log.Panic(err)
		}
		id := block.Id
		if block, uncles, err := api.GetShareFromRawEntry(id, true); err != nil {
			log.Panicf("[CHAIN] Could not find block %s to insert at height %d. Check disk or uncles\n", id.String(), startFrom)
		} else {
			if err = db.InsertBlock(block, nil); err != nil {
				log.Panic(err)
			}
			for _, uncle := range uncles {
				if err = db.InsertUncleBlock(uncle, nil); err != nil {
					log.Panic(err)
				}
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

				if tx, _ := client.GetClient().GetCoinbaseTransaction(b.Coinbase.Id); tx != nil {
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

				if tx, _ := client.GetClient().GetCoinbaseTransaction(u.Block.Coinbase.Id); tx != nil {
					_ = db.SetBlockFound(u.Block.Id, true)
					processFoundBlockWithTransaction(api, u, tx)
				}
			}
		}

	}

	for {
		runs++

		diskTip, _, _ := api.GetShareEntry(knownTip)

		if rawTip, _, _ := api.GetShareFromRawEntry(diskTip.Id, false); rawTip != nil {
			diskTip = rawTip
		}

		dbTip := db.GetBlockByHeight(knownTip)

		if dbTip.Id != diskTip.Id { //Reorg has happened, delete old values
			log.Printf("[REORG] Reorg happened, deleting blocks to match from height %d.\n", dbTip.Height)
			for h := knownTip; h > 0; h-- {
				dbBlock := db.GetBlockByHeight(h)
				diskBlock, _, _ := api.GetShareEntry(h)
				if dbBlock.PreviousId == diskBlock.PreviousId {
					log.Printf("[REORG] Found matching head %s at height %d\n", dbBlock.PreviousId.String(), dbBlock.Height-1)
					deleted, err := db.DeleteBlockById(dbBlock.Id)
					if err != nil {
						log.Panic(err)
					}
					log.Printf("[REORG] Deleted %d block(s).\n", deleted)
					log.Printf("[REORG] Next tip %s : %d.\n", diskBlock.PreviousId, diskBlock.Height)
					knownTip = dbBlock.Height - 1
					break
				}
			}
			continue
		}

		for h := knownTip + 1; api.BlockExists(h); h++ {
			diskBlock, _, _ := api.GetShareEntry(h)
			if diskBlock == nil {
				break
			}
			id := diskBlock.Id

			var uncles []*database.UncleBlock
			diskBlock, uncles, err = api.GetShareFromRawEntry(id, true)
			if err != nil {
				log.Printf("[CHAIN] Could not find block %s to insert at height %d. Check disk or uncles\n", id.String(), h)
				break
			}

			prevBlock := db.GetBlockByHeight(h - 1)
			if diskBlock.PreviousId != prevBlock.Id {
				log.Printf("[CHAIN] Possible reorg occurred, aborting insertion at height %d: prev id %s != id %s\n", h, diskBlock.PreviousId.String(), prevBlock.Id.String())
				break
			}

			log.Printf("[CHAIN] Inserting block %s at height %d\n", diskBlock.Id.String(), diskBlock.Height)

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

						if b, _ := api.GetRawBlock(uncle.Block.Id); b != nil {
							processFoundBlockWithTransaction(api, uncle, b.Main.Coinbase)
						}
					}

				}

				knownTip = diskBlock.Height
			}

			if diskBlock.Main.Found {
				log.Printf("[CHAIN] BLOCK FOUND! Main height %d, main id %s", diskBlock.Main.Height, diskBlock.Main.Id.String())

				if b, _ := api.GetRawBlock(diskBlock.Id); b != nil {
					processFoundBlockWithTransaction(api, diskBlock, b.Main.Coinbase)
				}
			}
		}

		if runs%10 == 0 { //Every 10 seconds or so
			for foundBlock := range db.GetAllFound(10, 0) {
				//Scan last 10 found blocks and set status accordingly if found/not found

				// Look between +1 block and +4 blocks
				if (diskTip.Main.Height-1) > foundBlock.GetBlock().Main.Height && (diskTip.Main.Height-5) < foundBlock.GetBlock().Main.Height || db.GetCoinbaseTransaction(foundBlock.GetBlock()) == nil {
					if tx, _ := client.GetClient().GetCoinbaseTransaction(foundBlock.GetBlock().Coinbase.Id); tx == nil {
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

		time.Sleep(time.Second * 1)
	}
}
