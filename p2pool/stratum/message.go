package stratum

import "git.gammaspectra.live/P2Pool/p2pool-observer/types"

type JsonRpcMessage struct {
	// Id set by client
	Id any `json:"id,omitempty"`
	// JsonRpcVersion Always "2.0"
	JsonRpcVersion string `json:"jsonrpc"`
	Method         string `json:"method"`
	Params         any    `json:"params,omitempty"`
}
type JsonRpcResult struct {
	// Id set by client
	Id any `json:"id,omitempty"`
	// JsonRpcVersion Always "2.0"
	JsonRpcVersion string `json:"jsonrpc"`
	Result         any    `json:"result,omitempty"`
	Error          any    `json:"error"`
}

type JsonRpcJob struct {
	// JsonRpcVersion Always "2.0"
	JsonRpcVersion string `json:"jsonrpc"`
	// Method always "job"
	Method string           `json:"method"`
	Params jsonRpcJobParams `json:"params"`
}

type jsonRpcJobParams struct {
	// Blob HashingBlob, in hex
	Blob string `json:"blob"`

	// JobId anything?
	JobId string `json:"job_id"`

	// Target uint64 target in hex
	Target string `json:"target"`

	// Algo always "rx/0"
	Algo string `json:"algo"`

	// Height main height
	Height uint64 `json:"height"`

	// SeedHash
	SeedHash types.Hash `json:"seed_hash"`
}
type JsonRpcResponseJob struct {
	// Id set by client
	Id any `json:"id,omitempty"`
	// JsonRpcVersion Always "2.0"
	JsonRpcVersion string                   `json:"jsonrpc"`
	Result         jsonRpcResponseJobResult `json:"result"`
}

type jsonRpcResponseJobResult struct {
	Id         string           `json:"id,omitempty"`
	Job        jsonRpcJobParams `json:"job"`
	Extensions []string         `json:"extensions"`
	Status     string           `json:"status"`
}

var baseRpcJob = JsonRpcJob{
	JsonRpcVersion: "2.0",
	Method:         "job",
	Params: jsonRpcJobParams{
		Algo: "rx/0",
	},
}

var baseRpcResponseJob = JsonRpcResponseJob{
	JsonRpcVersion: "2.0",
	Result: jsonRpcResponseJobResult{
		Job: jsonRpcJobParams{
			Algo: "rx/0",
		},
		Extensions: []string{"algo"},
		Status:     "OK",
	},
}

func copyBaseJob() JsonRpcJob {
	return baseRpcJob
}

func copyBaseResponseJob() JsonRpcResponseJob {
	return baseRpcResponseJob
}
