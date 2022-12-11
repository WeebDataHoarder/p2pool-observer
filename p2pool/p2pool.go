package p2pool

import (
	"errors"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/p2p"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"strconv"
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

	maxOutgoingPeers := uint64(10)
	if outgoingPeers, ok := settings["out-peers"]; ok {
		maxOutgoingPeers, _ = strconv.ParseUint(outgoingPeers, 10, 0)
	}

	maxIncomingPeers := uint64(450)
	if incomingPeers, ok := settings["in-peers"]; ok {
		maxIncomingPeers, _ = strconv.ParseUint(incomingPeers, 10, 0)
	}

	externalListenPort := uint64(0)
	if externalPort, ok := settings["external-port"]; ok {
		externalListenPort, _ = strconv.ParseUint(externalPort, 10, 0)
	}

	if pool.server, err = p2p.NewServer(pool.sidechain, listenAddress, uint16(externalListenPort), uint32(maxOutgoingPeers), uint32(maxIncomingPeers)); err != nil {
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
