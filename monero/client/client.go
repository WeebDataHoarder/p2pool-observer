package client

import (
	"bytes"
	"context"
	"encoding/binary"
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

//TODO: ZMQ

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

type TransactionInputResult struct {
	Id         types.Hash
	UnlockTime uint64
	Inputs     []TransactionInput
}

type TransactionInput struct {
	InputType  uint8
	Amount     uint64
	KeyOffsets []uint64
	KeyImage   types.Hash
}

func (c *Client) GetTransactionInputs(hashes ...types.Hash) ([]TransactionInputResult, error) {
	<-c.throttler

	if result, err := c.d.GetTransactions(context.Background(), func() []string {
		result := make([]string, 0, len(hashes))
		for _, h := range hashes {
			result = append(result, h.String())
		}
		return result
	}()); err != nil {
		return nil, err
	} else {
		if len(result.Txs) != len(hashes) {
			return nil, errors.New("invalid transaction count")
		}

		var (
			Version    uint8
			InputCount uint8

			OffsetCount uint8
		)

		s := make([]TransactionInputResult, len(result.Txs))

		for ix := range result.Txs {
			s[ix].Id = hashes[ix]
			if buf, err := hex.DecodeString(result.Txs[ix].AsHex); err != nil {
				return nil, err
			} else {
				reader := bytes.NewReader(buf)
				if Version, err = reader.ReadByte(); err != nil {
					return nil, err
				}

				if Version != 2 {
					return nil, errors.New("version not supported")
				}

				if s[ix].UnlockTime, err = binary.ReadUvarint(reader); err != nil {
					return nil, err
				}

				if InputCount, err = reader.ReadByte(); err != nil {
					return nil, err
				}

				s[ix].Inputs = make([]TransactionInput, InputCount)

				for i := 0; i < int(InputCount); i++ {
					if s[ix].Inputs[i].InputType, err = reader.ReadByte(); err != nil {
						return nil, err
					}
					if s[ix].Inputs[i].Amount, err = binary.ReadUvarint(reader); err != nil {
						return nil, err
					}
					if s[ix].Inputs[i].InputType != transaction.TxInToKey {
						continue
					}
					if OffsetCount, err = reader.ReadByte(); err != nil {
						return nil, err
					}

					s[ix].Inputs[i].KeyOffsets = make([]uint64, OffsetCount)

					for j := 0; j < int(OffsetCount); j++ {
						if s[ix].Inputs[i].KeyOffsets[j], err = binary.ReadUvarint(reader); err != nil {
							return nil, err
						}

						if j > 0 {
							s[ix].Inputs[i].KeyOffsets[j] += s[ix].Inputs[i].KeyOffsets[j-1]
						}
					}

					if err = binary.Read(reader, binary.LittleEndian, &s[ix].Inputs[i].KeyImage); err != nil {
						return nil, err
					}
				}
			}
		}

		return s, nil
	}
}

type Output struct {
	Height        uint64
	Key           types.Hash
	Mask          types.Hash
	TransactionId types.Hash
	Unlocked      bool
}

func (c *Client) GetOuts(inputs ...uint64) ([]Output, error) {
	<-c.throttler

	if result, err := c.d.GetOuts(context.Background(), func() []uint {
		r := make([]uint, len(inputs))
		for i, v := range inputs {
			r[i] = uint(v)
		}
		return r
	}(), true); err != nil {
		return nil, err
	} else {
		if len(result.Outs) != len(inputs) {
			return nil, errors.New("invalid output count")
		}

		s := make([]Output, len(inputs))
		for i := range result.Outs {
			o := &result.Outs[i]
			s[i].Height = o.Height
			s[i].Key, _ = types.HashFromString(o.Key)
			s[i].Mask, _ = types.HashFromString(o.Mask)
			s[i].TransactionId, _ = types.HashFromString(o.Txid)
			s[i].Unlocked = o.Unlocked
		}

		return s, nil
	}
}

func (c *Client) GetBlockIdByHeight(height uint64) (types.Hash, error) {
	if r, err := c.GetBlockHeaderByHeight(height); err != nil {
		return types.ZeroHash, err
	} else {
		if h, err := types.HashFromString(r.BlockHeader.Hash); err != nil {
			return types.ZeroHash, err
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
