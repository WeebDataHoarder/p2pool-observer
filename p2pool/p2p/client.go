package p2p

import (
	"bufio"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"golang.org/x/exp/slices"
	"io"
	"log"
	unsafeRandom "math/rand"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

const DefaultBanTime = time.Second * 600
const PeerListResponseMaxPeers = 16

// MaxBlockTemplateSize Max P2P message size (128 KB) minus BLOCK_RESPONSE header (5 bytes)
const MaxBlockTemplateSize = 128*1024 - (1 - 4)

type Client struct {
	Owner                           *Server
	Connection                      *net.TCPConn
	Closed                          atomic.Bool
	AddressPort                     netip.AddrPort
	LastActive                      time.Time
	LastBroadcast                   time.Time
	LastBlockRequest                time.Time
	PingTime                        atomic.Uint64
	LastPeerListRequestTime         atomic.Uint64
	NextOutgoingPeerListRequest     atomic.Uint64
	LastIncomingPeerListRequestTime time.Time
	PeerId                          uint64
	VersionInformation              PeerVersionInformation
	IsIncomingConnection            bool
	HandshakeComplete               atomic.Bool
	ListenPort                      atomic.Uint32

	BroadcastedHashes *utils.CircularBuffer[types.Hash]
	RequestedHashes   *utils.CircularBuffer[types.Hash]

	BlockPendingRequests int64
	ChainTipBlockRequest atomic.Bool

	expectedMessage MessageId

	handshakeChallenge HandshakeChallenge

	closeChannel chan struct{}

	blockRequestThrottler <-chan time.Time

	sendLock sync.Mutex
}

func NewClient(owner *Server, conn *net.TCPConn) *Client {
	c := &Client{
		Owner:                 owner,
		Connection:            conn,
		AddressPort:           netip.MustParseAddrPort(conn.RemoteAddr().String()),
		LastActive:            time.Now(),
		expectedMessage:       MessageHandshakeChallenge,
		blockRequestThrottler: time.Tick(time.Second / 50), //maximum 50 per second
		closeChannel:          make(chan struct{}),
		BroadcastedHashes:     utils.NewCircularBuffer[types.Hash](8),
		RequestedHashes:       utils.NewCircularBuffer[types.Hash](16),
	}

	return c
}

func (c *Client) Ban(duration time.Duration, err error) {
	c.Owner.Ban(c.AddressPort.Addr(), duration, err)
	c.Close()
}

func (c *Client) OnAfterHandshake() {
	c.SendListenPort()
	c.SendBlockRequest(types.ZeroHash)
}

func (c *Client) SendListenPort() {
	c.SendMessage(&ClientMessage{
		MessageId: MessageListenPort,
		Buffer:    binary.LittleEndian.AppendUint32(nil, uint32(c.Owner.listenAddress.Port())),
	})
}

func (c *Client) SendMissingBlockRequest(hash types.Hash) {
	if hash == types.ZeroHash {
		return
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

func (c *Client) SendBlockRequest(hash types.Hash) {
	//<-c.blockRequestThrottler

	c.SendMessage(&ClientMessage{
		MessageId: MessageBlockRequest,
		Buffer:    hash[:],
	})

	c.BlockPendingRequests++
	if hash == types.ZeroHash {
		c.ChainTipBlockRequest.Store(true)
	}
	c.LastBroadcast = time.Now()
}

func (c *Client) SendBlockResponse(block *sidechain.PoolBlock) {
	if block != nil {
		blockData, _ := block.MarshalBinary()

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
	c.NextOutgoingPeerListRequest.Store(uint64(time.Now().Unix()) + 60 + (unsafeRandom.Uint64() % 61))
	c.SendMessage(&ClientMessage{
		MessageId: MessagePeerListRequest,
	})
	c.LastPeerListRequestTime.Store(uint64(time.Now().UnixMicro()))
	log.Printf("[P2PClient] Sending PEER_LIST_REQUEST to %s", c.AddressPort.String())
}

func (c *Client) SendPeerListResponse(list []netip.AddrPort) {
	if len(list) > PeerListResponseMaxPeers {
		return
	}
	buf := make([]byte, 0, 1+len(list)*(1+16+2))
	buf = append(buf, byte(len(list)))
	for i := range list {
		if list[i].Addr().Is6() && !IsPeerVersionInformation(list[i]) {
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
	c.LastActive = time.Now()

	c.sendHandshakeChallenge()

	var messageIdBuf [1]byte
	var messageId MessageId
	for !c.Closed.Load() {
		if _, err := io.ReadFull(c, messageIdBuf[:]); err != nil {
			c.Ban(DefaultBanTime, err)
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
				//tried to connect to self
				c.Close()
				return
			}

			c.PeerId = peerId

			if func() bool {
				c.Owner.clientsLock.RLock()
				defer c.Owner.clientsLock.RUnlock()
				for _, client := range c.Owner.clients {
					if client != c && client.PeerId == peerId {
						return true
					}
				}
				return false
			}() {
				//same peer
				c.Close()
				return
			}

			c.sendHandshakeSolution(challenge)

			c.expectedMessage = MessageHandshakeSolution

			c.OnAfterHandshake()

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
				if hash, ok := CalculateChallengeHash(c.handshakeChallenge, c.Owner.Consensus().Id(), solution); !ok {
					//not enough PoW
					c.Ban(DefaultBanTime, fmt.Errorf("not enough PoW on HANDSHAKE_SOLUTION, challenge = %s, solution = %d, calculated hash = %s, expected hash = %s", hex.EncodeToString(c.handshakeChallenge[:]), solution, hash.String(), challengeHash.String()))
					return
				} else if hash != challengeHash {
					//wrong hash
					c.Ban(DefaultBanTime, fmt.Errorf("wrong hash HANDSHAKE_SOLUTION, challenge = %s, solution = %d, calculated hash = %s, expected hash = %s", hex.EncodeToString(c.handshakeChallenge[:]), solution, hash.String(), challengeHash.String()))
					return
				}
			} else {
				if hash, _ := CalculateChallengeHash(c.handshakeChallenge, c.Owner.Consensus().Id(), solution); hash != challengeHash {
					//wrong hash
					c.Ban(DefaultBanTime, fmt.Errorf("wrong hash HANDSHAKE_SOLUTION, challenge = %s, solution = %d, calculated hash = %s, expected hash = %s", hex.EncodeToString(c.handshakeChallenge[:]), solution, hash.String(), challengeHash.String()))
					return
				}
			}
			c.HandshakeComplete.Store(true)

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
		case MessageBlockRequest:
			c.LastBlockRequest = time.Now()

			var templateId types.Hash
			if err := binary.Read(c, binary.LittleEndian, &templateId); err != nil {
				c.Ban(DefaultBanTime, err)
				return
			}

			var block *sidechain.PoolBlock
			//if empty, return chain tip
			if templateId == types.ZeroHash {
				//todo: don't return stale
				block = c.Owner.SideChain().GetChainTip()
			} else {
				block = c.Owner.SideChain().GetPoolBlockByTemplateId(templateId)
			}

			c.SendBlockResponse(block)
		case MessageBlockResponse:
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
			} else {
				if err = block.FromReader(bufio.NewReader(io.LimitReader(c, int64(blockSize)))); err != nil {
					//TODO warn
					c.Ban(DefaultBanTime, err)
					return
				} else {
					if c.ChainTipBlockRequest.Swap(false) {
						//peerHeight := block.Main.Coinbase.GenHeight
						//ourHeight :=
						//TODO: stale block

						log.Printf("[P2PClient] Peer %s tip is at id = %s, height = %d, main height = %d", c.AddressPort.String(), types.HashFromBytes(block.CoinbaseExtra(sidechain.SideTemplateId)), block.Side.Height, block.Main.Coinbase.GenHeight)

						c.SendPeerListRequest()
					}
					if missingBlocks, err := c.Owner.SideChain().AddPoolBlockExternal(block); err != nil {
						//TODO warn
						c.Ban(DefaultBanTime, err)
						return
					} else {
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
			} else if messageId == MessageBlockBroadcastCompact {
				if err = block.FromCompactReader(bufio.NewReader(io.LimitReader(c, int64(blockSize)))); err != nil {
					//TODO warn
					c.Ban(DefaultBanTime, err)
					return
				}
			} else {
				if err = block.FromReader(bufio.NewReader(io.LimitReader(c, int64(blockSize)))); err != nil {
					//TODO warn
					c.Ban(DefaultBanTime, err)
					return
				}
			}

			c.BroadcastedHashes.Push(types.HashFromBytes(block.CoinbaseExtra(sidechain.SideTemplateId)))

			c.LastBroadcast = time.Now()
			if missingBlocks, err := c.Owner.SideChain().PreprocessBlock(block); err != nil {
				for _, id := range missingBlocks {
					c.SendMissingBlockRequest(id)
				}
				//TODO: ban here, but sort blocks properly, maybe a queue to re-try?
				return
			} else {
				//TODO: investigate different monero block mining

				block.WantBroadcast.Store(true)
				if missingBlocks, err = c.Owner.SideChain().AddPoolBlockExternal(block); err != nil {
					//TODO warn
					c.Ban(DefaultBanTime, err)
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
			peersToSendTarget := utils.Min(PeerListResponseMaxPeers, utils.Max(len(entriesToSend)/4, 1))
			n := 0
			for _, peer := range connectedPeerList {
				if peer.AddressPort.Addr().IsLoopback() || !peer.IsGood() || peer.AddressPort.Addr().Compare(c.AddressPort.Addr()) == 0 {
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
				c.PingTime.Store(uint64(utils.Max(time.Now().Sub(time.UnixMicro(int64(c.LastPeerListRequestTime.Load()))), 0)))
				var rawIp [16]byte
				var port uint16

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
									c.VersionInformation.Protocol = ProtocolVersion(binary.LittleEndian.Uint32(rawIp[0:]))
									c.VersionInformation.Version = ImplementationVersion(binary.LittleEndian.Uint32(rawIp[4:]))
									c.VersionInformation.Code = ImplementationCode(binary.LittleEndian.Uint32(rawIp[8:]))
									log.Printf("peer %s is %s", c.AddressPort.String(), c.VersionInformation.String())
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
			//TODO
		default:
			c.Ban(DefaultBanTime, fmt.Errorf("unknown MessageId %d", messageId))
			return
		}
	}
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

	if solution, hash, ok := FindChallengeSolution(challenge, c.Owner.Consensus().Id(), stop); ok {

		var buf [HandshakeChallengeSize + types.HashSize]byte
		copy(buf[:], hash[:])
		binary.LittleEndian.PutUint64(buf[types.HashSize:], solution)

		c.SendMessage(&ClientMessage{
			MessageId: MessageHandshakeSolution,
			Buffer:    buf[:],
		})
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
		//c.sendLock.Lock()
		//defer c.sendLock.Unlock()
		if _, err := c.Connection.Write(append([]byte{byte(message.MessageId)}, message.Buffer...)); err != nil {
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

func (c *Client) Close() {
	if c.Closed.Swap(true) {
		return
	}

	c.Owner.clientsLock.Lock()
	defer c.Owner.clientsLock.Unlock()
	if i := slices.Index(c.Owner.clients, c); i != -1 {
		c.Owner.clients = slices.Delete(c.Owner.clients, i, i+1)
		if c.IsIncomingConnection {
			c.Owner.NumIncomingConnections.Add(-1)
		} else {
			c.Owner.NumOutgoingConnections.Add(-1)
		}
	}

	_ = c.Connection.Close()
	close(c.closeChannel)
}
