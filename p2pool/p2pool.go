package p2pool

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
)

type P2Pool struct {
	Consensus *sidechain.Consensus
}

func NewP2Pool() *P2Pool {
	pool := &P2Pool{}

	return pool
}
