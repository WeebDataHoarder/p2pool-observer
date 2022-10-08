package database

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"golang.org/x/exp/slices"
)

type CoinbaseTransaction struct {
	id         types.Hash
	privateKey types.Hash
	outputs    []*CoinbaseTransactionOutput
}

func NewCoinbaseTransaction(id types.Hash, privateKey types.Hash, outputs []*CoinbaseTransactionOutput) *CoinbaseTransaction {
	return &CoinbaseTransaction{
		id:         id,
		privateKey: privateKey,
		outputs:    outputs,
	}
}

func (t *CoinbaseTransaction) Outputs() []*CoinbaseTransactionOutput {
	return t.outputs
}

func (t *CoinbaseTransaction) Reward() (result uint64) {
	for _, o := range t.outputs {
		result += o.amount
	}
	return
}

func (t *CoinbaseTransaction) OutputByIndex(index uint64) *CoinbaseTransactionOutput {
	if uint64(len(t.outputs)) > index {
		return t.outputs[index]
	}
	return nil
}

func (t *CoinbaseTransaction) OutputByMiner(miner uint64) *CoinbaseTransactionOutput {
	if i := slices.IndexFunc(t.outputs, func(e *CoinbaseTransactionOutput) bool {
		return e.Miner() == miner
	}); i != -1 {
		return t.outputs[i]
	}
	return nil
}

func (t *CoinbaseTransaction) PrivateKey() types.Hash {
	return t.privateKey
}

func (t *CoinbaseTransaction) Id() types.Hash {
	return t.id
}

func (t *CoinbaseTransaction) GetEphemeralPublicKey(miner *Miner, index int64) types.Hash {
	if index != -1 {
		return miner.MoneroAddress().GetEphemeralPublicKey(t.privateKey, uint64(index))
	} else {
		return miner.MoneroAddress().GetEphemeralPublicKey(t.privateKey, t.OutputByMiner(miner.Id()).Index())
	}

}
