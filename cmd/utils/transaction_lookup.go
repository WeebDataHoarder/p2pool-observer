package utils

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/cmd/index"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func LookupTransactions(requestOther func(ctx context.Context, indices []uint64) []*index.MatchedOutput, indexDb *index.Index, ctx context.Context, inputThreshold int, ids ...types.Hash) (results []index.TransactionInputQueryResults) {
	txs, err := client.GetDefaultClient().GetTransactionInputs(ctx, ids...)
	if err != nil || len(txs) != len(ids) {
		return nil
	}

	decoys := make([]uint64, 0, len(txs)*16*16)
	for _, tx := range txs {
		if len(tx.Inputs) < inputThreshold {
			continue
		}
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
		if len(tx.Inputs) < inputThreshold {
			queryResult := make(index.TransactionInputQueryResults, len(tx.Inputs))
			for i := range queryResult {
				queryResult[i].Input = tx.Inputs[i]
				queryResult[i].MatchedOutputs = make([]*index.MatchedOutput, len(tx.Inputs[i].KeyOffsets))
			}
			results = append(results, queryResult)
			continue
		}
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

var otherLookupHostFunc func(ctx context.Context, indices []uint64) []*index.MatchedOutput

func init() {
	if os.Getenv("TRANSACTION_LOOKUP_OTHER") != "" {
		otherLookupHostFunc = func(ctx context.Context, indices []uint64) (result []*index.MatchedOutput) {
			data, _ := utils.MarshalJSON(indices)

			result = make([]*index.MatchedOutput, len(indices))

			for _, host := range strings.Split(os.Getenv("TRANSACTION_LOOKUP_OTHER"), ",") {
				host = strings.TrimSpace(host)
				if host == "" {
					continue
				}
				uri, _ := url.Parse(host + "/api/global_indices_lookup")
				if response, err := http.DefaultClient.Do(&http.Request{
					Method: "POST",
					URL:    uri,
					Body:   io.NopCloser(bytes.NewReader(data)),
				}); err == nil {
					func() {
						defer response.Body.Close()
						if response.StatusCode == http.StatusOK {
							if data, err := io.ReadAll(response.Body); err == nil {
								r := make([]*index.MatchedOutput, 0, len(indices))
								if utils.UnmarshalJSON(data, &r) == nil && len(r) == len(indices) {
									for i := range r {
										if result[i] == nil {
											result[i] = r[i]
										}
									}
								}
							}
						}
					}()
				}
			}
			return result
		}
	}
}

func ProcessFullBlock(b *index.MainBlock, indexDb *index.Index) error {

	var sideTemplateId types.Hash
	var extraNonce []byte
	mainBlock, err := GetMainBlock(b.Id, client.GetDefaultClient())
	if err != nil {
		return fmt.Errorf("could not get main block for %s at %d: %w", b.Id, b.Height, err)
	}

	utils.Logf("Block %s at %d (%s), processing %d transactions", b.Id, b.Height, time.Unix(int64(b.Timestamp), 0).UTC().Format("02-01-2006 15:04:05 MST"), len(mainBlock.Transactions))

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

	if sideTemplateId != types.ZeroHash && len(extraNonce) != 0 {
		b.SetMetadata("merge_mining_tag", sideTemplateId)
		b.SetMetadata("extra_nonce", binary.LittleEndian.Uint32(extraNonce))
		utils.Logf("p2pool tags found template id %s, extra nonce %d", sideTemplateId, binary.LittleEndian.Uint32(extraNonce))
	}

	const txInputThreshold = 4
	const txInputThresholdForRatio = 8

	if len(mainBlock.Transactions) > 0 {

		results := LookupTransactions(otherLookupHostFunc, indexDb, context.Background(), txInputThreshold, mainBlock.Transactions...)

		for i, inputs := range results {
			txId := mainBlock.Transactions[i]

			if len(inputs) < txInputThreshold {
				//skip due to not enough matches later on
				continue
			}

			matches := inputs.Match()

			var topMiner *index.TransactionInputQueryResultsMatch
			for j, m := range matches {
				if m.Address == nil {
					continue
				} else if topMiner == nil {
					topMiner = &matches[j]
				} else {
					if topMiner.Count <= 2 && topMiner.Count == m.Count {
						//if count is not greater
						topMiner = nil
					}
					break
				}
			}

			if topMiner != nil {
				var noMinerCount, minerCount, otherMinerCount uint64
				for _, i := range inputs {
					var isNoMiner, isMiner, isOtherMiner bool
					for _, o := range i.MatchedOutputs {
						if o == nil {
							isNoMiner = true
						} else if topMiner.Address.Compare(o.Address) == 0 {
							isMiner = true
						} else {
							isOtherMiner = true
						}
					}

					if isMiner {
						minerCount++
					} else if isOtherMiner {
						otherMinerCount++
					} else if isNoMiner {
						noMinerCount++
					}
				}

				minerRatio := float64(minerCount) / float64(len(inputs))
				noMinerRatio := float64(noMinerCount) / float64(len(inputs))
				otherMinerRatio := float64(otherMinerCount) / float64(len(inputs))
				var likelyMiner bool
				if (len(inputs) >= txInputThresholdForRatio && minerRatio >= noMinerRatio && minerRatio > otherMinerRatio) || (len(inputs) >= txInputThresholdForRatio && minerRatio > 0.35 && minerRatio > otherMinerRatio) || (len(inputs) >= txInputThreshold && minerRatio > 0.75) {
					likelyMiner = true
				}

				if likelyMiner {
					utils.Logf("transaction %s is LIKELY for %s: miner ratio %.02f (%d/%d), none %.02f (%d/%d), other %.02f (%d/%d); coinbase %d, sweep %d", txId, topMiner.Address.ToBase58(), minerRatio, minerCount, len(inputs), noMinerRatio, noMinerCount, len(inputs), otherMinerRatio, otherMinerCount, len(inputs), topMiner.CoinbaseCount, topMiner.SweepCount)

					minimalInputs := make(index.MinimalTransactionInputQueryResults, len(inputs))

					decoyCount := len(inputs[0].Input.KeyOffsets)
					spendingOutputIndices := make([]uint64, 0, len(inputs)*decoyCount)
					for j, input := range inputs {
						spendingOutputIndices = append(spendingOutputIndices, input.Input.KeyOffsets...)
						minimalInputs[j].Input = input.Input
						minimalInputs[j].MatchedOutputs = make([]*index.MinimalMatchedOutput, len(input.MatchedOutputs))
						for oi, o := range input.MatchedOutputs {
							if o == nil {
								continue
							} else if o.Coinbase != nil {
								minimalInputs[j].MatchedOutputs[oi] = &index.MinimalMatchedOutput{
									Coinbase:          o.Coinbase.Id,
									GlobalOutputIndex: o.GlobalOutputIndex,
									Address:           o.Address,
								}
							} else if o.Sweep != nil {
								minimalInputs[j].MatchedOutputs[oi] = &index.MinimalMatchedOutput{
									Sweep:             o.Sweep.Id,
									GlobalOutputIndex: o.GlobalOutputIndex,
									Address:           o.Address,
								}
							}
						}
					}

					outputIndexes, err := client.GetDefaultClient().GetOutputIndexes(txId)
					if err != nil {
						return err
					}

					tx := &index.MainLikelySweepTransaction{
						Id:                    txId,
						Timestamp:             mainBlock.Timestamp,
						Result:                minimalInputs,
						Match:                 matches,
						Value:                 topMiner.CoinbaseAmount,
						SpendingOutputIndices: spendingOutputIndices,
						GlobalOutputIndices:   outputIndexes,
						InputCount:            len(inputs),
						InputDecoyCount:       decoyCount,
						MinerCount:            int(minerCount),
						OtherMinersCount:      int(otherMinerCount),
						NoMinerCount:          int(noMinerCount),
						MinerRatio:            float32(minerRatio),
						OtherMinersRatio:      float32(otherMinerRatio),
						NoMinerRatio:          float32(noMinerRatio),
						Address:               topMiner.Address,
					}
					if err := indexDb.InsertOrUpdateMainLikelySweepTransaction(tx); err != nil {
						return err
					}
				} else {
					utils.Logf("transaction %s is NOT likely for %s: miner ratio %.02f (%d/%d), none %.02f (%d/%d), other %.02f (%d/%d); coinbase %d, sweep %d", txId, topMiner.Address.ToBase58(), minerRatio, topMiner.Count, len(inputs), noMinerRatio, noMinerCount, len(inputs), otherMinerRatio, otherMinerCount, len(inputs), topMiner.CoinbaseCount, topMiner.SweepCount)
				}
			} else {
				//utils.Logf("transaction %s does not have enough matches, %d outputs", txId, len(inputs))
			}
		}
	}

	b.SetMetadata("processed", true)

	if err = indexDb.InsertOrUpdateMainBlock(b); err != nil {
		return err
	}

	return nil
}
