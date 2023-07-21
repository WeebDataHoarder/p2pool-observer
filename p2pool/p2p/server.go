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
	"log"
	unsafeRandom "math/rand"
	"net"
	"net/netip"
	"slices"
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

type BanEntry struct {
	Expiration uint64
	Error      error
}

type Server struct {
	p2pool P2PoolInterface

	peerId             uint64
	versionInformation p2pooltypes.PeerVersionInformation

	listenAddress      netip.AddrPort
	externalListenPort uint16

	useIPv4 bool
	useIPv6 bool

	ipv6AddrsLock         sync.RWMutex
	ipv6OutgoingAddresses []netip.Addr

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
	bans     map[[16]byte]BanEntry

	clientsLock sync.RWMutex
	clients     []*Client

	cachedBlocksLock sync.RWMutex
	cachedBlocks     map[types.Hash]*sidechain.PoolBlock

	ctx context.Context
}

func NewServer(p2pool P2PoolInterface, listenAddress string, externalListenPort uint16, maxOutgoingPeers, maxIncomingPeers uint32, useIPv4, useIPv6 bool, ctx context.Context) (*Server, error) {
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
		MaxOutgoingPeers:   min(maxOutgoingPeers, 450),
		MaxIncomingPeers:   min(maxIncomingPeers, 450),
		cachedBlocks:       make(map[types.Hash]*sidechain.PoolBlock, p2pool.Consensus().ChainWindowSize*3),
		versionInformation: p2pooltypes.PeerVersionInformation{
			SoftwareId:      p2pooltypes.CurrentSoftwareId,
			SoftwareVersion: p2pooltypes.CurrentSoftwareVersion,
			Protocol:        p2pooltypes.SupportedProtocolVersion,
		},
		useIPv4: useIPv4,
		useIPv6: useIPv6,
		ctx:     ctx,
		bans:    make(map[[16]byte]BanEntry),
	}

	s.PendingOutgoingConnections = utils.NewCircularBuffer[string](int(s.MaxOutgoingPeers))
	s.RefreshOutgoingIPv6()

	return s, nil
}

func (s *Server) RefreshOutgoingIPv6() {
	log.Printf("[P2PServer] Refreshing outgoing IPv6")
	addrs, _ := utils.GetOutboundIPv6()
	s.ipv6AddrsLock.Lock()
	defer s.ipv6AddrsLock.Unlock()
	s.ipv6OutgoingAddresses = addrs
	for _, a := range addrs {
		log.Printf("[P2PServer] Outgoing IPv6: %s", a.String())
	}
}

func (s *Server) GetOutgoingIPv6() []netip.Addr {
	s.ipv6AddrsLock.RLock()
	defer s.ipv6AddrsLock.RUnlock()
	return s.ipv6OutgoingAddresses
}

func (s *Server) ListenPort() uint16 {
	return s.listenAddress.Port()
}

func (s *Server) ExternalListenPort() uint16 {
	if s.externalListenPort != 0 {
		return s.externalListenPort
	} else {
		return s.listenAddress.Port()
	}
}

func (s *Server) AddToPeerList(addressPort netip.AddrPort) {
	if addressPort.Addr().IsLoopback() {
		return
	}
	addr := addressPort.Addr().Unmap()

	if !s.useIPv4 && addr.Is4() {
		return
	} else if !s.useIPv6 && addr.Is6() {
		return
	}

	s.peerListLock.Lock()
	defer s.peerListLock.Unlock()
	if e := s.peerList.Get(addr); e == nil {
		if ok, _ := s.IsBanned(addr); ok {
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

	if !s.useIPv4 && addr.Is4() {
		return
	} else if !s.useIPv6 && addr.Is6() {
		return
	}
	s.peerListLock.Lock()
	defer s.peerListLock.Unlock()
	if e := s.peerList.Get(addr); e == nil {
		if ok, _ := s.IsBanned(addr); ok {
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
		if c.IsGood() && p != 0 && (ping == 0 || p < ping) {
			client = c
			ping = p
		}
	}

	return client
}

func (s *Server) UpdatePeerList() {
	curTime := uint64(time.Now().Unix())
	for _, c := range s.Clients() {
		if c.IsGood() && curTime >= c.NextOutgoingPeerListRequestTimestamp.Load() {
			c.SendPeerListRequest()
		}
	}
}

func (s *Server) CleanupBanList() {
	s.bansLock.Lock()
	defer s.bansLock.Unlock()

	currentTime := uint64(time.Now().Unix())

	for k, b := range s.bans {
		if currentTime >= b.Expiration {
			delete(s.bans, k)
		}
	}
}

func (s *Server) UpdateClientConnections() {

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
			log.Printf("[P2PServer] peer %s has been idle for %d seconds, disconnecting", client.AddressPort, idleTime)
			client.Close()
			continue
		}

		if client.HandshakeComplete.Load() && client.LastBroadcastTimestamp.Load() > 0 {
			// - Side chain is at least 15 minutes newer (last_updated >= client->m_lastBroadcastTimestamp + 900)
			// - It's been at least 10 seconds since side chain updated (cur_time >= last_updated + 10)
			// - It's been at least 10 seconds since the last block request (peer is not syncing)
			// - Peer should have sent a broadcast by now

			if lastUpdated > 0 && (currentTime >= max(lastUpdated, client.LastBlockRequestTimestamp.Load())+10) && (lastUpdated >= client.LastBroadcastTimestamp.Load()+900) {
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
		log.Printf("[P2PServer] Scanning monerod peers, %d left", len(s.moneroPeerList))
		for i := 0; i < 25 && len(s.moneroPeerList) > 0; i++ {
			peerList = append(peerList, s.moneroPeerList[len(s.moneroPeerList)-1])
			s.moneroPeerList = s.moneroPeerList[:len(s.moneroPeerList)-1]
		}
		N = len(peerList)
	}

	var wg sync.WaitGroup
	attempts := 0

	for i := s.NumOutgoingConnections.Load() - s.NumIncomingConnections.Load(); int(i) < N && len(peerList) > 0; {
		k := unsafeRandom.Intn(len(peerList)) % len(peerList)
		peer := peerList[k]

		if !slices.ContainsFunc(connectedPeers, func(addr netip.Addr) bool {
			return peer.AddressPort.Addr().Compare(addr) == 0
		}) {
			wg.Add(1)
			attempts++
			go func() {
				defer wg.Done()

				if s.NumOutgoingConnections.Load() >= int32(s.MaxOutgoingPeers) {
					return
				}
				if err := s.Connect(peer.AddressPort); err != nil {
					log.Printf("[P2PServer] Connection to %s rejected (%s)", peer.AddressPort.String(), err.Error())
				}
			}()
			i++
		}

		peerList = slices.Delete(peerList, k, k+1)
	}

	wg.Wait()

	if attempts == 0 && !hasGoodPeers && len(s.moneroPeerList) == 0 {
		log.Printf("[P2PServer] No connections to other p2pool nodes, check your monerod/p2pool/network/firewall setup!")
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
				if ok, _ := s.IsBanned(addr); !ok {
					if !s.useIPv4 && addr.Is4() {
						continue
					} else if !s.useIPv6 && addr.Is6() {
						continue
					}
					s.moneroPeerList = append(s.moneroPeerList, e)
				}
			}
			slices.SortFunc(s.moneroPeerList, func(a, b *PeerListEntry) int {
				aValue, bValue := a.LastSeenTimestamp.Load(), b.LastSeenTimestamp.Load()
				if aValue < bValue {
					return -1
				} else if aValue > bValue {
					return 1
				}
				return 0
			})
			log.Printf("[P2PServer] monerod peer list loaded (%d peers)", len(s.moneroPeerList))
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

	for {
		obtained := false
		for _, h := range s.SideChain().GetMissingBlocks() {
			if b, ok := s.cachedBlocks[h]; ok {
				if _, err, _ := s.SideChain().AddPoolBlockExternal(b); err == nil {
					obtained = true
					continue
				}
			}

			clientList[unsafeRandom.Intn(len(clientList))].SendUniqueBlockRequest(h)
		}
		if !obtained {
			break
		}
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
			for range utils.ContextTick(s.ctx, time.Second*5) {
				s.UpdatePeerList()
				s.UpdateClientConnections()
			}
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range utils.ContextTick(s.ctx, time.Second) {
				if s.SideChain().PreCalcFinished() {
					s.ClearCachedBlocks()
					break
				}

				s.DownloadMissingBlocks()
			}
			// Slow down updates for missing blocks after sync
			for range utils.ContextTick(s.ctx, time.Minute*2) {
				s.DownloadMissingBlocks()
			}
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range utils.ContextTick(s.ctx, time.Hour) {
				s.RefreshOutgoingIPv6()
			}
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range utils.ContextTick(s.ctx, time.Minute*5) {
				s.CleanupBanList()
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

						addr := addrPort.Addr().Unmap()

						if !s.useIPv4 && addr.Is4() {
							return errors.New("peer is IPv4 but we do not allow it")
						} else if !s.useIPv6 && addr.Is6() {
							return errors.New("peer is IPv6 but we do not allow it")
						}

						if ok, b := s.IsBanned(addr); ok {
							return fmt.Errorf("peer is banned: %w", b.Error)
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
					log.Printf("[P2PServer] Incoming connection from %s", conn.RemoteAddr().String())

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

func (s *Server) GetAddressConnectedPrefix(prefix netip.Prefix) (result []*Client) {
	s.clientsLock.RLock()
	defer s.clientsLock.RUnlock()
	for _, c := range s.clients {
		if prefix.Contains(c.AddressPort.Addr()) {
			result = append(result, c)
		}
	}
	return result
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
	addr := addrPort.Addr().Unmap()
	if !s.useIPv4 && addr.Is4() {
		return nil, errors.New("peer is IPv4 but we do not allow it")
	} else if !s.useIPv6 && addr.Is6() {
		return nil, errors.New("peer is IPv6 but we do not allow it")
	}

	if clients := s.GetAddressConnected(addrPort.Addr()); !addrPort.Addr().IsLoopback() && len(clients) != 0 {
		return nil, errors.New("peer is already connected as " + clients[0].AddressPort.String())
	}

	if !s.PendingOutgoingConnections.PushUnique(addrPort.Addr().String()) {
		return nil, errors.New("peer is already attempting connection")
	}

	s.NumOutgoingConnections.Add(1)

	var localAddr net.Addr

	//select IPv6 outgoing address
	if addr.Is6() {
		addrs := s.GetOutgoingIPv6()
		if len(addrs) > 1 {
			a := addrs[unsafeRandom.Intn(len(addrs))]
			localAddr = &net.TCPAddr{IP: a.AsSlice(), Zone: a.Zone()}
		} else if len(addrs) == 1 {
			localAddr = &net.TCPAddr{IP: addrs[0].AsSlice(), Zone: addrs[0].Zone()}
		}
	}

	if localAddr != nil {
		log.Printf("[P2PServer] Outgoing connection to %s using %s", addrPort.String(), localAddr.String())
	} else {
		log.Printf("[P2PServer] Outgoing connection to %s", addrPort.String())
	}

	if conn, err := (&net.Dialer{Timeout: time.Second * 5, LocalAddr: localAddr}).DialContext(s.ctx, "tcp", addrPort.String()); err != nil {
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
	if ok, b := s.IsBanned(addrPort.Addr()); ok {
		return fmt.Errorf("peer is banned: %w", b.Error)
	}

	_, err := s.DirectConnect(addrPort)
	return err
}

func (s *Server) Clients() []*Client {
	s.clientsLock.RLock()
	defer s.clientsLock.RUnlock()
	return slices.Clone(s.clients)
}

func (s *Server) IsBanned(ip netip.Addr) (bool, *BanEntry) {
	if ip.IsLoopback() {
		return false, nil
	}
	ip = ip.Unmap()
	var prefix netip.Prefix
	if ip.Is6() {
		//ban the /64
		prefix, _ = ip.Prefix(64)
	} else if ip.Is4() {
		//ban only a single ip, /32
		prefix, _ = ip.Prefix(32)
	}

	if !prefix.IsValid() {
		return false, nil
	}

	k := prefix.Addr().As16()

	if b, ok := func() (entry BanEntry, ok bool) {
		s.bansLock.RLock()
		defer s.bansLock.RUnlock()
		entry, ok = s.bans[k]
		return entry, ok
	}(); ok == false {
		return false, nil
	} else if uint64(time.Now().Unix()) >= b.Expiration {
		return false, nil
	} else {
		return true, &b
	}
}

func (s *Server) Ban(ip netip.Addr, duration time.Duration, err error) {
	if ok, _ := s.IsBanned(ip); ok {
		return
	}

	log.Printf("[P2PServer] Banned %s for %s: %s", ip.String(), duration.String(), err.Error())
	if !ip.IsLoopback() {
		ip = ip.Unmap()
		var prefix netip.Prefix
		if ip.Is6() {
			//ban the /64
			prefix, _ = ip.Prefix(64)
		} else if ip.Is4() {
			//ban only a single ip, /32
			prefix, _ = ip.Prefix(32)
		}

		if prefix.IsValid() {
			func() {
				s.bansLock.Lock()
				defer s.bansLock.Unlock()
				s.bans[prefix.Addr().As16()] = BanEntry{
					Error:      err,
					Expiration: uint64(time.Now().Unix()) + uint64(duration.Seconds()),
				}
			}()
			for _, c := range s.GetAddressConnectedPrefix(prefix) {
				c.Close()
			}
		}
	}

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

func (s *Server) Broadcast(block *sidechain.PoolBlock) {
	var message, prunedMessage, compactMessage *ClientMessage
	if block != nil {
		blockData, err := block.AppendBinaryFlags(make([]byte, 0, block.BufferLength()), false, false)
		if err != nil {
			log.Panicf("[P2PServer] Tried to broadcast block %s at height %d but received error: %s", block.SideTemplateId(s.Consensus()), block.Side.Height, err)
			return
		}
		message = &ClientMessage{
			MessageId: MessageBlockBroadcast,
			Buffer:    append(binary.LittleEndian.AppendUint32(make([]byte, 0, len(blockData)+4), uint32(len(blockData))), blockData...),
		}
		prunedBlockData, _ := block.AppendBinaryFlags(make([]byte, 0, block.BufferLength()), true, false)
		prunedMessage = &ClientMessage{
			MessageId: MessageBlockBroadcast,
			Buffer:    append(binary.LittleEndian.AppendUint32(make([]byte, 0, len(prunedBlockData)+4), uint32(len(prunedBlockData))), prunedBlockData...),
		}
		compactBlockData, _ := block.AppendBinaryFlags(make([]byte, 0, block.BufferLength()), true, true)
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

	blockTemplateId := block.SideTemplateId(s.Consensus())

	go func() {
		for _, c := range s.Clients() {
			if c.IsGood() {
				if !func() (sent bool) {
					broadcastedHashes := c.BroadcastedHashes.Slice()

					// has peer not broadcasted block parent to us?
					if slices.Index(broadcastedHashes, block.Side.Parent) == -1 {
						return false
					}
					// has peer not broadcasted block uncles to us?
					for _, uncleHash := range block.Side.Uncles {
						if slices.Index(broadcastedHashes, uncleHash) == -1 {
							return false
						}
					}

					// has peer broadcasted this block to us?
					if slices.Index(broadcastedHashes, blockTemplateId) != -1 &&
						c.VersionInformation.SupportsFeature(p2pooltypes.FeatureBlockNotify) {
						c.SendBlockNotify(blockTemplateId)
						return true
					}

					if c.VersionInformation.SupportsFeature(p2pooltypes.FeatureCompactBroadcast) {
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
