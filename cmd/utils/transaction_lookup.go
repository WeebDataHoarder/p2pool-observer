package utils

import (
	"context"
	"git.gammaspectra.live/P2Pool/p2pool-observer/index"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
)

func LookupTransactions(requestOther func(ctx context.Context, indices []uint64) []*index.MatchedOutput, indexDb *index.Index, ctx context.Context, ids ...types.Hash) (results []index.TransactionInputQueryResults) {
	txs, err := client.GetDefaultClient().GetTransactionInputs(ctx, ids...)
	if err != nil || len(txs) != len(ids) {
		return nil
	}

	decoys := make([]uint64, 0, len(txs)*16*16)
	for _, tx := range txs {
		for _, i := range tx.Inputs {
			decoys = append(decoys, i.KeyOffsets...)
		}
	}

	var otherResult []*index.MatchedOutput
	if requestOther != nil {
		otherResult = requestOther(ctx, decoys)
		if len(otherResult) != len(decoys) {
			otherResult = nil
		}
	}

	var otherIndex int
	for _, tx := range txs {
		queryResult := indexDb.QueryTransactionInputs(tx.Inputs)

		if otherResult != nil {
			for i, input := range tx.Inputs {
				for j := range input.KeyOffsets {
					output := otherResult[otherIndex]
					otherIndex++
					if output == nil {
						continue
					}
					if queryResult[i].MatchedOutputs[j] == nil { //todo: multiple matches??
						queryResult[i].MatchedOutputs[j] = output
					}
				}
			}
		}

		results = append(results, queryResult)
	}

	return results
}
