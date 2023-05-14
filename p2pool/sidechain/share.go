package sidechain

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"golang.org/x/exp/constraints"
	"sync"
)

type Shares []*Share

type PreAllocatedSharesPool struct {
	pool sync.Pool
}

func NewPreAllocatedSharesPool[T constraints.Integer](n T) *PreAllocatedSharesPool {
	p := &PreAllocatedSharesPool{}
	p.pool.New = func() any {
		return PreAllocateShares(n)
	}
	return p
}

func (p *PreAllocatedSharesPool) Get() Shares {
	return p.pool.Get().(Shares)
}

func (p *PreAllocatedSharesPool) Put(s Shares) {
	p.pool.Put(s)
}

func PreAllocateShares[T constraints.Integer](n T) Shares {
	preAllocatedShares := make(Shares, n)
	for i := range preAllocatedShares {
		preAllocatedShares[i] = &Share{}
	}
	return preAllocatedShares
}

type Share struct {
	Weight  types.Difficulty
	Address address.PackedAddress
}
