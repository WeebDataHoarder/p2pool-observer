package main

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/database"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/transaction"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/api"
	"log"
)

func processFoundBlockWithTransaction(api *api.Api, b database.BlockInterface, tx *transaction.CoinbaseTransaction) bool {
	if api.GetDatabase().CoinbaseTransactionExists(b.GetBlock()) {
		return true
	}
	log.Printf("[OUTPUT] Trying to insert transaction %s\n", b.GetBlock().Coinbase.Id.String())

	payoutHint, _ := api.GetBlockWindowPayouts(b.GetBlock())

	miners := make([]*database.Miner, 0, len(payoutHint))
	for minerId, _ := range payoutHint {
		miners = append(miners, api.GetDatabase().GetMiner(minerId))
		if miners[len(miners)-1] == nil {
			log.Panicf("minerId %d is nil", minerId)
		}
	}

	outputs := database.MatchOutputs(tx, miners, &b.GetBlock().Coinbase.PrivateKey)

	if len(outputs) == len(miners) && len(outputs) == len(tx.Outputs) {
		newOutputs := make([]*database.CoinbaseTransactionOutput, 0, len(outputs))
		for _, o := range outputs {
			newOutputs = append(newOutputs, database.NewCoinbaseTransactionOutput(b.GetBlock().Coinbase.Id, o.Output.Index, o.Output.Reward, o.Miner.Id()))
		}

		return api.GetDatabase().InsertCoinbaseTransaction(database.NewCoinbaseTransaction(b.GetBlock().Coinbase.Id, b.GetBlock().Coinbase.PrivateKey, newOutputs)) == nil
	} else {
		log.Printf("[OUTPUT] Could not find all outputs! Coinbase transaction %s, got %d, expected %d, real %d\n", b.GetBlock().Coinbase.Id.String(), len(outputs), len(miners), len(tx.Outputs))
	}

	return false
}
