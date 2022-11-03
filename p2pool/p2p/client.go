package p2p

import (
	"crypto/rand"
	"encoding/binary"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"golang.org/x/exp/slices"
	"log"
	"net"
	"net/netip"
	"sync/atomic"
	"time"
	"unsafe"
)

const DefaultBanTime = time.Second * 600

type Client struct {
	Owner                *Server
	Connection           net.Conn
	Closed               atomic.Bool
	AddressPort          netip.AddrPort
	LastActive           time.Time
	LastBroadcast        time.Time
	LastBlockRequest     time.Time
	PeerId               uint64
	IsIncomingConnection bool
	HandshakeComplete    bool
	ListenPort           uint32

	BlockPendingRequests int64
	ChainTipBlockRequest bool

	ExpectedMessage MessageId

	HandshakeChallenge HandshakeChallenge
}

func NewClient(owner *Server, conn net.Conn) *Client {
	c := &Client{
		Owner:           owner,
		Connection:      conn,
		AddressPort:     netip.MustParseAddrPort(conn.RemoteAddr().String()),
		LastActive:      time.Now(),
		ExpectedMessage: MessageHandshakeChallenge,
	}

	return c
}

func (c *Client) Ban(duration time.Duration) {
	c.Owner.Ban(c.AddressPort.Addr(), duration)
	c.Close()
}

func (c *Client) OnAfterHandshake() {
	c.SendListenPort()
	c.SendBlockRequest(types.ZeroHash)
}

func (c *Client) SendListenPort() {
	var buf [1 + int(unsafe.Sizeof(uint32(0)))]byte
	buf[0] = byte(MessageListenPort)
	binary.LittleEndian.PutUint32(buf[1:], uint32(c.Owner.listenAddress.Port()))
	_, _ = c.Write(buf[:])
}

func (c *Client) SendBlockRequest(hash types.Hash) {
	var buf [1 + types.HashSize]byte
	buf[0] = byte(MessageBlockRequest)
	copy(buf[1:], hash[:])

	_, _ = c.Write(buf[:])

	c.BlockPendingRequests++
	if hash == types.ZeroHash {
		c.ChainTipBlockRequest = true
	}
	c.LastBroadcast = time.Now()
}

func (c *Client) SendBlockResponse(block *sidechain.PoolBlock) {
	if block != nil {
		blockData, _ := block.MarshalBinary()
		_, _ = c.Write(append([]byte{byte(MessageBlockResponse)}, blockData...))
	} else {
		_, _ = c.Write([]byte{byte(MessageBlockResponse)})
	}
}

func (c *Client) SendPeerListRequest() {
	return
	//TODO
	_, _ = c.Write([]byte{byte(MessagePeerListRequest)})
}

func (c *Client) OnConnection() {
	c.LastActive = time.Now()

	c.sendHandshakeChallenge()

	for !c.Closed.Load() {
		var messageId MessageId
		if err := binary.Read(c, binary.LittleEndian, &messageId); err != nil {
			c.Ban(DefaultBanTime)
			return
		}

		if !c.HandshakeComplete && messageId != c.ExpectedMessage {
			c.Ban(DefaultBanTime)
			return
		}

		switch messageId {
		case MessageHandshakeChallenge:
			if c.HandshakeComplete {
				c.Ban(DefaultBanTime)
				return
			}

			var challenge HandshakeChallenge
			var peerId uint64
			if err := binary.Read(c, binary.LittleEndian, &challenge); err != nil {
				c.Ban(DefaultBanTime)
				return
			}
			if err := binary.Read(c, binary.LittleEndian, &peerId); err != nil {
				c.Ban(DefaultBanTime)
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

			c.ExpectedMessage = MessageHandshakeSolution

			c.OnAfterHandshake()

		case MessageHandshakeSolution:
			if c.HandshakeComplete {
				c.Ban(DefaultBanTime)
				return
			}

			var challengeHash types.Hash
			var solution uint64
			if err := binary.Read(c, binary.LittleEndian, &challengeHash); err != nil {
				c.Ban(DefaultBanTime)
				return
			}
			if err := binary.Read(c, binary.LittleEndian, &solution); err != nil {
				c.Ban(DefaultBanTime)
				return
			}

			if c.IsIncomingConnection {
				if hash, ok := CalculateChallengeHash(c.HandshakeChallenge, c.Owner.Consensus().Id(), solution); !ok {
					//not enough PoW
					c.Ban(DefaultBanTime)
					return
				} else if hash != challengeHash {
					//wrong hash
					c.Ban(DefaultBanTime)
					return
				}
			} else {
				if hash, _ := CalculateChallengeHash(c.HandshakeChallenge, c.Owner.Consensus().Id(), solution); hash != challengeHash {
					//wrong hash
					c.Ban(DefaultBanTime)
					return
				}
			}
			c.HandshakeComplete = true

		case MessageListenPort:
			if c.ListenPort != 0 {
				c.Ban(DefaultBanTime)
				return
			}

			if err := binary.Read(c, binary.LittleEndian, &c.ListenPort); err != nil {
				c.Ban(DefaultBanTime)
				return
			}

			if c.ListenPort == 0 || c.ListenPort >= 65536 {
				c.Ban(DefaultBanTime)
				return
			}
		case MessageBlockRequest:
			c.LastBlockRequest = time.Now()

			var templateId types.Hash
			if err := binary.Read(c, binary.LittleEndian, &templateId); err != nil {
				c.Ban(DefaultBanTime)
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
			block := &sidechain.PoolBlock{}

			var blockSize uint32
			if err := binary.Read(c, binary.LittleEndian, &blockSize); err != nil {
				//TODO warn
				c.Ban(DefaultBanTime)
				return
			} else if err = block.FromReader(c); err != nil {
				//TODO warn
				c.Ban(DefaultBanTime)
				return
			} else {
				if c.ChainTipBlockRequest {
					c.ChainTipBlockRequest = false

					//peerHeight := block.Main.Coinbase.GenHeight
					//ourHeight :=
					//TODO: stale block

					c.SendPeerListRequest()
				}

				if err = c.Owner.SideChain().AddPoolBlockExternal(block); err != nil {
					//TODO warn
					c.Ban(DefaultBanTime)
					return
				}

				//TODO:
			}

		case MessageBlockBroadcast:
			block := &sidechain.PoolBlock{}
			var blockSize uint32
			if err := binary.Read(c, binary.LittleEndian, &blockSize); err != nil {
				//TODO warn
				c.Ban(DefaultBanTime)
				return
			} else if err = block.FromReader(c); err != nil {
				//TODO warn
				c.Ban(DefaultBanTime)
				return
			}

			if err := c.Owner.SideChain().PreprocessBlock(block); err != nil {
				//TODO warn
				c.Ban(DefaultBanTime)
				return
			}

			//TODO: investigate different monero block mining

			block.WantBroadcast.Store(true)
			c.LastBroadcast = time.Now()
			if err := c.Owner.SideChain().AddPoolBlockExternal(block); err != nil {
				//TODO warn
				c.Ban(DefaultBanTime)
				return
			}
		case MessagePeerListRequest:
			//TODO
		case MessagePeerListResponse:
			//TODO
			fallthrough
		default:
			c.Ban(DefaultBanTime)
		}
	}
}

func (c *Client) sendHandshakeChallenge() {
	if _, err := rand.Read(c.HandshakeChallenge[:]); err != nil {
		log.Printf("[P2PServer] Unable to generate handshake challenge for %s", c.AddressPort.String())
		c.Close()
		return
	}

	var buf [1 + HandshakeChallengeSize + int(unsafe.Sizeof(uint64(0)))]byte
	buf[0] = byte(MessageHandshakeChallenge)
	copy(buf[1:], c.HandshakeChallenge[:])
	binary.LittleEndian.PutUint64(buf[1+HandshakeChallengeSize:], c.Owner.PeerId())

	_, _ = c.Write(buf[:])
}

func (c *Client) sendHandshakeSolution(challenge HandshakeChallenge) {
	stop := &c.Closed
	if c.IsIncomingConnection {
		stop = &atomic.Bool{}
		stop.Store(true)
	}

	if solution, hash, ok := FindChallengeSolution(challenge, c.Owner.Consensus().Id(), stop); ok {
		var buf [1 + HandshakeChallengeSize + types.HashSize]byte
		buf[0] = byte(MessageHandshakeSolution)
		copy(buf[1:], hash[:])
		binary.LittleEndian.PutUint64(buf[1+types.HashSize:], solution)

		_, _ = c.Write(buf[:])
	}
}

// Write writes to underlying connection, on error it will Close
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
	}

	_ = c.Connection.Close()
}
