package client

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"git.gammaspectra.live/P2Pool/go-monero/pkg/rpc"
	"git.gammaspectra.live/P2Pool/go-monero/pkg/rpc/daemon"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/block"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

var client atomic.Pointer[Client]

var lock sync.Mutex

var address = "http://localhost:18081"

func SetClientSettings(addr string) {
	lock.Lock()
	defer lock.Unlock()
	address = addr
	client.Store(nil)
}

func GetClient() *Client {
	if c := client.Load(); c == nil {
		lock.Lock()
		defer lock.Unlock()
		if c = client.Load(); c == nil {
			//fallback for lock racing
			if c, err := newClient(); err != nil {
				log.Panic(err)
			} else {
				client.Store(c)
				return c
			}
		}
		return c
	} else {
		return c
	}
}

// Client TODO: ratelimit
type Client struct {
	c         *rpc.Client
	d         *daemon.Client
	throttler <-chan time.Time
}

func newClient() (*Client, error) {
	c, err := rpc.NewClient(address)
	if err != nil {
		return nil, err
	}
	return &Client{
		c:         c,
		d:         daemon.NewClient(c),
		throttler: time.Tick(time.Second / 2),
	}, nil
}

func (c *Client) GetCoinbaseTransaction(txId types.Hash) (*block.CoinbaseTransaction, error) {
	<-c.throttler
	if result, err := c.d.GetTransactions(context.Background(), []string{txId.String()}); err != nil {
		return nil, err
	} else {
		if len(result.Txs) != 1 {
			return nil, errors.New("invalid transaction count")
		}

		if buf, err := hex.DecodeString(result.Txs[0].PrunedAsHex); err != nil {
			return nil, err
		} else {
			tx := &block.CoinbaseTransaction{}
			if err = tx.FromReader(bytes.NewReader(buf)); err != nil {
				return nil, err
			}

			if tx.Id() != txId {
				return nil, fmt.Errorf("expected %s, got %s", txId.String(), tx.Id().String())
			}

			return tx, nil
		}
	}
}

func (c *Client) GetBlockHeaderByHeight(height uint64) (*daemon.GetBlockHeaderByHeightResult, error) {
	<-c.throttler
	if result, err := c.d.GetBlockHeaderByHeight(context.Background(), height); err != nil {
		return nil, err
	} else {
		return result, nil
	}
}

func (c *Client) GetBlockTemplate(address string) (*daemon.GetBlockTemplateResult, error) {
	<-c.throttler
	if result, err := c.d.GetBlockTemplate(context.Background(), address, 60); err != nil {
		return nil, err
	} else {
		return result, nil
	}
}
