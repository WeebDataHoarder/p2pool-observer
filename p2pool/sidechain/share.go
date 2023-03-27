package sidechain

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"golang.org/x/exp/constraints"
)

type Shares []*Share

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
