package index

import (
	"encoding/json"
	"errors"
	"git.gammaspectra.live/P2Pool/moneroutil"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"github.com/lib/pq"
)

const MainLikelySweepTransactionSelectFields = "id, timestamp, result, match, value, spending_output_indices, global_output_indices, input_count, input_decoy_count, miner_count, other_miners_count, no_miner_count, miner_ratio, other_miners_ratio, no_miner_ratio, miner_spend_public_key, miner_view_public_key"

type MainLikelySweepTransaction struct {
	// Id coinbase id
	Id                    types.Hash                          `json:"id"`
	Timestamp             uint64                              `json:"timestamp"`
	Result                MinimalTransactionInputQueryResults `json:"result"`
	Match                 []TransactionInputQueryResultsMatch `json:"match"`
	Value                 uint64                              `json:"value"`
	SpendingOutputIndices []uint64                            `json:"spending_output_indices"`
	GlobalOutputIndices   []uint64                            `json:"global_output_indices"`

	InputCount      int `json:"input_count"`
	InputDecoyCount int `json:"input_decoy_count"`

	MinerCount       int `json:"miner_count"`
	OtherMinersCount int `json:"other_miners_count"`
	NoMinerCount     int `json:"no_miner_count"`

	MinerRatio       float32 `json:"miner_ratio"`
	OtherMinersRatio float32 `json:"other_miners_ratio"`
	NoMinerRatio     float32 `json:"no_miner_ratio"`

	Address *address.Address `json:"address"`
}

func (t *MainLikelySweepTransaction) ScanFromRow(i *Index, row RowScanInterface) error {
	var spendPub, viewPub crypto.PublicKeyBytes
	var resultBuf, matchBuf []byte
	var spendingOutputIndices, globalOutputIndices pq.Int64Array
	if err := row.Scan(&t.Id, &t.Timestamp, &resultBuf, &matchBuf, &t.Value, &spendingOutputIndices, &globalOutputIndices, &t.InputCount, &t.InputDecoyCount, &t.MinerCount, &t.OtherMinersCount, &t.NoMinerCount, &t.MinerRatio, &t.OtherMinersRatio, &t.NoMinerRatio, &spendPub, &viewPub); err != nil {
		return err
	} else if err = json.Unmarshal(resultBuf, &t.Result); err != nil {
		return err
	} else if err = json.Unmarshal(resultBuf, &t.Match); err != nil {
		return err
	}
	t.SpendingOutputIndices = make([]uint64, len(spendingOutputIndices))
	for j, ix := range spendingOutputIndices {
		t.SpendingOutputIndices[j] = uint64(ix)
	}
	t.GlobalOutputIndices = make([]uint64, len(globalOutputIndices))
	for j, ix := range globalOutputIndices {
		t.GlobalOutputIndices[j] = uint64(ix)
	}
	var network uint8
	switch i.consensus.NetworkType {
	case sidechain.NetworkMainnet:
		network = moneroutil.MainNetwork
	case sidechain.NetworkTestnet:
		network = moneroutil.TestNetwork
	default:
		return errors.New("unknown network type")
	}

	t.Address = address.FromRawAddress(network, &spendPub, &viewPub)
	return nil
}
