package p2p

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/mainchain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	p2pooltypes "git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
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

type P2PoolInterface interface {
	sidechain.ConsensusProvider
	SideChain() *sidechain.SideChain
	MainChain() *mainchain.MainChain
	GetChainMainTip() *sidechain.ChainMain
	GetMinerDataTip() *p2pooltypes.MinerData
	ClientRPC() *client.Client
}

type PeerListEntry struct {
	AddressPort       netip.AddrPort
	FailedConnections atomic.Uint32
	LastSeenTimestamp atomic.Uint64
}

type PeerList []*PeerListEntry

func (l PeerList) Get(addr netip.Addr) *PeerListEntry {
	if i := slices.IndexFunc(l, func(entry *PeerListEntry) bool {
		return entry.AddressPort.Addr().Compare(addr) == 0
	}); i != -1 {
		return l[i]
	}
	return nil
}
func (l PeerList) Delete(addr netip.Addr) PeerList {
	ret := l
	for i := slices.IndexFunc(ret, func(entry *PeerListEntry) bool {
		return entry.AddressPort.Addr().Compare(addr) == 0
	}); i != -1; i = slices.IndexFunc(ret, func(entry *PeerListEntry) bool {
		return entry.AddressPort.Addr().Compare(addr) == 0
	}) {
		ret = slices.Delete(ret, i, i+1)
	}
	return ret
}

type Server struct {
	p2pool P2PoolInterface

	peerId             uint64
	versionInformation p2pooltypes.PeerVersionInformation

	listenAddress      netip.AddrPort
	externalListenPort uint16

	close    atomic.Bool
	listener *net.TCPListener

	fastestPeer *Client

	MaxOutgoingPeers uint32
	MaxIncomingPeers uint32

	NumOutgoingConnections atomic.Int32
	NumIncomingConnections atomic.Int32

	PendingOutgoingConnections *utils.CircularBuffer[string]

	peerList       PeerList
	peerListLock   sync.RWMutex
	moneroPeerList PeerList

	bansLock sync.RWMutex
	bans     map[[16]byte]uint64

	clientsLock sync.RWMutex
	clients     []*Client

	cachedBlocksLock sync.RWMutex
	cachedBlocks     map[types.Hash]*sidechain.PoolBlock

	ctx context.Context
}

func NewServer(p2pool P2PoolInterface, listenAddress string, externalListenPort uint16, maxOutgoingPeers, maxIncomingPeers uint32, ctx context.Context) (*Server, error) {
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
		p2pool:             p2pool,
		listenAddress:      addrPort,
		externalListenPort: externalListenPort,
		peerId:             binary.LittleEndian.Uint64(peerId),
		MaxOutgoingPeers:   utils.Min(utils.Max(maxOutgoingPeers, 10), 450),
		MaxIncomingPeers:   utils.Min(utils.Max(maxIncomingPeers, 10), 450),
		cachedBlocks:       make(map[types.Hash]*sidechain.PoolBlock, p2pool.Consensus().ChainWindowSize*3),
		versionInformation: p2pooltypes.PeerVersionInformation{
			SoftwareId:      p2pooltypes.SoftwareIdGoObserver,
			SoftwareVersion: p2pooltypes.CurrentSoftwareVersion,
			Protocol:        p2pooltypes.SupportedProtocolVersion,
		},
		ctx:  ctx,
		bans: make(map[[16]byte]uint64),
	}

	s.PendingOutgoingConnections = utils.NewCircularBuffer[string](int(s.MaxOutgoingPeers))

	return s, nil
}

func (s *Server) AddToPeerList(addressPort netip.AddrPort) {
	if addressPort.Addr().IsLoopback() {
		return
	}
	addr := addressPort.Addr().Unmap()
	s.peerListLock.Lock()
	defer s.peerListLock.Unlock()
	if e := s.peerList.Get(addr); e == nil {
		if s.IsBanned(addr) {
			return
		}
		e = &PeerListEntry{
			AddressPort: netip.AddrPortFrom(addr, addressPort.Port()),
		}
		e.LastSeenTimestamp.Store(uint64(time.Now().Unix()))
		s.peerList = append(s.peerList, e)
	} else {
		e.LastSeenTimestamp.Store(uint64(time.Now().Unix()))
	}
}

func (s *Server) UpdateInPeerList(addressPort netip.AddrPort) {
	if addressPort.Addr().IsLoopback() {
		return
	}
	addr := addressPort.Addr().Unmap()
	s.peerListLock.Lock()
	defer s.peerListLock.Unlock()
	if e := s.peerList.Get(addr); e == nil {
		if s.IsBanned(addr) {
			return
		}
		e = &PeerListEntry{
			AddressPort: netip.AddrPortFrom(addr, addressPort.Port()),
		}
		e.LastSeenTimestamp.Store(uint64(time.Now().Unix()))
		s.peerList = append(s.peerList, e)
	} else {
		e.FailedConnections.Store(0)
		e.LastSeenTimestamp.Store(uint64(time.Now().Unix()))
	}
}

func (s *Server) ListenPort() uint16 {
	return s.externalListenPort
}

func (s *Server) PeerList() PeerList {
	s.peerListLock.RLock()
	defer s.peerListLock.RUnlock()

	return slices.Clone(s.peerList)
}

func (s *Server) RemoveFromPeerList(ip netip.Addr) {
	ip = ip.Unmap()
	s.peerListLock.Lock()
	defer s.peerListLock.Unlock()
	for i, a := range s.peerList {
		if a.AddressPort.Addr().Compare(ip) == 0 {
			s.peerList = slices.Delete(s.peerList, i, i+1)
			return
		}
	}
}

func (s *Server) GetFastestClient() *Client {
	var client *Client
	var ping uint64
	for _, c := range s.Clients() {
		p := c.PingDuration.Load()
		if p != 0 && (ping == 0 || p < ping) {
			client = c
			ping = p
		}
	}

	return client
}

func (s *Server) updatePeerList() {
	curTime := uint64(time.Now().Unix())
	for _, c := range s.Clients() {
		if c.IsGood() && curTime >= c.NextOutgoingPeerListRequestTimestamp.Load() {
			c.SendPeerListRequest()
		}
	}
}

func (s *Server) updateClientConnections() {

	//TODO: remove peers from peerlist not seen in the last hour

	currentTime := uint64(time.Now().Unix())
	lastUpdated := s.SideChain().LastUpdated()

	var hasGoodPeers bool
	var fastestPeer *Client

	connectedClients := s.Clients()

	connectedPeers := make([]netip.Addr, 0, len(connectedClients))

	for _, client := range connectedClients {
		timeout := uint64(10)
		if client.HandshakeComplete.Load() {
			timeout = 300
		}

		lastAlive := client.LastActiveTimestamp.Load()

		if currentTime >= (lastAlive + timeout) {
			idleTime := currentTime - lastAlive
			log.Printf("peer %s has been idle for %d seconds, disconnecting", client.AddressPort, idleTime)
			client.Close()
			continue
		}

		if client.HandshakeComplete.Load() && client.LastBroadcastTimestamp.Load() > 0 {
			// - Side chain is at least 15 minutes newer (last_updated >= client->m_lastBroadcastTimestamp + 900)
			// - It's been at least 10 seconds since side chain updated (cur_time >= last_updated + 10)
			// - It's been at least 10 seconds since the last block request (peer is not syncing)
			// - Peer should have sent a broadcast by now

			if lastUpdated > 0 && (currentTime >= utils.Max(lastUpdated, client.LastBlockRequestTimestamp.Load())+10) && (lastUpdated >= client.LastBroadcastTimestamp.Load()+900) {
				dt := lastUpdated - client.LastBroadcastTimestamp.Load()
				client.Ban(DefaultBanTime, fmt.Errorf("not broadcasting blocks (last update %d seconds ago)", dt))
				client.Close()
				continue
			}
		}

		connectedPeers = append(connectedPeers, client.AddressPort.Addr().Unmap())
		if client.IsGood() {
			hasGoodPeers = true
			if client.PingDuration.Load() >= 0 && (fastestPeer == nil || fastestPeer.PingDuration.Load() > client.PingDuration.Load()) {
				fastestPeer = client
			}
		}
	}

	deletedPeers := 0
	peerList := s.PeerList()
	for _, p := range peerList {
		if slices.ContainsFunc(connectedPeers, func(addr netip.Addr) bool {
			return p.AddressPort.Addr().Compare(addr) == 0
		}) {
			p.LastSeenTimestamp.Store(currentTime)
		}
		if (p.LastSeenTimestamp.Load() + 3600) < currentTime {
			peerList = peerList.Delete(p.AddressPort.Addr())
			deletedPeers++
		}
	}
	if deletedPeers > 0 {
		func() {
			s.peerListLock.Lock()
			defer s.peerListLock.Unlock()
			s.peerList = peerList
		}()
	}

	N := int(s.MaxOutgoingPeers)

	// Special case: when we can't find p2pool peers, scan through monerod peers (try 25 peers at a time)
	if !hasGoodPeers && len(s.moneroPeerList) > 0 {
		log.Printf("Scanning monerod peers, %d left", len(s.moneroPeerList))
		for i := 0; i < 25 && len(s.moneroPeerList) > 0; i++ {
			peerList = append(peerList, s.moneroPeerList[len(s.moneroPeerList)-1])
			s.moneroPeerList = s.moneroPeerList[:len(s.moneroPeerList)-1]
		}
		N = len(peerList)
	}

	for i := s.NumOutgoingConnections.Load() - s.NumIncomingConnections.Load(); int(i) < N && len(peerList) > 0; {
		k := unsafeRandom.Intn(len(peerList)) % len(peerList)
		peer := peerList[k]

		if !slices.ContainsFunc(connectedPeers, func(addr netip.Addr) bool {
			return peer.AddressPort.Addr().Compare(addr) == 0
		}) {
			go func() {
				if err := s.Connect(peer.AddressPort); err != nil {
					log.Printf("error connecting to %s: %s", peer.AddressPort.String(), err.Error())
				} else {
					log.Printf("connected to %s", peer.AddressPort.String())
				}
			}()
			i++
		}

		peerList = slices.Delete(peerList, k, k+1)
	}

	if !hasGoodPeers && len(s.moneroPeerList) == 0 {
		log.Printf("no connections to other p2pool nodes, check your monerod/p2pool/network/firewall setup!")
		if moneroPeerList, err := s.p2pool.ClientRPC().GetPeerList(); err == nil {
			s.moneroPeerList = make(PeerList, 0, len(moneroPeerList.WhiteList))
			for _, p := range moneroPeerList.WhiteList {
				addr, err := netip.ParseAddr(p.Host)
				if err != nil {
					continue
				}
				e := &PeerListEntry{
					AddressPort: netip.AddrPortFrom(addr, s.Consensus().DefaultPort()),
				}
				e.LastSeenTimestamp.Store(uint64(p.LastSeen))
				if !s.IsBanned(addr) {
					s.moneroPeerList = append(s.moneroPeerList, e)
				}
			}
			slices.SortFunc(s.moneroPeerList, func(a, b *PeerListEntry) bool {
				return a.LastSeenTimestamp.Load() < b.LastSeenTimestamp.Load()
			})
			log.Printf("monerod peer list loaded (%d peers)", len(s.moneroPeerList))
		}
	}
}

func (s *Server) AddCachedBlock(block *sidechain.PoolBlock) {
	s.cachedBlocksLock.Lock()
	defer s.cachedBlocksLock.Unlock()

	if s.cachedBlocks == nil {
		return
	}

	s.cachedBlocks[block.SideTemplateId(s.p2pool.Consensus())] = block
}

func (s *Server) ClearCachedBlocks() {
	s.cachedBlocksLock.Lock()
	defer s.cachedBlocksLock.Unlock()

	s.cachedBlocks = nil
}

func (s *Server) GetCachedBlock(hash types.Hash) *sidechain.PoolBlock {
	s.cachedBlocksLock.RLock()
	defer s.cachedBlocksLock.RUnlock()

	return s.cachedBlocks[hash]
}

func (s *Server) DownloadMissingBlocks() {
	clientList := s.Clients()

	if len(clientList) == 0 {
		return
	}

	s.cachedBlocksLock.RLock()
	defer s.cachedBlocksLock.RUnlock()

	for _, h := range s.SideChain().GetMissingBlocks() {
		if b, ok := s.cachedBlocks[h]; ok {
			if _, err := s.SideChain().AddPoolBlockExternal(b); err == nil {
				continue
			}
		}

		clientList[unsafeRandom.Intn(len(clientList))].SendUniqueBlockRequest(h)
	}
}

func (s *Server) Listen() (err error) {
	var listener net.Listener
	var ok bool
	if listener, err = (&net.ListenConfig{}).Listen(s.ctx, "tcp", s.listenAddress.String()); err != nil {
		return err
	} else if s.listener, ok = listener.(*net.TCPListener); !ok {
		return errors.New("not a tcp listener")
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

				s.updatePeerList()
				s.updateClientConnections()
			}
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range time.Tick(time.Second) {
				if s.close.Load() {
					return
				}

				if s.SideChain().PreCalcFinished() {
					s.ClearCachedBlocks()
				}

				s.DownloadMissingBlocks()
			}
		}()
		for !s.close.Load() {
			if conn, err := s.listener.AcceptTCP(); err != nil {
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
					if s.IsBanned(netip.MustParseAddrPort(conn.RemoteAddr().String()).Addr()) {
						log.Printf("[P2PServer] Connection from %s rejected (banned)", conn.RemoteAddr().String())
						return
					}

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

func (s *Server) DirectConnect(addrPort netip.AddrPort) (*Client, error) {
	if clients := s.GetAddressConnected(addrPort.Addr()); !addrPort.Addr().IsLoopback() && len(clients) != 0 {
		return nil, errors.New("peer is already connected as " + clients[0].AddressPort.String())
	}

	if !s.PendingOutgoingConnections.PushUnique(addrPort.Addr().String()) {
		return nil, errors.New("peer is already attempting connection")
	}

	s.NumOutgoingConnections.Add(1)

	if conn, err := (&net.Dialer{Timeout: time.Second * 5}).DialContext(s.ctx, "tcp", addrPort.String()); err != nil {
		s.NumOutgoingConnections.Add(-1)
		s.PendingOutgoingConnections.Replace(addrPort.Addr().String(), "")
		if p := s.PeerList().Get(addrPort.Addr()); p != nil {
			if p.FailedConnections.Add(1) >= 10 {
				s.RemoveFromPeerList(addrPort.Addr())
			}
		}
		return nil, err
	} else if tcpConn, ok := conn.(*net.TCPConn); !ok {
		s.NumOutgoingConnections.Add(-1)
		s.PendingOutgoingConnections.Replace(addrPort.Addr().String(), "")
		if p := s.PeerList().Get(addrPort.Addr()); p != nil {
			if p.FailedConnections.Add(1) >= 10 {
				s.RemoveFromPeerList(addrPort.Addr())
			}
		}
		return nil, errors.New("not a tcp connection")
	} else {
		s.clientsLock.Lock()
		defer s.clientsLock.Unlock()
		client := NewClient(s, tcpConn)
		s.clients = append(s.clients, client)
		go client.OnConnection()
		return client, nil
	}
}

func (s *Server) Connect(addrPort netip.AddrPort) error {
	if s.IsBanned(addrPort.Addr()) {
		return fmt.Errorf("peer is banned")
	}

	_, err := s.DirectConnect(addrPort)
	return err
}

func (s *Server) Clients() []*Client {
	s.clientsLock.RLock()
	defer s.clientsLock.RUnlock()
	return slices.Clone(s.clients)
}

func (s *Server) IsBanned(ip netip.Addr) bool {
	if ip.IsLoopback() {
		return false
	}
	s.bansLock.RLock()
	defer s.bansLock.RUnlock()
	k := ip.Unmap().As16()
	if t, ok := s.bans[k]; ok == false {
		return false
	} else if uint64(time.Now().Unix()) >= t {
		go func() {
			//HACK: delay via goroutine
			s.bansLock.Lock()
			defer s.bansLock.Unlock()
			delete(s.bans, k)
		}()
		return false
	}
	return true
}

func (s *Server) Ban(ip netip.Addr, duration time.Duration, err error) {
	go func() {
		if !ip.IsLoopback() {
			s.bansLock.Lock()
			defer s.bansLock.Unlock()
			s.bans[ip.Unmap().As16()] = uint64(time.Now().Unix()) + uint64(duration.Seconds())
			log.Printf("[P2PServer] Banned %s for %s: %s", ip.String(), duration.String(), err.Error())
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

func (s *Server) VersionInformation() *p2pooltypes.PeerVersionInformation {
	return &s.versionInformation
}

func (s *Server) PeerId() uint64 {
	return s.peerId
}

func (s *Server) SideChain() *sidechain.SideChain {
	return s.p2pool.SideChain()
}

func (s *Server) MainChain() *mainchain.MainChain {
	return s.p2pool.MainChain()
}

func (s *Server) updateClients() {

}

func (s *Server) Broadcast(block *sidechain.PoolBlock) {
	var message, prunedMessage, compactMessage *ClientMessage
	if block != nil {
		blockData, _ := block.MarshalBinary()
		message = &ClientMessage{
			MessageId: MessageBlockBroadcast,
			Buffer:    append(binary.LittleEndian.AppendUint32(make([]byte, 0, len(blockData)+4), uint32(len(blockData))), blockData...),
		}
		prunedBlockData, _ := block.MarshalBinaryFlags(true, false)
		prunedMessage = &ClientMessage{
			MessageId: MessageBlockBroadcast,
			Buffer:    append(binary.LittleEndian.AppendUint32(make([]byte, 0, len(prunedBlockData)+4), uint32(len(prunedBlockData))), prunedBlockData...),
		}
		compactBlockData, _ := block.MarshalBinaryFlags(true, true)
		if len(compactBlockData) >= len(prunedBlockData) {
			//do not send compact if it ends up larger due to some reason, like parent missing or mismatch in transactions
			compactMessage = prunedMessage
		} else {
			compactMessage = &ClientMessage{
				MessageId: MessageBlockBroadcastCompact,
				Buffer:    append(binary.LittleEndian.AppendUint32(make([]byte, 0, len(compactBlockData)+4), uint32(len(compactBlockData))), compactBlockData...),
			}
		}
	} else {
		message = &ClientMessage{
			MessageId: MessageBlockBroadcast,
			Buffer:    binary.LittleEndian.AppendUint32(nil, 0),
		}
		prunedMessage, compactMessage = message, message
	}

	go func() {
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

					if c.VersionInformation.Protocol >= p2pooltypes.ProtocolVersion_1_1 {
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
	}()
}

func (s *Server) Consensus() *sidechain.Consensus {
	return s.p2pool.Consensus()
}
