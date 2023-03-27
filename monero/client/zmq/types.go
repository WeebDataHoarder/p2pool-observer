package zmq

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
)

type Topic string

const (
	TopicUnknown Topic = "unknown"

	TopicMinimalTxPoolAdd Topic = "json-minimal-txpool_add"
	TopicFullTxPoolAdd    Topic = "json-full-txpool_add"

	TopicMinimalChainMain Topic = "json-minimal-chain_main"
	TopicFullChainMain    Topic = "json-full-chain_main"

	TopicFullMinerData Topic = "json-full-miner_data"
)

type MinimalChainMain struct {
	FirstHeight uint64       `json:"first_height"`
	FirstPrevID types.Hash   `json:"first_prev_id"`
	Ids         []types.Hash `json:"ids"`
}

type FullChainMain struct {
	MajorVersion int        `json:"major_version"`
	MinorVersion int        `json:"minor_version"`
	Timestamp    int64      `json:"timestamp"`
	PrevID       types.Hash `json:"prev_id"`
	Nonce        uint64     `json:"nonce"`
	MinerTx      struct {
		Version    int   `json:"version"`
		UnlockTime int64 `json:"unlock_time"`
		Inputs     []struct {
			Gen struct {
				Height uint64 `json:"height"`
			} `json:"gen"`
		} `json:"inputs"`
		Outputs []struct {
			Amount uint64 `json:"amount"`
			ToKey  *struct {
				Key crypto.PublicKeyBytes `json:"key"`
			} `json:"to_key"`
			ToTaggedKey *struct {
				Key     crypto.PublicKeyBytes `json:"key"`
				ViewTag string                `json:"view_tag"`
			} `json:"to_tagged_key"`
		} `json:"outputs"`
		Extra      string        `json:"extra"`
		Signatures []interface{} `json:"signatures"`
		Ringct     struct {
			Type        int           `json:"type"`
			Encrypted   []interface{} `json:"encrypted"`
			Commitments []interface{} `json:"commitments"`
			Fee         uint64        `json:"fee"`
		} `json:"ringct"`
	} `json:"miner_tx"`
	TxHashes []types.Hash `json:"tx_hashes"`
}

type MinimalTxPoolAdd struct {
	ID       types.Hash `json:"id"`
	BlobSize uint64     `json:"blob_size"`
}

type FullTxPoolAdd struct {
	Version    int   `json:"version"`
	UnlockTime int64 `json:"unlock_time"`
	Inputs     []struct {
		ToKey struct {
			Amount     uint64     `json:"amount"`
			KeyOffsets []uint64   `json:"key_offsets"`
			KeyImage   types.Hash `json:"key_image"`
		} `json:"to_key"`
	} `json:"inputs"`
	Outputs []struct {
		Amount int `json:"amount"`
		ToKey  struct {
			Key crypto.PublicKeyBytes `json:"key"`
		} `json:"to_key"`
	} `json:"outputs"`
	Extra      string        `json:"extra"`
	Signatures []interface{} `json:"signatures"`
	Ringct     struct {
		Type      int `json:"type"`
		Encrypted []struct {
			Mask   string `json:"mask"`
			Amount string `json:"amount"`
		} `json:"encrypted"`
		Commitments []string `json:"commitments"`
		Fee         int      `json:"fee"`
		Prunable    struct {
			RangeProofs  []interface{} `json:"range_proofs"`
			Bulletproofs []struct {
				V      []string `json:"V"`
				AUpper string   `json:"A"`
				S      string   `json:"S"`
				T1     string   `json:"T1"`
				T2     string   `json:"T2"`
				Taux   string   `json:"taux"`
				Mu     string   `json:"mu"`
				L      []string `json:"L"`
				R      []string `json:"R"`
				ALower string   `json:"a"`
				B      string   `json:"b"`
				T      string   `json:"t"`
			} `json:"bulletproofs"`
			Mlsags     []interface{} `json:"mlsags"`
			PseudoOuts []string      `json:"pseudo_outs"`
		} `json:"prunable"`
	} `json:"ringct"`
}

type TxMempoolData struct {
	Id       types.Hash `json:"id"`
	BlobSize uint64     `json:"blob_size"`
	Weight   uint64     `json:"weight"`
	Fee      uint64     `json:"fee"`
}

type FullMinerData struct {
	MajorVersion          uint8            `json:"major_version"`
	Height                uint64           `json:"height"`
	PrevId                types.Hash       `json:"prev_id"`
	SeedHash              types.Hash       `json:"seed_hash"`
	Difficulty            types.Difficulty `json:"difficulty"`
	MedianWeight          uint64           `json:"median_weight"`
	AlreadyGeneratedCoins uint64           `json:"already_generated_coins"`
	MedianTimestamp       uint64           `json:"median_timestamp"`
	TxBacklog             []TxMempoolData  `json:"tx_backlog"`
}
