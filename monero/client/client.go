package client

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"git.gammaspectra.live/P2Pool/go-monero/pkg/levin"
	"git.gammaspectra.live/P2Pool/go-monero/pkg/rpc"
	"git.gammaspectra.live/P2Pool/go-monero/pkg/rpc/daemon"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/transaction"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"github.com/floatdrop/lru"
	"io"
	"log"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

var client atomic.Pointer[Client]

var lock sync.Mutex

var address = "http://localhost:18081"

func SetDefaultClientSettings(addr string) {
	if addr == "" {
		return
	}
	lock.Lock()
	defer lock.Unlock()
	address = addr
	client.Store(nil)
}

func GetDefaultClient() *Client {
	if c := client.Load(); c == nil {
		lock.Lock()
		defer lock.Unlock()
		if c = client.Load(); c == nil {
			//fallback for lock racing
			if c, err := NewClient(address); err != nil {
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

// Client
type Client struct {
	c       *rpc.Client
	d       *daemon.Client
	address string

	coinbaseTransactionCache *lru.LRU[types.Hash, *transaction.CoinbaseTransaction]

	throttler <-chan time.Time
}

func NewClient(address string) (*Client, error) {
	c, err := rpc.NewClient(address)
	if err != nil {
		return nil, err
	}
	return &Client{
		address:                  address,
		c:                        c,
		d:                        daemon.NewClient(c),
		coinbaseTransactionCache: lru.New[types.Hash, *transaction.CoinbaseTransaction](1024),
		throttler:                time.Tick(time.Second / 8),
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

// GetOutputIndexes Get global output indexes
func (c *Client) GetOutputIndexes(id types.Hash) (indexes []uint64, finalError error) {
	<-c.throttler

	uri, _ := url.Parse(c.address)
	uri.Path = "/get_o_indexes.bin"

	storage := levin.PortableStorage{Entries: levin.Entries{
		levin.Entry{
			Name:         "txid",
			Serializable: levin.BoostString(id[:]),
		},
	}}

	data := storage.Bytes()

	body := io.NopCloser(bytes.NewReader(data))
	response, err := http.DefaultClient.Do(&http.Request{
		Method: "POST",
		URL:    uri,
		Header: http.Header{
			"content-type": []string{"application/octet-stream"},
		},
		Body:          body,
		ContentLength: int64(len(data)),
	})
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, fmt.Errorf("non-2xx status code: %d", response.StatusCode)
	}

	if buf, err := io.ReadAll(response.Body); err != nil {
		return nil, err
	} else {
		defer func() {
			if r := recover(); r != nil {
				indexes = nil
				finalError = errors.New("error decoding")
			}
		}()
		responseStorage, err := levin.NewPortableStorageFromBytes(buf)
		if err != nil {
			return nil, err
		}
		for _, e := range responseStorage.Entries {
			if e.Name == "o_indexes" {
				if entries, ok := e.Value.(levin.Entries); ok {
					indexes = make([]uint64, 0, len(entries))
					for _, e2 := range entries {
						if v, ok := e2.Value.(uint64); ok {
							indexes = append(indexes, v)
						}
					}
					return indexes, nil
				}
			}
		}
	}
	return nil, errors.New("could not get outputs")
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

func (c *Client) GetVersion() (*daemon.GetVersionResult, error) {
	<-c.throttler
	if result, err := c.d.GetVersion(context.Background()); err != nil {
		return nil, err
	} else {
		return result, nil
	}
}

func (c *Client) GetInfo() (*daemon.GetInfoResult, error) {
	<-c.throttler
	if result, err := c.d.GetInfo(context.Background()); err != nil {
		return nil, err
	} else {
		return result, nil
	}
}

func (c *Client) GetBlockHeaderByHash(hash types.Hash, ctx context.Context) (*daemon.GetBlockHeaderByHashResult, error) {
	<-c.throttler
	if result, err := c.d.GetBlockHeaderByHash(ctx, []string{hash.String()}); err != nil {
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

func (c *Client) GetBlockHeaderByHeight(height uint64, ctx context.Context) (*daemon.GetBlockHeaderByHeightResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.throttler:
		if result, err := c.d.GetBlockHeaderByHeight(ctx, height); err != nil {
			return nil, err
		} else {
			return result, nil
		}
	}
}

func (c *Client) GetBlockHeadersRangeResult(start, end uint64, ctx context.Context) (*daemon.GetBlockHeadersRangeResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.throttler:
		if result, err := c.d.GetBlockHeadersRange(ctx, start, end); err != nil {
			return nil, err
		} else {
			return result, nil
		}
	}
}

func (c *Client) SubmitBlock(blob []byte) (*daemon.SubmitBlockResult, error) {
	if result, err := c.d.SubmitBlock(context.Background(), blob); err != nil {
		return nil, err
	} else {
		return result, nil
	}
}

func (c *Client) GetMinerData() (*daemon.GetMinerDataResult, error) {
	<-c.throttler
	if result, err := c.d.GetMinerData(context.Background()); err != nil {
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
