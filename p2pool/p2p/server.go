package p2p

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"golang.org/x/exp/slices"
	"log"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)


type Server struct {
	sidechain *sidechain.SideChain

	peerId uint64
	versionInformation PeerVersionInformation

	listenAddress netip.AddrPort

	close atomic.Bool
	listener net.Listener

	MaxOutgoingPeers uint32
	MaxIncomingPeers uint32

	NumOutgoingConnections atomic.Int32
	NumIncomingConnections atomic.Int32

	clientsLock sync.RWMutex
	clients     []*Client
}

func NewServer(sidechain *sidechain.SideChain, listenAddress string, maxOutgoingPeers, maxIncomingPeers uint32) (*Server, error) {
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
		sidechain:        sidechain,
		listenAddress:    addrPort,
		peerId:           binary.LittleEndian.Uint64(peerId),
		MaxOutgoingPeers: utils.Min(utils.Max(maxOutgoingPeers, 10), 450),
		MaxIncomingPeers: utils.Min(utils.Max(maxIncomingPeers, 10), 450),
		versionInformation: PeerVersionInformation{Code: ImplementationCodeGoObserver, Version: CurrentImplementationVersion, Protocol: SupportedProtocolVersion},
	}

	return s, nil
}

func (s *Server) AddToPeerList(addressPort netip.AddrPort) {
	log.Printf("TODO AddToPeerList %s", addressPort.String())

	if uint32(s.NumOutgoingConnections.Load()) < s.MaxOutgoingPeers {
		go func() {
			if err := s.Connect(addressPort); err != nil {
				log.Printf("error connecting to %s: %s", addressPort.String(), err.Error())
			}
		}()
	}
}

func (s *Server) Listen() (err error) {
	if s.listener, err = net.Listen("tcp", s.listenAddress.String()); err != nil {
		return err
	} else {
		defer s.listener.Close()
		for !s.close.Load() {
			if conn, err := s.listener.Accept(); err != nil {
				return err
			} else {
				if err = func() error {
					if uint32(s.NumIncomingConnections.Load()) > s.MaxIncomingPeers {
						return errors.New("incoming connections limit was reached")
					}
					if addrPort, err := netip.ParseAddrPort(conn.RemoteAddr().String()); err != nil {
						return err
					} else if !addrPort.Addr().IsLoopback() {
						if clients := s.GetAddressConnected(addrPort.Addr()); !addrPort.Addr().IsLoopback() && len(clients) != 0 {
							return errors.New("peer is already connected as " + clients[0].AddressPort.String())
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
					s.NumIncomingConnections.Add(1)
					go client.OnConnection()
				}()
			}

		}
	}

	return nil
}

func (s *Server) GetAddressConnected(addr netip.Addr) (result []*Client) {
	s.clientsLock.RLock()
	defer s.clientsLock.RUnlock()
	for _, c := range s.clients {
		if c.AddressPort.Addr().Compare(addr) == 0 {
			result = append(result, c)
		}
	}
	return result
}

func (s *Server) Connect(addrPort netip.AddrPort) error {
	if clients := s.GetAddressConnected(addrPort.Addr()); !addrPort.Addr().IsLoopback() && len(clients) != 0 {
		return errors.New("peer is already connected as " + clients[0].AddressPort.String())
	}

	if conn, err := net.DialTimeout("tcp", addrPort.String(), time.Second * 10); err != nil {
		return err
	} else {
		s.clientsLock.Lock()
		defer s.clientsLock.Unlock()
		client := NewClient(s, conn)
		s.clients = append(s.clients, client)
		s.NumOutgoingConnections.Add(1)
		go client.OnConnection()
		return nil
	}
}

func (s *Server) Clients() []*Client {
	s.clientsLock.RLock()
	defer s.clientsLock.RUnlock()
	return slices.Clone(s.clients)
}

func (s *Server) Ban(ip netip.Addr, duration time.Duration, err error) {
	//TODO: banlist
	go func() {
		log.Printf("[P2PServer] Banned %s for %s: %s", ip.String(), duration.String(), err.Error())
		if !ip.IsLoopback() {
			for _, c := range s.GetAddressConnected(ip) {
				c.Close()
			}
		}
	}()
}

func (s *Server) Close() {
	if !s.close.Swap(true) && s.listener != nil {
		s.clientsLock.Lock()
		defer s.clientsLock.Unlock()
		for _, c := range s.clients {
			c.Connection.Close()
		}
		s.listener.Close()
	}
}

func (s *Server) VersionInformation() *PeerVersionInformation {
	return &s.versionInformation
}

func (s *Server) PeerId() uint64 {
	return s.peerId
}

func (s *Server) SideChain() *sidechain.SideChain {
	return s.sidechain
}

func (s *Server) updateClients() {

}

func (s *Server) Broadcast(block *sidechain.PoolBlock) {
	var message *ClientMessage
	if block != nil {
		blockData, _ := block.MarshalBinary()
		message = &ClientMessage{
			MessageId: MessageBlockBroadcast,
			Buffer: append(binary.LittleEndian.AppendUint32(make([]byte, 0, len(blockData)+4), uint32(len(blockData))), blockData...),
		}
	} else {
		message = &ClientMessage{
			MessageId: MessageBlockBroadcast,
			Buffer: binary.LittleEndian.AppendUint32(nil, 0),
		}
	}

	for _, c := range s.Clients() {
		if c.HandshakeComplete.Load() {
			c.SendMessage(message)
		}
	}
}

func (s *Server) Consensus() *sidechain.Consensus {
	return s.sidechain.Consensus()
}
