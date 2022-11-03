package sidechain

import "git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"

type Shares []Share

type Share struct {
	Weight  uint64
	Address address.PackedAddress
}
