package p2p

import (
	"bufio"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	p2pooltypes "git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"io"
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

const DefaultBanTime = time.Second * 600
const PeerListResponseMaxPeers = 16
const PeerRequestDelay = 60

const MaxBufferSize = 128 * 1024

var smallBufferPool = sync.Pool{
	New: func() any {
		return make([]byte, 16384)
	},
}

func getBuffer(length int) []byte {
	if length <= 16384 {
		return smallBufferPool.Get().([]byte)
	}
	return make([]byte, length)
}

func returnBuffer(x []byte) {
	if len(x) <= 16384 {
		smallBufferPool.Put(x)
	}
}

type Client struct {
	// Peer general static-ish information
	PeerId             atomic.Uint64
	VersionInformation p2pooltypes.PeerVersionInformation
	ListenPort         atomic.Uint32
	ConnectionTime     time.Time
	AddressPort        netip.AddrPort

	// Peer general dynamic-ish information
	BroadcastMaxHeight atomic.Uint64
	PingDuration       atomic.Uint64

	// Internal values
	Owner                                *Server
	Connection                           *net.TCPConn
	banErrorLock                         sync.Mutex
	banError                             error
	LastBroadcastTimestamp               atomic.Uint64
	LastBlockRequestTimestamp            atomic.Uint64
	LastIncomingPeerListRequestTime      time.Time
	LastActiveTimestamp                  atomic.Uint64
	LastPeerListRequestTimestamp         atomic.Uint64
	NextOutgoingPeerListRequestTimestamp atomic.Uint64

	expectedMessage      MessageId
	IsIncomingConnection bool

	Closed atomic.Bool
	//State properties
	HandshakeComplete     atomic.Bool
	SentHandshakeSolution atomic.Bool

	LastKnownTip atomic.Pointer[sidechain.PoolBlock]

	BroadcastedHashes *utils.CircularBuffer[types.Hash]
	RequestedHashes   *utils.CircularBuffer[types.Hash]

	blockPendingRequests chan types.Hash

	handshakeChallenge HandshakeChallenge

	closeChannel chan struct{}

	sendLock sync.Mutex
}

func NewClient(owner *Server, conn *net.TCPConn) *Client {
	c := &Client{
		Owner:                owner,
		Connection:           conn,
		ConnectionTime:       time.Now(),
		AddressPort:          netip.MustParseAddrPort(conn.RemoteAddr().String()),
		expectedMessage:      MessageHandshakeChallenge,
		closeChannel:         make(chan struct{}),
		BroadcastedHashes:    utils.NewCircularBuffer[types.Hash](8),
		RequestedHashes:      utils.NewCircularBuffer[types.Hash](16),
		blockPendingRequests: make(chan types.Hash, 100), //allow max 100 pending block requests at the same time
	}

	c.LastActiveTimestamp.Store(uint64(time.Now().Unix()))

	return c
}

func (c *Client) BanError() error {
	c.banErrorLock.Lock()
	c.banErrorLock.Unlock()
	return c.banError
}

func (c *Client) SetError(err error) {
	c.banErrorLock.Lock()
	c.banErrorLock.Unlock()
	if c.banError == nil {
		c.banError = err
	}
}

func (c *Client) Ban(duration time.Duration, err error) {

	c.SetError(err)
	c.Owner.Ban(c.AddressPort.Addr(), duration, err)
	c.Owner.RemoveFromPeerList(c.AddressPort.Addr())
	c.Close()
}

func (c *Client) OnAfterHandshake() {
	c.SendListenPort()
	c.SendBlockRequest(types.ZeroHash)
	log.Printf("[P2PClient] Peer %s after handshake complete: sent LISTEN_PORT + tip BLOCK_REQUEST", c.AddressPort.String())

	c.LastBroadcastTimestamp.Store(uint64(time.Now().Unix()))
}

func (c *Client) getNextBlockRequest() (id types.Hash, ok bool) {
	select {
	case id = <-c.blockPendingRequests:
		return id, true
	default:
		return types.ZeroHash, false
	}
}

func (c *Client) SendListenPort() {
	c.SendMessage(&ClientMessage{
		MessageId: MessageListenPort,
		Buffer:    binary.LittleEndian.AppendUint32(nil, uint32(c.Owner.ExternalListenPort())),
	})
}

func (c *Client) SendMissingBlockRequestAtRandom(hash types.Hash, allowedClients []*Client) []*Client {
	if hash == types.ZeroHash || c.Owner.SideChain().GetPoolBlockByTemplateId(hash) != nil {
		return allowedClients
	}

	if b := c.Owner.GetCachedBlock(hash); b != nil {
		log.Printf("[P2PClient] Using cached block for id = %s", hash.String())
		if _, err, _ := c.Owner.SideChain().AddPoolBlockExternal(b); err == nil {
			return allowedClients
		}
	}

	if len(allowedClients) == 0 {
		allowedClients = append(allowedClients, c)
	}

	for len(allowedClients) > 0 {
		k := unsafeRandom.Intn(len(allowedClients)) % len(allowedClients)
		client := allowedClients[k]
		if client.IsGood() && len(client.blockPendingRequests) < 20 {
			client.SendBlockRequest(hash)
			break
		} else {
			allowedClients = slices.Delete(allowedClients, k, k+1)
		}
	}
	return allowedClients
}

func (c *Client) SendMissingBlockRequest(hash types.Hash) {
	if hash == types.ZeroHash || c.Owner.SideChain().GetPoolBlockByTemplateId(hash) != nil {
		return
	}

	if b := c.Owner.GetCachedBlock(hash); b != nil {
		log.Printf("[P2PClient] Using cached block for id = %s", hash.String())
		if missingBlocks, err, _ := c.Owner.SideChain().AddPoolBlockExternal(b); err == nil {
			for _, id := range missingBlocks {
				c.SendMissingBlockRequest(id)
			}
			return
		}
	}

	// do not re-request hashes that have been requested
	if !c.RequestedHashes.PushUnique(hash) {
		return
	}

	// If the initial sync is not finished yet, try to ask the fastest peer too
	if !c.Owner.SideChain().PreCalcFinished() {
		fastest := c.Owner.GetFastestClient()
		if fastest != nil && c != fastest && !c.Owner.SideChain().PreCalcFinished() {
			//send towards the fastest peer as well
			fastest.SendMissingBlockRequest(hash)
		}
	}

	c.SendBlockRequest(hash)
}

func (c *Client) SendUniqueBlockRequest(hash types.Hash) {
	if hash == types.ZeroHash {
		return
	}

	// do not re-request hashes that have been requested
	if !c.RequestedHashes.PushUnique(hash) {
		return
	}

	c.SendBlockRequest(hash)
}

func (c *Client) SendBlockRequest(id types.Hash) {
	c.SendBlockRequestWithBound(id, 80)
}

func (c *Client) SendBlockRequestWithBound(id types.Hash, bound int) bool {
	if len(c.blockPendingRequests) < bound {
		c.blockPendingRequests <- id
		c.SendMessage(&ClientMessage{
			MessageId: MessageBlockRequest,
			Buffer:    id[:],
		})
		return true
	}
	return false
}

func (c *Client) SendBlockNotify(id types.Hash) {
	c.SendMessage(&ClientMessage{
		MessageId: MessageBlockNotify,
		Buffer:    id[:],
	})
}

func (c *Client) SendBlockResponse(block *sidechain.PoolBlock) {
	if block != nil {
		blockData, err := block.AppendBinaryFlags(make([]byte, 0, block.BufferLength()), false, false)
		if err != nil {
			log.Printf("[P2PClient] Peer %s tried to respond with a block but received error, disconnecting: %s", c.AddressPort, err)
			c.Close()
			return
		}

		c.SendMessage(&ClientMessage{
			MessageId: MessageBlockResponse,
			Buffer:    append(binary.LittleEndian.AppendUint32(make([]byte, 0, len(blockData)+4), uint32(len(blockData))), blockData...),
		})

	} else {
		c.SendMessage(&ClientMessage{
			MessageId: MessageBlockResponse,
			Buffer:    binary.LittleEndian.AppendUint32(nil, 0),
		})
	}
}

func (c *Client) SendPeerListRequest() {
	c.NextOutgoingPeerListRequestTimestamp.Store(uint64(time.Now().Unix()) + PeerRequestDelay + (unsafeRandom.Uint64() % (PeerRequestDelay + 1)))
	c.SendMessage(&ClientMessage{
		MessageId: MessagePeerListRequest,
	})
	c.LastPeerListRequestTimestamp.Store(uint64(time.Now().UnixMicro()))
	//log.Printf("[P2PClient] Sending PEER_LIST_REQUEST to %s", c.AddressPort.String())
}

func (c *Client) SendPeerListResponse(list []netip.AddrPort) {
	if len(list) > PeerListResponseMaxPeers {
		return
	}
	buf := make([]byte, 0, 1+len(list)*(1+16+2))
	buf = append(buf, byte(len(list)))
	for i := range list {
		//TODO: check ipv4 gets sent properly
		if list[i].Addr().Is6() && !p2pooltypes.IsPeerVersionInformation(list[i]) {
			buf = append(buf, 1)
		} else {
			buf = append(buf, 0)
		}
		ip := list[i].Addr().As16()
		buf = append(buf, ip[:]...)
		buf = binary.LittleEndian.AppendUint16(buf, list[i].Port())
	}
	c.SendMessage(&ClientMessage{
		MessageId: MessagePeerListResponse,
		Buffer:    buf,
	})
}

func (c *Client) IsGood() bool {
	return c.HandshakeComplete.Load() && c.ListenPort.Load() > 0
}

func (c *Client) OnConnection() {
	c.LastActiveTimestamp.Store(uint64(time.Now().Unix()))

	c.sendHandshakeChallenge()

	var messageIdBuf [1]byte
	var messageId MessageId
	for !c.Closed.Load() {
		if _, err := io.ReadFull(c, messageIdBuf[:]); err != nil {
			c.Close()
			return
		}

		messageId = MessageId(messageIdBuf[0])

		if !c.HandshakeComplete.Load() && messageId != c.expectedMessage {
			c.Ban(DefaultBanTime, fmt.Errorf("unexpected pre-handshake message: got %d, expected %d", messageId, c.expectedMessage))
			return
		}

		switch messageId {
		case MessageHandshakeChallenge:
			if c.HandshakeComplete.Load() {
				c.Ban(DefaultBanTime, errors.New("got HANDSHAKE_CHALLENGE but handshake is complete"))
				return
			}

			var challenge HandshakeChallenge
			var peerId uint64
			if err := binary.Read(c, binary.LittleEndian, &challenge); err != nil {
				c.Ban(DefaultBanTime, err)
				return
			}
			if err := binary.Read(c, binary.LittleEndian, &peerId); err != nil {
				c.Ban(DefaultBanTime, err)
				return
			}

			if peerId == c.Owner.PeerId() {
				c.HandshakeComplete.Store(true)
				c.SetError(errors.New("connected to self"))
				//tried to connect to self
				c.Close()
				return
			}

			c.PeerId.Store(peerId)

			if ok, otherClient := func() (bool, *Client) {
				c.Owner.clientsLock.RLock()
				defer c.Owner.clientsLock.RUnlock()
				for _, client := range c.Owner.clients {
					if client != c && client.PeerId.Load() == peerId {
						return true, client
					}
				}
				return false, nil
			}(); ok {
				c.HandshakeComplete.Store(true)
				c.SetError(fmt.Errorf("already connected as %s (%d)", otherClient.AddressPort, otherClient.PeerId.Load()))
				//same peer
				log.Printf("[P2PClient] Connected to other same peer: %s (%d) is also %s (%d)", c.AddressPort, c.PeerId.Load(), otherClient.AddressPort, otherClient.PeerId.Load())
				c.Close()
				return
			}

			c.sendHandshakeSolution(challenge)

			c.expectedMessage = MessageHandshakeSolution

			if c.HandshakeComplete.Load() && c.SentHandshakeSolution.Load() {
				c.OnAfterHandshake()
			}

		case MessageHandshakeSolution:
			if c.HandshakeComplete.Load() {
				c.Ban(DefaultBanTime, errors.New("got HANDSHAKE_SOLUTION but handshake is complete"))
				return
			}

			var challengeHash types.Hash
			var solution uint64
			if err := binary.Read(c, binary.LittleEndian, &challengeHash); err != nil {
				c.Ban(DefaultBanTime, err)
				return
			}
			if err := binary.Read(c, binary.LittleEndian, &solution); err != nil {
				c.Ban(DefaultBanTime, err)
				return
			}

			if c.IsIncomingConnection {
				if hash, ok := CalculateChallengeHash(c.handshakeChallenge, c.Owner.Consensus().Id, solution); !ok {
					//not enough PoW
					c.Ban(DefaultBanTime, fmt.Errorf("not enough PoW on HANDSHAKE_SOLUTION, challenge = %s, solution = %d, calculated hash = %s, expected hash = %s", hex.EncodeToString(c.handshakeChallenge[:]), solution, hash.String(), challengeHash.String()))
					return
				} else if hash != challengeHash {
					//wrong hash
					c.Ban(DefaultBanTime, fmt.Errorf("wrong hash HANDSHAKE_SOLUTION, challenge = %s, solution = %d, calculated hash = %s, expected hash = %s", hex.EncodeToString(c.handshakeChallenge[:]), solution, hash.String(), challengeHash.String()))
					return
				}
			} else {
				if hash, _ := CalculateChallengeHash(c.handshakeChallenge, c.Owner.Consensus().Id, solution); hash != challengeHash {
					//wrong hash
					c.Ban(DefaultBanTime, fmt.Errorf("wrong hash HANDSHAKE_SOLUTION, challenge = %s, solution = %d, calculated hash = %s, expected hash = %s", hex.EncodeToString(c.handshakeChallenge[:]), solution, hash.String(), challengeHash.String()))
					return
				}
			}
			c.HandshakeComplete.Store(true)

			if c.HandshakeComplete.Load() && c.SentHandshakeSolution.Load() {
				c.OnAfterHandshake()
			}

		case MessageListenPort:
			if c.ListenPort.Load() != 0 {
				c.Ban(DefaultBanTime, errors.New("got LISTEN_PORT but we already received it"))
				return
			}

			var listenPort uint32

			if err := binary.Read(c, binary.LittleEndian, &listenPort); err != nil {
				c.Ban(DefaultBanTime, err)
				return
			}

			if listenPort == 0 || listenPort >= 65536 {
				c.Ban(DefaultBanTime, fmt.Errorf("listen port out of range: %d", listenPort))
				return
			}
			c.ListenPort.Store(listenPort)
			c.Owner.UpdateInPeerList(netip.AddrPortFrom(c.AddressPort.Addr(), uint16(c.ListenPort.Load())))
		case MessageBlockRequest:
			c.LastBlockRequestTimestamp.Store(uint64(time.Now().Unix()))

			var templateId types.Hash
			if err := binary.Read(c, binary.LittleEndian, &templateId); err != nil {
				c.Ban(DefaultBanTime, err)
				return
			}

			var block *sidechain.PoolBlock
			//if empty, return chain tip
			if templateId == types.ZeroHash {
				log.Printf("[P2PClient] Peer %s requested tip", c.AddressPort.String())
				// Don't return stale chain tip
				if block = c.Owner.SideChain().GetChainTip(); block != nil && (block.Main.Coinbase.GenHeight+2) < c.Owner.MainChain().GetMinerDataTip().Height {
					block = nil
				}
			} else {
				block = c.Owner.SideChain().GetPoolBlockByTemplateId(templateId)
				if block == nil {
					log.Printf("[P2PClient] Peer %s requested id = %s, got nil", c.AddressPort.String(), templateId)
				} else {
					log.Printf("[P2PClient] Peer %s requested id = %s, got height = %d, main height = %d", c.AddressPort.String(), templateId, block.Side.Height, block.Main.Coinbase.GenHeight)
				}
			}

			if block != nil && c.VersionInformation.SupportsFeature(p2pooltypes.FeatureBlockNotify) {
				c.SendBlockNotify(block.Side.Parent)
				for _, uncleId := range block.Side.Uncles {
					c.SendBlockNotify(uncleId)
				}
				if parent := c.Owner.SideChain().GetParent(block); parent != nil {
					c.SendBlockNotify(parent.Side.Parent)
				}
			}

			c.SendBlockResponse(block)
		case MessageBlockResponse:
			block := &sidechain.PoolBlock{
				LocalTimestamp: uint64(time.Now().Unix()),
			}

			expectedBlockId, ok := c.getNextBlockRequest()

			if !ok {
				c.Ban(DefaultBanTime, errors.New("unexpected BLOCK_RESPONSE"))
				return
			}

			isChainTipBlockRequest := expectedBlockId == types.ZeroHash

			var blockSize uint32
			if err := binary.Read(c, binary.LittleEndian, &blockSize); err != nil {
				//TODO warn
				c.Ban(DefaultBanTime, err)
				return
			} else if blockSize == 0 {
				log.Printf("[P2PClient] Peer %s sent nil BLOCK_RESPONSE to id = %s", c.AddressPort.String(), expectedBlockId)
				if isChainTipBlockRequest && time.Now().Unix() >= int64(c.NextOutgoingPeerListRequestTimestamp.Load()) {
					c.SendPeerListRequest()
				}
				break
			} else {
				if err = block.FromReader(c.Owner.Consensus(), c.Owner.SideChain().DerivationCache(), bufio.NewReaderSize(io.LimitReader(c, int64(blockSize)), int(blockSize))); err != nil {
					//TODO warn
					c.Ban(DefaultBanTime, err)
					return
				} else {
					tipHash := types.HashFromBytes(block.CoinbaseExtra(sidechain.SideTemplateId))
					if isChainTipBlockRequest {
						c.LastKnownTip.Store(block)
						log.Printf("[P2PClient] Peer %s tip is at id = %s, height = %d, main height = %d", c.AddressPort.String(), tipHash, block.Side.Height, block.Main.Coinbase.GenHeight)
						peerHeight := block.Main.Coinbase.GenHeight
						ourHeight := c.Owner.MainChain().GetMinerDataTip().Height

						if (peerHeight + 2) < ourHeight {
							c.Ban(DefaultBanTime, fmt.Errorf("mining on top of a stale block (mainchain peer height %d, expected >= %d)", peerHeight, ourHeight))
							return
						}

						//Atomic max, not necessary as no external writers exist
						topHeight := max(c.BroadcastMaxHeight.Load(), block.Side.Height)
						for {
							if oldHeight := c.BroadcastMaxHeight.Swap(topHeight); oldHeight <= topHeight {
								break
							} else {
								topHeight = oldHeight
							}
						}

						if time.Now().Unix() >= int64(c.NextOutgoingPeerListRequestTimestamp.Load()) {
							c.SendPeerListRequest()
						}
					}
					if c.Owner.SideChain().BlockSeen(block) {
						//log.Printf("[P2PClient] Peer %s block id = %s, height = %d (nonce %d, extra_nonce %d) was received before, skipping it", c.AddressPort.String(), types.HashFromBytes(block.CoinbaseExtra(sidechain.SideTemplateId)), block.Side.Height, block.Main.Nonce, block.ExtraNonce())
						break
					}
					if missingBlocks, err, ban := c.Owner.SideChain().AddPoolBlockExternal(block); err != nil {
						if ban {
							c.Ban(DefaultBanTime, err)
							return
						} else {
							log.Printf("[P2PClient] Peer %s error adding block id = %s, height = %d, main height = %d, timestamp = %d", c.AddressPort.String(), tipHash, block.Side.Height, block.Main.Coinbase.GenHeight, block.Main.Timestamp)
							break
						}
					} else {
						if !isChainTipBlockRequest && expectedBlockId != block.SideTemplateId(c.Owner.SideChain().Consensus()) {
							c.Ban(DefaultBanTime, fmt.Errorf("expected block id = %s, got %s", expectedBlockId.String(), block.SideTemplateId(c.Owner.SideChain().Consensus()).String()))
							return
						}
						for _, id := range missingBlocks {
							c.SendMissingBlockRequest(id)
						}
					}
				}
			}

		case MessageBlockBroadcast, MessageBlockBroadcastCompact:
			block := &sidechain.PoolBlock{
				LocalTimestamp: uint64(time.Now().Unix()),
			}
			var blockSize uint32
			if err := binary.Read(c, binary.LittleEndian, &blockSize); err != nil {
				//TODO warn
				c.Ban(DefaultBanTime, err)
				return
			} else if blockSize == 0 {
				//NOT found
				//TODO log
				break
			} else if messageId == MessageBlockBroadcastCompact {
				if err = block.FromCompactReader(c.Owner.Consensus(), c.Owner.SideChain().DerivationCache(), bufio.NewReaderSize(io.LimitReader(c, int64(blockSize)), int(blockSize))); err != nil {
					//TODO warn
					c.Ban(DefaultBanTime, err)
					return
				}
			} else {
				if err = block.FromReader(c.Owner.Consensus(), c.Owner.SideChain().DerivationCache(), bufio.NewReaderSize(io.LimitReader(c, int64(blockSize)), int(blockSize))); err != nil {
					//TODO warn
					c.Ban(DefaultBanTime, err)
					return
				}
			}

			//Atomic max, not necessary as no external writers exist
			topHeight := max(c.BroadcastMaxHeight.Load(), block.Side.Height)
			for {
				if oldHeight := c.BroadcastMaxHeight.Swap(topHeight); oldHeight <= topHeight {
					break
				} else {
					topHeight = oldHeight
				}
			}

			tipHash := types.HashFromBytes(block.CoinbaseExtra(sidechain.SideTemplateId))

			c.BroadcastedHashes.Push(tipHash)

			c.LastBroadcastTimestamp.Store(uint64(time.Now().Unix()))
			c.LastKnownTip.Store(block)
			//log.Printf("[P2PClient] Peer %s broadcast tip is at id = %s, height = %d, main height = %d", c.AddressPort.String(), tipHash, block.Side.Height, block.Main.Coinbase.GenHeight)

			if missingBlocks, err := c.Owner.SideChain().PreprocessBlock(block); err != nil {
				for _, id := range missingBlocks {
					c.SendMissingBlockRequest(id)
				}
				//TODO: ban here, but sort blocks properly, maybe a queue to re-try?
				break
			} else {
				ourMinerData := c.Owner.MainChain().GetMinerDataTip()

				if block.Main.PreviousId != ourMinerData.PrevId {
					// This peer is mining on top of a different Monero block, investigate it

					peerHeight := block.Main.Coinbase.GenHeight
					ourHeight := ourMinerData.Height

					if peerHeight < ourHeight {
						if (ourHeight - peerHeight) < 5 {
							elapsedTime := time.Now().Sub(ourMinerData.TimeReceived)
							if (ourHeight-peerHeight) > 1 || elapsedTime > (time.Second*10) {
								log.Printf("[P2PClient] Peer %s broadcasted a stale block (%d ms late, mainchain height %d, expected >= %d), ignoring it", c.AddressPort.String(), elapsedTime.Milliseconds(), peerHeight, ourHeight)
							}
						} else {
							c.Ban(DefaultBanTime, fmt.Errorf("broadcasted an unreasonably stale block (mainchain height %d, expected >= %d)", peerHeight, ourHeight))
							return
						}
					} else if peerHeight > ourHeight {
						if peerHeight >= (ourHeight + 2) {
							log.Printf("[P2PClient] Peer %s is ahead on mainchain (mainchain height %d, your height %d). Is monerod stuck or lagging?", c.AddressPort.String(), peerHeight, ourHeight)
						}
					} else {
						log.Printf("[P2PClient] Peer %s is mining on an alternative mainchain tip (mainchain height %d, previous_id = %s)", c.AddressPort.String(), peerHeight, block.Main.PreviousId)
					}
				}

				if c.Owner.SideChain().BlockSeen(block) {
					//log.Printf("[P2PClient] Peer %s block id = %s, height = %d (nonce %d, extra_nonce %d) was received before, skipping it", c.AddressPort.String(), types.HashFromBytes(block.CoinbaseExtra(sidechain.SideTemplateId)), block.Side.Height, block.Main.Nonce, block.ExtraNonce())
					break
				}

				block.WantBroadcast.Store(true)
				if missingBlocks, err, ban := c.Owner.SideChain().AddPoolBlockExternal(block); err != nil {
					if ban {
						c.Ban(DefaultBanTime, err)
					} else {
						log.Printf("[P2PClient] Peer %s error adding block id = %s, height = %d, main height = %d, timestamp = %d", c.AddressPort.String(), tipHash, block.Side.Height, block.Main.Coinbase.GenHeight, block.Main.Timestamp)
					}
					return
				} else {
					for _, id := range missingBlocks {
						c.SendMissingBlockRequest(id)
					}
				}
			}
		case MessagePeerListRequest:
			connectedPeerList := c.Owner.Clients()

			entriesToSend := make([]netip.AddrPort, 0, PeerListResponseMaxPeers)

			// Send every 4th peer on average, selected at random
			peersToSendTarget := min(PeerListResponseMaxPeers, max(len(connectedPeerList)/4, 1))
			n := 0
			for _, peer := range connectedPeerList {
				if peer.AddressPort.Addr().IsLoopback() || peer.AddressPort.Addr().IsPrivate() || !peer.IsGood() || peer.AddressPort.Addr().Compare(c.AddressPort.Addr()) == 0 {
					continue
				}

				n++

				// Use https://en.wikipedia.org/wiki/Reservoir_sampling algorithm
				if len(entriesToSend) < peersToSendTarget {
					entriesToSend = append(entriesToSend, peer.AddressPort)
				}

				k := unsafeRandom.Intn(n)
				if k < peersToSendTarget {
					entriesToSend[k] = peer.AddressPort
				}
			}

			if c.LastIncomingPeerListRequestTime.IsZero() {
				//first, send version / protocol information
				if len(entriesToSend) == 0 {
					entriesToSend = append(entriesToSend, c.Owner.versionInformation.ToAddrPort())
				} else {
					entriesToSend[0] = c.Owner.versionInformation.ToAddrPort()
				}
			}

			lastLen := len(entriesToSend)

			if lastLen < PeerListResponseMaxPeers {
				//improvement from normal p2pool: pad response with other peers from peer list, not connected
				peerList := c.Owner.PeerList()
				for i := lastLen; i < PeerListResponseMaxPeers; i++ {
					k := unsafeRandom.Intn(len(peerList)) % len(peerList)
					peer := peerList[k]
					if !slices.ContainsFunc(entriesToSend, func(addrPort netip.AddrPort) bool {
						return addrPort.Addr().Compare(peer.AddressPort.Addr()) == 0
					}) {
						entriesToSend = append(entriesToSend, peer.AddressPort)
					}
				}
			}

			var hasIpv6 bool
			for _, e := range entriesToSend {
				if e.Addr().Is6() {
					hasIpv6 = true
					break
				}
			}

			//include one ipv6, if existent
			if !hasIpv6 {
				peerList := c.Owner.PeerList()
				unsafeRandom.Shuffle(len(peerList), func(i, j int) {
					peerList[i] = peerList[j]
				})
				for _, p := range c.Owner.PeerList() {
					if p.AddressPort.Addr().Is4In6() || p.AddressPort.Addr().Is6() {
						if len(entriesToSend) < PeerListResponseMaxPeers {
							entriesToSend = append(entriesToSend, p.AddressPort)
						} else {
							entriesToSend[len(entriesToSend)-1] = p.AddressPort
						}
						break
					}
				}
			}

			c.LastIncomingPeerListRequestTime = time.Now()

			c.SendPeerListResponse(entriesToSend)
		case MessagePeerListResponse:
			if numPeers, err := c.ReadByte(); err != nil {
				c.Ban(DefaultBanTime, err)
				return
			} else if numPeers > PeerListResponseMaxPeers {
				c.Ban(DefaultBanTime, fmt.Errorf("too many peers on PEER_LIST_RESPONSE num_peers = %d", numPeers))
				return
			} else {
				firstPeerResponse := c.PingDuration.Swap(uint64(max(time.Now().Sub(time.UnixMicro(int64(c.LastPeerListRequestTimestamp.Load()))), 0))) == 0
				var rawIp [16]byte
				var port uint16

				if firstPeerResponse {
					log.Printf("[P2PClient] Peer %s initial PEER_LIST_RESPONSE: num_peers %d", c.AddressPort.String(), numPeers)
				}
				for i := uint8(0); i < numPeers; i++ {
					if isV6, err := c.ReadByte(); err != nil {
						c.Ban(DefaultBanTime, err)
						return
					} else {
						if _, err = c.Read(rawIp[:]); err != nil {
							c.Ban(DefaultBanTime, err)
							return
						} else if err = binary.Read(c, binary.LittleEndian, &port); err != nil {
							c.Ban(DefaultBanTime, err)
							return
						}

						if isV6 == 0 {
							if rawIp[12] == 0 || rawIp[12] >= 224 {
								// Ignore 0.0.0.0/8 (special-purpose range for "this network") and 224.0.0.0/3 (IP multicast and reserved ranges)

								// Check for protocol version message
								if binary.LittleEndian.Uint32(rawIp[12:]) == 0xFFFFFFFF && port == 0xFFFF {
									c.VersionInformation.Protocol = p2pooltypes.ProtocolVersion(binary.LittleEndian.Uint32(rawIp[0:]))
									c.VersionInformation.SoftwareVersion = p2pooltypes.SoftwareVersion(binary.LittleEndian.Uint32(rawIp[4:]))
									c.VersionInformation.SoftwareId = p2pooltypes.SoftwareId(binary.LittleEndian.Uint32(rawIp[8:]))
									log.Printf("[P2PClient] Peer %s version information: %s", c.AddressPort.String(), c.VersionInformation.String())

									c.afterInitialProtocolExchange()
								}
								continue
							}

							copy(rawIp[:], make([]byte, 10))
							rawIp[10], rawIp[11] = 0xFF, 0xFF

						}

						c.Owner.AddToPeerList(netip.AddrPortFrom(netip.AddrFrom16(rawIp).Unmap(), port))
					}
				}
			}
		case MessageBlockNotify:
			c.LastBlockRequestTimestamp.Store(uint64(time.Now().Unix()))

			var templateId types.Hash
			if err := binary.Read(c, binary.LittleEndian, &templateId); err != nil {
				c.Ban(DefaultBanTime, err)
				return
			}

			c.BroadcastedHashes.Push(templateId)

			// If we don't know about this block, request it from this peer. The peer can do it to speed up our initial sync, for example.
			if tip := c.Owner.SideChain().GetPoolBlockByTemplateId(templateId); tip == nil {
				//TODO: prevent sending duplicate requests
				if c.SendBlockRequestWithBound(templateId, 25) {

				}
			} else {
				c.LastKnownTip.Store(tip)
			}

		case MessageInternal:
			internalMessageId, err := binary.ReadUvarint(c)
			if err != nil {
				c.Ban(DefaultBanTime, err)
				return
			}
			messageSize, err := binary.ReadUvarint(c)
			if err != nil {
				c.Ban(DefaultBanTime, err)
				return
			}
			reader := io.LimitReader(c, int64(messageSize))

			_ = reader

			switch InternalMessageId(internalMessageId) {
			default:
				c.Ban(DefaultBanTime, fmt.Errorf("unknown InternalMessageId %d", internalMessageId))
				return
			}
		default:
			c.Ban(DefaultBanTime, fmt.Errorf("unknown MessageId %d", messageId))
			return
		}

		c.LastActiveTimestamp.Store(uint64(time.Now().Unix()))
	}
}

func (c *Client) afterInitialProtocolExchange() {
	//TODO: use notify to send fast sync data
}

func (c *Client) sendHandshakeChallenge() {
	if _, err := rand.Read(c.handshakeChallenge[:]); err != nil {
		log.Printf("[P2PServer] Unable to generate handshake challenge for %s", c.AddressPort.String())
		c.Close()
		return
	}

	var buf [HandshakeChallengeSize + int(unsafe.Sizeof(uint64(0)))]byte
	copy(buf[:], c.handshakeChallenge[:])
	binary.LittleEndian.PutUint64(buf[HandshakeChallengeSize:], c.Owner.PeerId())

	c.SendMessage(&ClientMessage{
		MessageId: MessageHandshakeChallenge,
		Buffer:    buf[:],
	})
}

func (c *Client) sendHandshakeSolution(challenge HandshakeChallenge) {
	stop := &c.Closed
	if c.IsIncomingConnection {
		stop = &atomic.Bool{}
		stop.Store(true)
	}

	if solution, hash, ok := FindChallengeSolution(challenge, c.Owner.Consensus().Id, stop); ok || c.IsIncomingConnection {

		var buf [HandshakeChallengeSize + types.HashSize]byte
		copy(buf[:], hash[:])
		binary.LittleEndian.PutUint64(buf[types.HashSize:], solution)

		c.SendMessage(&ClientMessage{
			MessageId: MessageHandshakeSolution,
			Buffer:    buf[:],
		})
		c.SentHandshakeSolution.Store(true)
	}
}

// Read reads from underlying connection, on error it will Close
func (c *Client) Read(buf []byte) (n int, err error) {
	if n, err = c.Connection.Read(buf); err != nil {
		c.Close()
	}
	return
}

type ClientMessage struct {
	MessageId MessageId
	Buffer    []byte
}

func (c *Client) SendMessage(message *ClientMessage) {
	if !c.Closed.Load() {
		bufLen := len(message.Buffer) + 1
		if bufLen > MaxBufferSize {
			log.Printf("[P2PClient] Peer %s tried to send more than %d bytes, sent %d, disconnecting", c.AddressPort, MaxBufferSize, len(message.Buffer)+1)
			c.Close()
			return
		}

		buf := getBuffer(bufLen)
		defer returnBuffer(buf)
		buf[0] = byte(message.MessageId)
		copy(buf[1:], message.Buffer)
		//c.sendLock.Lock()
		//defer c.sendLock.Unlock()
		if err := c.Connection.SetWriteDeadline(time.Now().Add(time.Second * 5)); err != nil {
			c.Close()
		} else if _, err = c.Connection.Write(buf[:bufLen]); err != nil {
			c.Close()
		}
		//_, _ = c.Write(message.Buffer)
	}
}

// ReadByte reads from underlying connection, on error it will Close
func (c *Client) ReadByte() (b byte, err error) {
	var buf [1]byte
	if _, err = c.Connection.Read(buf[:]); err != nil && c.Closed.Load() {
		c.Close()
	}
	return buf[0], err
}

func (c *Client) Close() bool {
	if c.Closed.Swap(true) {
		return false
	}

	if !c.HandshakeComplete.Load() {
		c.Ban(DefaultBanTime, errors.New("disconnected before finishing handshake"))
	}

	func() {
		c.Owner.clientsLock.Lock()
		defer c.Owner.clientsLock.Unlock()
		if c.Owner.fastestPeer == c {
			c.Owner.fastestPeer = nil
		}
		if i := slices.Index(c.Owner.clients, c); i != -1 {
			c.Owner.clients = slices.Delete(c.Owner.clients, i, i+1)
			if c.IsIncomingConnection {
				c.Owner.NumIncomingConnections.Add(-1)
			} else {
				c.Owner.NumOutgoingConnections.Add(-1)
				c.Owner.PendingOutgoingConnections.Replace(c.AddressPort.Addr().String(), "")
			}
		}
	}()

	_ = c.Connection.Close()
	close(c.closeChannel)

	log.Printf("[P2PClient] Peer %s connection closed", c.AddressPort.String())
	return true
}
