package client

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"git.gammaspectra.live/P2Pool/go-monero/pkg/rpc"
	"git.gammaspectra.live/P2Pool/go-monero/pkg/rpc/daemon"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/randomx"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/transaction"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"github.com/floatdrop/lru"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

var client atomic.Pointer[Client]

var lock sync.Mutex

var address = "http://localhost:18081"

func SetClientSettings(addr string) {
	if addr == "" {
		return
	}
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
	c *rpc.Client
	d *daemon.Client

	difficultyCache *lru.LRU[uint64, types.Difficulty]

	seedCache *lru.LRU[uint64, types.Hash]

	coinbaseTransactionCache *lru.LRU[types.Hash, *transaction.CoinbaseTransaction]

	throttler <-chan time.Time
}

func newClient() (*Client, error) {
	c, err := rpc.NewClient(address)
	if err != nil {
		return nil, err
	}
	return &Client{
		c:                        c,
		d:                        daemon.NewClient(c),
		difficultyCache:          lru.New[uint64, types.Difficulty](1024),
		seedCache:                lru.New[uint64, types.Hash](1024),
		coinbaseTransactionCache: lru.New[types.Hash, *transaction.CoinbaseTransaction](1024),
		throttler:                time.Tick(time.Second / 2),
	}, nil
}

func (c *Client) GetCoinbaseTransaction(txId types.Hash) (*transaction.CoinbaseTransaction, error) {
	if tx := c.coinbaseTransactionCache.Get(txId); tx == nil {
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
				tx := &transaction.CoinbaseTransaction{}
				if err = tx.UnmarshalBinary(buf); err != nil {
					return nil, err
				}

				if tx.Id() != txId {
					return nil, fmt.Errorf("expected %s, got %s", txId.String(), tx.Id().String())
				}

				c.coinbaseTransactionCache.Set(txId, tx)

				return tx, nil
			}
		}
	} else {
		return *tx, nil
	}
}

func (c *Client) GetBlockIdByHeight(height uint64) (types.Hash, error) {
	if r, err := c.GetBlockHeaderByHeight(height); err != nil {
		return types.Hash{}, err
	} else {
		if h, err := types.HashFromString(r.BlockHeader.Hash); err != nil {
			return types.Hash{}, err
		} else {
			return h, nil
		}
	}
}

func (c *Client) AddSeedByHeightToCache(seedHeight uint64, seed types.Hash) {
	c.seedCache.Set(seedHeight, seed)
}

func (c *Client) GetSeedByHeight(height uint64) (types.Hash, error) {

	seedHeight := randomx.SeedHeight(height)

	if seed := c.seedCache.Get(seedHeight); seed == nil {
		if seed, err := c.GetBlockIdByHeight(seedHeight); err != nil {
			return types.ZeroHash, err
		} else {
			c.AddSeedByHeightToCache(seedHeight, seed)
			return seed, nil
		}
	} else {
		return *seed, nil
	}
}

func (c *Client) GetDifficultyByHeight(height uint64) (types.Difficulty, error) {
	if difficulty := c.difficultyCache.Get(height); difficulty == nil {
		if header, err := c.GetBlockHeaderByHeight(height); err != nil {
			if template, err := c.GetBlockTemplate(types.DonationAddress); err != nil {
				return types.ZeroDifficulty, err
			} else if uint64(template.Height) == height {
				difficulty := types.DifficultyFrom64(uint64(template.Difficulty))
				c.difficultyCache.Set(height, difficulty)
				return difficulty, nil
			} else {
				return types.ZeroDifficulty, errors.New("height not found and is not next template")
			}
		} else {
			difficulty := types.DifficultyFrom64(uint64(header.BlockHeader.Difficulty))
			c.difficultyCache.Set(height, difficulty)
			return difficulty, nil
		}
	} else {
		return *difficulty, nil
	}
}

func (c *Client) GetBlockHeaderByHash(hash types.Hash) (*daemon.GetBlockHeaderByHashResult, error) {
	<-c.throttler
	if result, err := c.d.GetBlockHeaderByHash(context.Background(), []string{hash.String()}); err != nil {
		return nil, err
	} else {
		return result, nil
	}
}

func (c *Client) GetLastBlockHeader() (*daemon.GetLastBlockHeaderResult, error) {
	<-c.throttler
	if result, err := c.d.GetLastBlockHeader(context.Background()); err != nil {
		return nil, err
	} else {
		return result, nil
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
