package p2p

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"golang.org/x/exp/slices"
	"log"
	unsafeRandom "math/rand"
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

	peerList []netip.AddrPort
	peerListLock sync.RWMutex

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
	if addressPort.Addr().IsLoopback() {
		return
	}
	s.peerListLock.Lock()
	defer s.peerListLock.Unlock()
	for _, a := range s.peerList {
		if a.Addr().Compare(addressPort.Addr()) == 0 {
			//already added
			return
		}
	}
	s.peerList = append(s.peerList, addressPort)
}

func (s *Server) PeerList() []netip.AddrPort {
	s.peerListLock.RLock()
	defer s.peerListLock.RUnlock()

	peerList := make([]netip.AddrPort, len(s.peerList))
	copy(peerList, s.peerList)
	return peerList
}

func (s *Server) RemoveFromPeerList(ip netip.Addr) {
	s.peerListLock.Lock()
	defer s.peerListLock.Unlock()
	for i, a := range s.peerList {
		if a.Addr().Compare(ip) == 0 {
			slices.Delete(s.peerList, i, i)
			return
		}
	}
}

func (s *Server) Listen() (err error) {
	if s.listener, err = net.Listen("tcp", s.listenAddress.String()); err != nil {
		return err
	} else {
		defer s.listener.Close()

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range time.Tick(time.Second * 5) {
				if s.close.Load() {
					return
				}

				for uint32(s.NumOutgoingConnections.Load()) < s.MaxOutgoingPeers {
					peerList := s.PeerList()
					if len(peerList) == 0 {
						break
					}
					randomPeer := peerList[unsafeRandom.Intn(len(peerList))]
					go func() {
						if err := s.Connect(randomPeer); err != nil {
							log.Printf("error connecting to %s: %s", randomPeer.String(), err.Error())
							s.RemoveFromPeerList(randomPeer.Addr())
						} else {
							log.Printf("connected to %s", randomPeer.String())
						}
					}()
				}

				curTime := uint64(time.Now().Unix())
				for _, c := range s.Clients() {
					if c.IsGood() && curTime >= c.NextOutgoingPeerListRequest.Load() {
						c.SendPeerListRequest()
					}
				}
			}
		}()
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

		wg.Wait()
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


	s.NumOutgoingConnections.Add(1)

	if conn, err := net.DialTimeout("tcp", addrPort.String(), time.Second * 5); err != nil {
		s.NumOutgoingConnections.Add(-1)
		return err
	} else {
		s.clientsLock.Lock()
		defer s.clientsLock.Unlock()
		client := NewClient(s, conn)
		s.clients = append(s.clients, client)
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
	go func() {
		log.Printf("[P2PServer] Banned %s for %s: %s", ip.String(), duration.String(), err.Error())
		s.RemoveFromPeerList(ip)
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
	var message, prunedMessage, compactMessage *ClientMessage
	if block != nil {
		blockData, _ := block.MarshalBinary()
		message = &ClientMessage{
			MessageId: MessageBlockBroadcast,
			Buffer: append(binary.LittleEndian.AppendUint32(make([]byte, 0, len(blockData)+4), uint32(len(blockData))), blockData...),
		}
		prunedBlockData, _ := block.MarshalBinaryFlags(true, false)
		prunedMessage = &ClientMessage{
			MessageId: MessageBlockBroadcast,
			Buffer: append(binary.LittleEndian.AppendUint32(make([]byte, 0, len(prunedBlockData)+4), uint32(len(prunedBlockData))), prunedBlockData...),
		}
		compactBlockData, _ := block.MarshalBinaryFlags(true, true)
		compactMessage = &ClientMessage{
			MessageId: MessageBlockBroadcastCompact,
			Buffer: append(binary.LittleEndian.AppendUint32(make([]byte, 0, len(compactBlockData)+4), uint32(len(compactBlockData))), compactBlockData...),
		}
	} else {
		message = &ClientMessage{
			MessageId: MessageBlockBroadcast,
			Buffer: binary.LittleEndian.AppendUint32(nil, 0),
		}
		prunedMessage, compactMessage = message, message
	}

	for _, c := range s.Clients() {
		if c.IsGood() {
			if !func() bool {
				broadcastedHashes := c.BroadcastedHashes.Slice()
				if slices.Index(broadcastedHashes, block.Side.Parent) == -1 {
					return false
				}
				for _, uncleHash := range block.Side.Uncles {
					if slices.Index(broadcastedHashes, uncleHash) == -1 {
						return false
					}
				}

				if c.VersionInformation.Protocol >= ProtocolVersion_1_1 {
					c.SendMessage(compactMessage)
				} else {
					c.SendMessage(prunedMessage)
				}
				return true
			}() {
				//fallback
				c.SendMessage(message)
			}
		}
	}
}

func (s *Server) Consensus() *sidechain.Consensus {
	return s.sidechain.Consensus()
}
