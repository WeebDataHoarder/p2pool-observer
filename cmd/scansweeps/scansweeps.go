package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/cmd/utils"
	"git.gammaspectra.live/P2Pool/p2pool-observer/index"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	p2poolapi "git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/api"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"
)

func main() {
	moneroHost := flag.String("host", "127.0.0.1", "IP address of your Monero node")
	moneroRpcPort := flag.Uint("rpc-port", 18081, "monerod RPC API port number")
	startFromHeight := flag.Uint64("from", 0, "Start sweep from this main height")
	dbString := flag.String("db", "", "")
	otherLookupHost := flag.String("other-lookup-host", "", "Other observer api to lookup, for example https://p2pool.observer")
	p2poolApiHost := flag.String("api-host", "", "Host URL for p2pool go observer consensus")
	flag.Parse()

	client.SetDefaultClientSettings(fmt.Sprintf("http://%s:%d", *moneroHost, *moneroRpcPort))

	client.GetDefaultClient().SetThrottle(1000)

	p2api := p2poolapi.NewP2PoolApi(*p2poolApiHost)

	for status := p2api.Status(); !p2api.Status().Synchronized; status = p2api.Status() {
		log.Printf("[API] Not synchronized (height %d, id %s), waiting five seconds", status.Height, status.Id)
		time.Sleep(time.Second * 5)
	}

	log.Printf("[CHAIN] Consensus id = %s\n", p2api.Consensus().Id())

	indexDb, err := index.OpenIndex(*dbString, p2api.Consensus(), p2api.DifficultyByHeight, p2api.SeedByHeight, p2api.ByTemplateId)
	if err != nil {
		log.Panic(err)
	}
	defer indexDb.Close()

	startHeight := *startFromHeight

	var otherLookupHostFunc func(ctx context.Context, indices []uint64) []*index.MatchedOutput

	if *otherLookupHost != "" {
		otherLookupHostFunc = func(ctx context.Context, indices []uint64) (result []*index.MatchedOutput) {
			data, _ := json.Marshal(indices)
			uri, _ := url.Parse(*otherLookupHost + "/api/global_indices_lookup")
			if response, err := http.DefaultClient.Do(&http.Request{
				Method: "POST",
				URL:    uri,
				Body:   io.NopCloser(bytes.NewReader(data)),
			}); err == nil {
				defer response.Body.Close()
				if response.StatusCode == http.StatusOK {
					if data, err := io.ReadAll(response.Body); err == nil {
						if json.Unmarshal(data, &result) == nil && len(result) == len(indices) {
							return result
						}
					}
				}
			}
			return nil
		}
	}

	var tipHeight uint64

	if err := indexDb.Query("SELECT MIN(height), MAX(height) FROM main_blocks WHERE height > 0;", func(row index.RowScanInterface) error {
		var minHeight uint64
		if err := row.Scan(&minHeight, &tipHeight); err != nil {
			return err
		}

		if minHeight > startHeight {
			startHeight = minHeight
		}
		return nil
	}); err != nil {
		log.Panic(err)
	}

	stopHeight := tipHeight - monero.MinerRewardUnlockTime
	if tipHeight < monero.MinerRewardUnlockTime {
		stopHeight = tipHeight
	}

	log.Printf("Starting at height %d, stopping at %d", startHeight, stopHeight)

	for height := startHeight; height <= stopHeight; height++ {
		b := indexDb.GetMainBlockByHeight(height)
		if b == nil {
			log.Printf("Block at %d is nil", height)
			continue
		}
		if isProcessed, ok := b.GetMetadata("processed").(bool); ok && isProcessed {
			log.Printf("Block %s at %d (%s) has already been processed", b.Id, b.Height, time.Unix(int64(b.Timestamp), 0).UTC().Format("02-01-2006 15:04:05 MST"))
			continue
		}

		var sideTemplateId types.Hash
		var extraNonce []byte
		mainBlock, err := utils.GetMainBlock(b.Id, client.GetDefaultClient())
		if err != nil {
			log.Printf("could not get main block for %s at %d: %e", b.Id, b.Height, err)
			continue
		}

		log.Printf("Block %s at %d (%s), processing %d transactions", b.Id, b.Height, time.Unix(int64(b.Timestamp), 0).UTC().Format("02-01-2006 15:04:05 MST"), len(mainBlock.Transactions))

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
			log.Printf("p2pool tags found template id %s, extra nonce %d", sideTemplateId, binary.LittleEndian.Uint32(extraNonce))
		}

		const txInputThreshold = 4
		const txInputThresholdForRatio = 8

		if len(mainBlock.Transactions) > 0 {

			results := utils.LookupTransactions(otherLookupHostFunc, indexDb, context.Background(), txInputThreshold, mainBlock.Transactions...)

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
					if (len(inputs) >= txInputThresholdForRatio && minerRatio >= noMinerRatio) || (len(inputs) >= txInputThresholdForRatio && minerRatio > 0.35 && minerRatio > otherMinerRatio) || (len(inputs) >= txInputThreshold && minerRatio > 0.9) {
						likelyMiner = true
					}

					if likelyMiner {
						log.Printf("transaction %s is LIKELY for %s: miner ratio %.02f (%d/%d), none %.02f (%d/%d), other %.02f (%d/%d); coinbase %d, sweep %d", txId, topMiner.Address.ToBase58(), minerRatio, minerCount, len(inputs), noMinerRatio, noMinerCount, len(inputs), otherMinerRatio, otherMinerCount, len(inputs), topMiner.CoinbaseCount, topMiner.SweepCount)

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
							log.Panic(err)
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
							log.Panic(err)
						}
					} else {
						log.Printf("transaction %s is NOT likely for %s: miner ratio %.02f (%d/%d), none %.02f (%d/%d), other %.02f (%d/%d); coinbase %d, sweep %d", txId, topMiner.Address.ToBase58(), minerRatio, topMiner.Count, len(inputs), noMinerRatio, noMinerCount, len(inputs), otherMinerRatio, otherMinerCount, len(inputs), topMiner.CoinbaseCount, topMiner.SweepCount)
					}
				} else {
					//log.Printf("transaction %s does not have enough matches, %d outputs", txId, len(inputs))
				}
			}
		}

		b.SetMetadata("processed", true)

		if err = indexDb.InsertOrUpdateMainBlock(b); err != nil {
			log.Panic(err)
		}
	}

}
