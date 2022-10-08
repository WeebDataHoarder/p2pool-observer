package database

import "git.gammaspectra.live/P2Pool/p2pool-observer/types"

type CoinbaseTransactionOutput struct {
	id     types.Hash
	index  uint64
	amount uint64
	miner  uint64
}

func NewCoinbaseTransactionOutput(id types.Hash, index, amount, miner uint64) *CoinbaseTransactionOutput {
	return &CoinbaseTransactionOutput{
		id:     id,
		index:  index,
		amount: amount,
		miner:  miner,
	}
}

func (o *CoinbaseTransactionOutput) Miner() uint64 {
	return o.miner
}

func (o *CoinbaseTransactionOutput) Amount() uint64 {
	return o.amount
}

func (o *CoinbaseTransactionOutput) Index() uint64 {
	return o.index
}

func (o *CoinbaseTransactionOutput) Id() types.Hash {
	return o.id
}
