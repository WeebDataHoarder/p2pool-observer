package sidechain

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
)

type Shares []*Share

type Share struct {
	Weight  types.Difficulty
	Address address.PackedAddress
}
