package p2p

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"golang.org/x/exp/slices"
	"log"
	"net"
	"net/netip"
	"sync/atomic"
	"time"
	"unsafe"
)

const DefaultBanTime = time.Second * 600
const PeerListResponseMaxPeers = 16

type Client struct {
	Owner                *Server
	Connection           net.Conn
	Closed               atomic.Bool
	AddressPort          netip.AddrPort
	LastActive           time.Time
	LastBroadcast        time.Time
	LastBlockRequest     time.Time
	PingTime time.Duration
	LastPeerListRequestTime time.Time
	PeerId               uint64
	IsIncomingConnection bool
	HandshakeComplete    bool
	ListenPort           uint32

	BlockPendingRequests int64
	ChainTipBlockRequest bool

	expectedMessage MessageId

	handshakeChallenge HandshakeChallenge

	messageChannel chan *ClientMessage

	closeChannel chan struct{}

	blockRequestThrottler <-chan time.Time
}

func NewClient(owner *Server, conn net.Conn) *Client {
	c := &Client{
		Owner:                 owner,
		Connection:            conn,
		AddressPort:           netip.MustParseAddrPort(conn.RemoteAddr().String()),
		LastActive:            time.Now(),
		expectedMessage:       MessageHandshakeChallenge,
		blockRequestThrottler: time.Tick(time.Second / 50), //maximum 50 per second
		messageChannel:        make(chan *ClientMessage, 10),
		closeChannel:        make(chan struct{}),
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
		Buffer: binary.LittleEndian.AppendUint32(nil, uint32(c.Owner.listenAddress.Port())),
	})
}

func (c *Client) SendBlockRequest(hash types.Hash) {
	<-c.blockRequestThrottler

	c.SendMessage(&ClientMessage{
		MessageId: MessageBlockRequest,
		Buffer: hash[:],
	})

	c.BlockPendingRequests++
	if hash == types.ZeroHash {
		c.ChainTipBlockRequest = true
	}
	c.LastBroadcast = time.Now()
}

func (c *Client) SendBlockResponse(block *sidechain.PoolBlock) {
	if block != nil {
		blockData, _ := block.MarshalBinary()

		c.SendMessage(&ClientMessage{
			MessageId: MessageBlockResponse,
			Buffer: append(binary.LittleEndian.AppendUint32(make([]byte, 0, len(blockData)+4), uint32(len(blockData))), blockData...),
		})

	} else {
		c.SendMessage(&ClientMessage{
			MessageId: MessageBlockResponse,
			Buffer: binary.LittleEndian.AppendUint32(nil, 0),
		})
	}
}

func (c *Client) SendBlockBroadcast(block *sidechain.PoolBlock) {
	if block != nil {
		blockData, _ := block.MarshalBinary()

		c.SendMessage(&ClientMessage{
			MessageId: MessageBlockBroadcast,
			Buffer: append(binary.LittleEndian.AppendUint32(make([]byte, 0, len(blockData)+4), uint32(len(blockData))), blockData...),
		})

	} else {
		c.SendMessage(&ClientMessage{
			MessageId: MessageBlockBroadcast,
			Buffer: binary.LittleEndian.AppendUint32(nil, 0),
		})
	}
}

func (c *Client) SendPeerListRequest() {
	c.SendMessage(&ClientMessage{
		MessageId: MessagePeerListRequest,
	})
	c.LastPeerListRequestTime = time.Now()
}

func (c *Client) SendPeerListResponse(list []netip.AddrPort) {
	if len(list) > PeerListResponseMaxPeers {
		return
	}
	buf := make([]byte, 0, 1 + len(list) * (1 + 16 + 2 ))
	buf = append(buf, byte(len(list)))
	for i := range list {
		if list[i].Addr().Is6() {
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
		Buffer: buf,
	})
}

func (c *Client) OnConnection() {
	c.LastActive = time.Now()

	c.sendHandshakeChallenge()

	var missingBlocks []types.Hash

	go func() {
		defer close(c.messageChannel)
		for {
			select {
			case <-c.closeChannel:
				return
			case message := <- c.messageChannel:
				//log.Printf("Sending message %d len %d", message.MessageId, len(message.Buffer))
				_, _ = c.Write([]byte{byte(message.MessageId)})
				_, _ = c.Write(message.Buffer)
			}
		}
	}()

	for !c.Closed.Load() {
		var messageId MessageId
		if err := binary.Read(c, binary.LittleEndian, &messageId); err != nil {
			c.Ban(DefaultBanTime, err)
			return
		}

		if !c.HandshakeComplete && messageId != c.expectedMessage {
			c.Ban(DefaultBanTime, fmt.Errorf("unexpected pre-handshake message: got %d, expected %d", messageId, c.expectedMessage))
			return
		}

		switch messageId {
		case MessageHandshakeChallenge:
			if c.HandshakeComplete {
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
			if c.HandshakeComplete {
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
			c.HandshakeComplete = true

		case MessageListenPort:
			if c.ListenPort != 0 {
				c.Ban(DefaultBanTime, errors.New("got LISTEN_PORT but we already received it"))
				return
			}

			if err := binary.Read(c, binary.LittleEndian, &c.ListenPort); err != nil {
				c.Ban(DefaultBanTime, err)
				return
			}

			if c.ListenPort == 0 || c.ListenPort >= 65536 {
				c.Ban(DefaultBanTime, fmt.Errorf("listen port out of range: %d", c.ListenPort))
				return
			}
		case MessageBlockRequest:
			c.LastBlockRequest = time.Now()

			var templateId types.Hash
			if err := binary.Read(c, binary.LittleEndian, &templateId); err != nil {
				c.Ban(DefaultBanTime, err)
				return
			}

			go func() {
				var block *sidechain.PoolBlock
				//if empty, return chain tip
				if templateId == types.ZeroHash {
					//todo: don't return stale
					block = c.Owner.SideChain().GetChainTip()
				} else {
					block = c.Owner.SideChain().GetPoolBlockByTemplateId(templateId)
				}

				c.SendBlockResponse(block)
			}()
		case MessageBlockResponse:
			block := &sidechain.PoolBlock{}

			var blockSize uint32
			if err := binary.Read(c, binary.LittleEndian, &blockSize); err != nil {
				//TODO warn
				c.Ban(DefaultBanTime, err)
				return
			} else if blockSize == 0 {
				//NOT found
				//TODO log
			} else {
				if err = block.FromReader(c); err != nil {
					//TODO warn
					c.Ban(DefaultBanTime, err)
					return
				} else {
					if c.ChainTipBlockRequest {
						c.ChainTipBlockRequest = false

						//peerHeight := block.Main.Coinbase.GenHeight
						//ourHeight :=
						//TODO: stale block

						c.SendPeerListRequest()
					}

					go func() {
						if missingBlocks, err = c.Owner.SideChain().AddPoolBlockExternal(block); err != nil {
							//TODO warn
							c.Ban(DefaultBanTime, err)
							return
						} else {
							for _, id := range missingBlocks {
								c.SendBlockRequest(id)
							}
						}

					}()
				}
			}

		case MessageBlockBroadcast:
			block := &sidechain.PoolBlock{}
			var blockSize uint32
			if err := binary.Read(c, binary.LittleEndian, &blockSize); err != nil {
				//TODO warn
				c.Ban(DefaultBanTime, err)
				return
			} else if blockSize == 0 {
				//NOT found
				//TODO log
			} else if err = block.FromReader(c); err != nil {
				//TODO warn
				c.Ban(DefaultBanTime, err)
				return
			}
			c.LastBroadcast = time.Now()
			go func() {
				if err := c.Owner.SideChain().PreprocessBlock(block); err != nil {
					//TODO warn
					c.Ban(DefaultBanTime, err)
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
							c.SendBlockRequest(id)
						}
					}
				}
			}()
		case MessagePeerListRequest:
			//TODO
			c.SendPeerListResponse(nil)
		case MessagePeerListResponse:
			if numPeers, err := c.ReadByte(); err != nil {
				c.Ban(DefaultBanTime, err)
				return
			} else if numPeers > PeerListResponseMaxPeers {
				c.Ban(DefaultBanTime, fmt.Errorf("too many peers on PEER_LIST_RESPONSE num_peers = %d", numPeers))
				return
			} else {
				c.PingTime = utils.Max(time.Now().Sub(c.LastPeerListRequestTime), 0)
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
						if isV6 != 0 {
							copy(rawIp[:], make([]byte, 10))
							rawIp[10], rawIp[11] = 0xFF, 0xFF
						}
						c.Owner.AddToPeerList(netip.AddrPortFrom(netip.AddrFrom16(rawIp), port))
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
		Buffer: buf[:],
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
			Buffer: buf[:],
		})
	}
}

// Write writes to underlying connection, on error it will Close. Do not use this directly, Use SendMessage instead.
func (c *Client) Write(buf []byte) (n int, err error) {
	if n, err = c.Connection.Write(buf); err != nil && c.Closed.Load() {
		c.Close()
	}
	return
}

// Read reads from underlying connection, on error it will Close
func (c *Client) Read(buf []byte) (n int, err error) {
	if n, err = c.Connection.Read(buf); err != nil && c.Closed.Load() {
		c.Close()
	}
	return
}

type ClientMessage struct {
	MessageId MessageId
	Buffer []byte
}

func (c *Client) SendMessage(message *ClientMessage) {
	if !c.Closed.Load() {
		c.messageChannel <- message
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
	if c.Closed.Load() {
		return
	}

	if c.Closed.Swap(true) {
		return
	}

	if i, ok := func() (int, bool) {
		c.Owner.clientsLock.RLock()
		defer c.Owner.clientsLock.RUnlock()
		for i, client := range c.Owner.clients {
			if client == c {
				return i, true
			}
		}
		return 0, false
	}(); ok {
		c.Owner.clientsLock.Lock()
		defer c.Owner.clientsLock.Unlock()
		c.Owner.clients = slices.Delete(c.Owner.clients, i, i)
		if c.IsIncomingConnection {
			c.Owner.NumIncomingConnections.Add(-1)
		} else {
			c.Owner.NumOutgoingConnections.Add(-1)
		}
	}

	_ = c.Connection.Close()
	close(c.closeChannel)
}
