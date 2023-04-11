package main

import (
	"encoding/json"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
)

type JsonBlock2 struct {
	MinerMainId    types.Hash             `json:"miner_main_id"`
	CoinbaseReward uint64                 `json:"coinbase_reward,string"`
	CoinbaseId     types.Hash             `json:"coinbase_id"`
	Version        uint64                 `json:"version,string"`
	Diff           types.Difficulty       `json:"diff"`
	Wallet         *address.Address       `json:"wallet"`
	MinerMainDiff  types.Difficulty       `json:"miner_main_diff"`
	Id             types.Hash             `json:"id"`
	Height         uint64                 `json:"height,string"`
	PowHash        types.Hash             `json:"pow_hash"`
	MainId         types.Hash             `json:"main_id"`
	MainHeight     uint64                 `json:"main_height,string"`
	Ts             uint64                 `json:"ts,string"`
	PrevId         types.Hash             `json:"prev_id"`
	CoinbasePriv   crypto.PrivateKeyBytes `json:"coinbase_priv"`
	Lts            uint64                 `json:"lts,string"`
	MainFound      string                 `json:"main_found,omitempty"`

	Uncles []struct {
		MinerMainId    types.Hash             `json:"miner_main_id"`
		CoinbaseReward uint64                 `json:"coinbase_reward,string"`
		CoinbaseId     types.Hash             `json:"coinbase_id"`
		Version        uint64                 `json:"version,string"`
		Diff           types.Difficulty       `json:"diff"`
		Wallet         *address.Address       `json:"wallet"`
		MinerMainDiff  types.Difficulty       `json:"miner_main_diff"`
		Id             types.Hash             `json:"id"`
		Height         uint64                 `json:"height,string"`
		PowHash        types.Hash             `json:"pow_hash"`
		MainId         types.Hash             `json:"main_id"`
		MainHeight     uint64                 `json:"main_height,string"`
		Ts             uint64                 `json:"ts,string"`
		PrevId         types.Hash             `json:"prev_id"`
		CoinbasePriv   crypto.PrivateKeyBytes `json:"coinbase_priv"`
		Lts            uint64                 `json:"lts,string"`
		MainFound      string                 `json:"main_found,omitempty"`
	} `json:"uncles,omitempty"`
}

type JsonBlock1 struct {
	Wallet     *address.Address       `json:"wallet"`
	Height     uint64                 `json:"height,string"`
	MHeight    uint64                 `json:"mheight,string"`
	PrevId     types.Hash             `json:"prev_id"`
	Ts         uint64                 `json:"ts,string"`
	PowHash    types.Hash             `json:"pow_hash"`
	Id         types.Hash             `json:"id"`
	PrevHash   types.Hash             `json:"prev_hash"`
	Diff       uint64                 `json:"diff,string"`
	TxCoinbase types.Hash             `json:"tx_coinbase"`
	Lts        uint64                 `json:"lts,string"`
	MHash      types.Hash             `json:"mhash"`
	TxPriv     crypto.PrivateKeyBytes `json:"tx_priv"`
	TxPub      crypto.PublicKeyBytes  `json:"tx_pub"`
	BlockFound string                 `json:"main_found,omitempty"`

	Uncles []struct {
		Diff     uint64           `json:"diff,string"`
		PrevId   types.Hash       `json:"prev_id"`
		Ts       uint64           `json:"ts,string"`
		MHeight  uint64           `json:"mheight,string"`
		PrevHash types.Hash       `json:"prev_hash"`
		Height   uint64           `json:"height,string"`
		Wallet   *address.Address `json:"wallet"`
		Id       types.Hash       `json:"id"`
	} `json:"uncles,omitempty"`
}

type versionBlock struct {
	Version uint64 `json:"version,string"`
}

func JSONFromTemplate(data []byte) (any, error) {
	var version versionBlock

	if err := json.Unmarshal(data, &version); err != nil {
		return nil, err
	} else {
		if version.Version == 2 {
			var b JsonBlock2
			if err = json.Unmarshal(data, &b); err != nil {
				return nil, err
			}
			return b, nil
		} else if version.Version == 0 || version.Version == 1 {

			var b JsonBlock1
			if err = json.Unmarshal(data, &b); err != nil {
				return nil, err
			}
			return b, nil
		} else {
			return nil, fmt.Errorf("unknown version %d", version.Version)
		}
	}
}
