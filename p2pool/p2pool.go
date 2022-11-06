package p2pool

import (
	"errors"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/p2p"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
)

type P2Pool struct {
	consensus *sidechain.Consensus
	sidechain *sidechain.SideChain
	server    *p2p.Server
}

func (p *P2Pool) GetBlob(key []byte) (blob []byte, err error) {
	return nil, errors.New("not found")
}

func (p *P2Pool) SetBlob(key, blob []byte) (err error) {
	return nil
}

func (p *P2Pool) RemoveBlob(key []byte) (err error) {
	return nil
}

func NewP2Pool(consensus *sidechain.Consensus, settings map[string]string) *P2Pool {
	pool := &P2Pool{
		consensus: consensus,
	}
	var err error

	listenAddress := fmt.Sprintf("0.0.0.0:%d", pool.Consensus().DefaultPort())

	pool.sidechain = sidechain.NewSideChain(pool)

	if addr, ok := settings["listen"]; ok {
		listenAddress = addr
	}

	if pool.server, err = p2p.NewServer(pool.sidechain, listenAddress, 10, 450); err != nil {
		return nil
	}

	return pool
}

func (p *P2Pool) Server() *p2p.Server {
	return p.server
}

func (p *P2Pool) Consensus() *sidechain.Consensus {
	return p.consensus
}

func (p *P2Pool) Broadcast(block *sidechain.PoolBlock) {
	p.server.Broadcast(block)
}
