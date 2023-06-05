package sidechain

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"golang.org/x/exp/constraints"
	"golang.org/x/exp/slices"
	"sync"
)

type Shares []*Share

func (s Shares) Index(addr address.PackedAddress) int {
	return slices.IndexFunc(s, func(share *Share) bool {
		return share.Address.ComparePacked(addr) == 0
	})
}

func (s Shares) Clone() (o Shares) {
	o = make(Shares, len(s))
	for i := range s {
		o[i] = &Share{Address: s[i].Address, Weight: s[i].Weight}
	}
	return o
}

// Compact Removes dupe Share entries
func (s Shares) Compact() Shares {
	// Sort shares based on address
	slices.SortFunc(s, func(a *Share, b *Share) bool {
		return a.Address.ComparePacked(b.Address) < 0
	})

	index := 0
	for i, share := range s {
		if i == 0 {
			continue
		}
		if s[index].Address.ComparePacked(share.Address) == 0 {
			s[index].Weight = s[index].Weight.Add(share.Weight)
		} else {
			index++
			s[index].Address = share.Address
			s[index].Weight = share.Weight
		}
	}

	return s[:index+1]
}

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
