package p2p

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"log"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"unsafe"
)

type MessageId uint8

// from p2p_server.h
const (
	MessageHandshakeChallenge MessageId = iota
	MessageHandshakeSolution
	MessageListenPort
	MessageBlockRequest
	MessageBlockResponse
	MessageBlockBroadcast
	MessagePeerListRequest
	MessagePeerListResponse
)

type Server struct {
	pool *p2pool.P2Pool

	peerId uint64

	listenAddress netip.AddrPort

	close atomic.Bool

	MaxOutgoingPeers uint32
	MaxIncomingPeers uint32

	NumOutgoingConnections atomic.Uint32
	NumIncomingConnections atomic.Uint32

	clientsLock sync.RWMutex
	clients     []*Client
}

func NewServer(p2pool *p2pool.P2Pool, listenAddress string, maxOutgoingPeers, maxIncomingPeers uint32) (*Server, error) {
	peerId := make([]byte, int(unsafe.Sizeof(uint64(0))))
	_, err := rand.Read(peerId)
	if err != nil {
		return nil, err
	}

	addrPort, err := netip.ParseAddrPort(listenAddress)
	if err != nil {
		return nil, err
	}

	s := &Server{
		pool:             p2pool,
		listenAddress:    addrPort,
		peerId:           binary.LittleEndian.Uint64(peerId),
		MaxOutgoingPeers: utils.Min(utils.Max(maxOutgoingPeers, 10), 450),
		MaxIncomingPeers: utils.Min(utils.Max(maxIncomingPeers, 10), 450),
	}

	return s, nil
}

func (s *Server) Listen() error {
	if listener, err := net.Listen("tcp", s.listenAddress.String()); err != nil {
		return err
	} else {
		defer listener.Close()
		for !s.close.Load() {
			if conn, err := listener.Accept(); err != nil {
				return err
			} else {
				if err = func() error {
					if s.NumIncomingConnections.Load() > s.MaxIncomingPeers {
						return errors.New("incoming connections limit was reached")
					}
					if addrPort, err := netip.ParseAddrPort(conn.RemoteAddr().String()); err != nil {
						return err
					} else if !addrPort.Addr().IsLoopback() {
						s.clientsLock.RLock()
						defer s.clientsLock.RUnlock()
						for _, c := range s.clients {
							if c.AddressPort.Addr().Compare(addrPort.Addr()) == 0 {
								return errors.New("peer is already connected as " + c.AddressPort.String())
							}
						}
					}

					return nil
				}(); err != nil {
					go func() {
						defer conn.Close()
						log.Printf("[P2PServer] Connection from %s rejected (%s)", conn.RemoteAddr().String(), err.Error())
					}()
					continue
				}

				func() {
					s.clientsLock.Lock()
					defer s.clientsLock.Unlock()
					client := NewClient(s, conn)
					client.IsIncomingConnection = true
					s.clients = append(s.clients, client)
					go client.OnConnection()
				}()
			}

		}
	}

	return nil
}

func (s *Server) Close() {
	s.close.Store(true)
}

func (s *Server) PeerId() uint64 {
	return s.peerId
}

func (s *Server) Pool() *p2pool.P2Pool {
	return s.pool
}
