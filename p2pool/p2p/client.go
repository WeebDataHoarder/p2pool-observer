package p2p

import (
	"crypto/rand"
	"encoding/binary"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"golang.org/x/exp/slices"
	"log"
	"net"
	"net/netip"
	"sync/atomic"
	"time"
)

type Client struct {
	Owner                *Server
	Connection           net.Conn
	Closed               atomic.Bool
	AddressPort          netip.AddrPort
	LastActive           time.Time
	PeerId               uint64
	IsIncomingConnection bool
	HandshakeComplete    bool

	HandshakeChallenge HandshakeChallenge
}

func NewClient(owner *Server, conn net.Conn) *Client {
	c := &Client{
		Owner:       owner,
		Connection:  conn,
		AddressPort: netip.MustParseAddrPort(conn.RemoteAddr().String()),
		LastActive:  time.Now(),
	}

	return c
}

func (c *Client) OnConnection() {
	c.LastActive = time.Now()

	c.sendHandshakeChallenge()
}

func (c *Client) sendHandshakeChallenge() {
	if _, err := rand.Read(c.HandshakeChallenge[:]); err != nil {
		log.Printf("[P2PServer] Unable to generate handshake challenge for %s", c.AddressPort.String())
		c.Close()
		return
	}

	var buf [1 + HandshakeChallengeSize]byte
	buf[0] = byte(MessageHandshakeChallenge)
	copy(buf[1:], c.HandshakeChallenge[:])

	_, _ = c.Write(buf[:])
}

func (c *Client) sendHandshakeSolution(challenge HandshakeChallenge) {
	stop := &c.Closed
	if c.IsIncomingConnection {
		stop = &atomic.Bool{}
		stop.Store(true)
	}

	if solution, hash, ok := FindChallengeSolution(challenge, c.Owner.Pool().Consensus.Id(), stop); ok {
		var buf [1 + HandshakeChallengeSize + types.HashSize]byte
		buf[0] = byte(MessageHandshakeSolution)
		copy(buf[1:], hash[:])
		binary.LittleEndian.PutUint64(buf[1+types.HashSize:], solution)

		if _, err := c.Write(buf[:]); err != nil {

		}
	}
}

// Write writes to underlying connection, on error it will Close
func (c *Client) Write(buf []byte) (n int, err error) {
	if n, err = c.Connection.Write(buf); err != nil && c.Closed.Load() {
		c.Close()
	}
	return
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
}
