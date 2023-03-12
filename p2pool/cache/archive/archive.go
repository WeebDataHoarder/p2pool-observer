package archive

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	bolt "go.etcd.io/bbolt"
	"golang.org/x/exp/slices"
	"log"
	"math"
	"time"
	"unsafe"
)

// EpochSize Maximum amount of blocks without a full one
const EpochSize = 256

type Cache struct {
	db        *bolt.DB
	consensus *sidechain.Consensus
}

var blocksByMainId = []byte("blocksByMainId")
var refByTemplateId = []byte("refByTemplateId")
var refBySideHeight = []byte("refBySideHeight")
var refByMainHeight = []byte("refByMainHeight")

func NewCache(path string, consensus *sidechain.Consensus) (*Cache, error) {
	if db, err := bolt.Open(path, 0666, &bolt.Options{Timeout: time.Second * 5}); err != nil {
		return nil, err
	} else {
		if err = db.Update(func(tx *bolt.Tx) error {
			if _, err := tx.CreateBucketIfNotExists(blocksByMainId); err != nil {
				return err
			} else if _, err := tx.CreateBucketIfNotExists(refByTemplateId); err != nil {
				return err
			} else if _, err := tx.CreateBucketIfNotExists(refBySideHeight); err != nil {
				return err
			} else if _, err := tx.CreateBucketIfNotExists(refByMainHeight); err != nil {
				return err
			}
			return nil
		}); err != nil {
			return nil, err
		}
		return &Cache{
			db:        db,
			consensus: consensus,
		}, nil
	}
}

type multiRecord []types.Hash

func multiRecordFromBytes(b []byte) multiRecord {
	if len(b) == 0 || (len(b)%types.HashSize) != 0 {
		return nil
	}

	return slices.Clone(unsafe.Slice((*types.Hash)(unsafe.Pointer(&b[0])), len(b)/types.HashSize))
}

func (r multiRecord) Contains(hash types.Hash) bool {
	return slices.Contains(r, hash)
}

func (r multiRecord) Bytes() []byte {
	if len(r) == 0 {
		return nil
	}
	return slices.Clone(unsafe.Slice((*byte)(unsafe.Pointer(&r[0])), len(r)*types.HashSize))
}

func (c *Cache) Store(block *sidechain.PoolBlock) {
	sideId := block.SideTemplateId(c.consensus)
	mainId := block.MainId()
	var sideHeight, mainHeight [8]byte
	binary.BigEndian.PutUint64(sideHeight[:], block.Side.Height)
	binary.BigEndian.PutUint64(mainHeight[:], block.Main.Coinbase.GenHeight)

	if c.existsByMainId(mainId) {
		return
	}

	var storePruned, storeCompact bool

	fullBlockTemplateHeight := block.Side.Height - (block.Side.Height % EpochSize)

	// store full blocks on epoch
	if block.Side.Height != fullBlockTemplateHeight {
		if len(block.Main.Transactions) == len(block.Main.TransactionParentIndices) && c.loadByTemplateId(block.Side.Parent) != nil {
			storeCompact = true
		}

		if block.Depth.Load() == math.MaxUint64 {
			//fallback
			if c.existsBySideChainHeightRange(block.Side.Height-c.consensus.ChainWindowSize-1, block.Side.Height-1) {
				storePruned = true
			}
		} else if block.Depth.Load() < c.consensus.ChainWindowSize {
			storePruned = true
		}
	}

	if blob, err := block.MarshalBinaryFlags(storePruned, storeCompact); err == nil {
		log.Printf("[Archive Cache] Store block id = %s, template id = %s, height = %d, sidechain height %d, pruned = %t, compact = %t, blob size = %d bytes", mainId.String(), sideId.String(), block.Main.Coinbase.GenHeight, block.Side.Height, storePruned, storeCompact, len(blob))

		if err = c.db.Update(func(tx *bolt.Tx) error {
			b1 := tx.Bucket(blocksByMainId)

			var flags uint64
			if storePruned {
				flags |= 0b1
			}
			if storeCompact {
				flags |= 0b10
			}
			buf := make([]byte, 0, len(blob)+8)
			binary.LittleEndian.AppendUint64(buf, flags)
			if err = b1.Put(mainId[:], append(buf, blob...)); err != nil {
				return err
			}
			b2 := tx.Bucket(refByTemplateId)
			if records := multiRecordFromBytes(b2.Get(sideId[:])); !records.Contains(mainId) {
				records = append(records, mainId)
				if err = b2.Put(sideId[:], records.Bytes()); err != nil {
					return err
				}
			}
			b3 := tx.Bucket(refBySideHeight)
			if records := multiRecordFromBytes(b3.Get(sideHeight[:])); !records.Contains(mainId) {
				records = append(records, mainId)
				if err = b3.Put(sideHeight[:], records.Bytes()); err != nil {
					return err
				}
			}
			b4 := tx.Bucket(refByMainHeight)
			if records := multiRecordFromBytes(b4.Get(mainHeight[:])); !records.Contains(mainId) {
				records = append(records, mainId)
				if err = b4.Put(mainHeight[:], records.Bytes()); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			log.Printf("[Archive Cache] bolt error: %s", err)
		}
	}
}

func (c *Cache) RemoveByMainId(id types.Hash) {
	//TODO
}
func (c *Cache) RemoveByTemplateId(id types.Hash) {
	//TODO
}

func (c *Cache) existsByMainId(id types.Hash) (result bool) {
	_ = c.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(blocksByMainId)
		if b.Get(id[:]) != nil {
			result = true
		}
		return nil
	})
	return result
}

func (c *Cache) loadByMainId(tx *bolt.Tx, id types.Hash) []byte {
	b := tx.Bucket(blocksByMainId)
	return b.Get(id[:])
}

func (c *Cache) decodeBlock(blob []byte) *sidechain.PoolBlock {
	if blob == nil {
		return nil
	}

	flags := binary.LittleEndian.Uint64(blob)

	b := &sidechain.PoolBlock{
		NetworkType: c.consensus.NetworkType,
	}
	reader := bytes.NewReader(blob[8:])
	if (flags & 0b10) > 0 {
		if err := b.FromCompactReader(reader); err != nil {
			log.Printf("[Archive Cache] error decoding block: %s", err)
			return nil
		}
	} else {
		if err := b.FromReader(reader); err != nil {
			log.Printf("[Archive Cache] error decoding block: %s", err)
			return nil
		}
	}

	return b
}

func (c *Cache) LoadByMainId(id types.Hash) *sidechain.PoolBlock {
	var blob []byte
	_ = c.db.View(func(tx *bolt.Tx) error {
		blob = c.loadByMainId(tx, id)
		return nil
	})
	return c.decodeBlock(blob)
}

func (c *Cache) loadByTemplateId(id types.Hash) (r multiRecord) {
	_ = c.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(refByTemplateId)
		r = multiRecordFromBytes(b.Get(id[:]))
		return nil
	})
	return r
}

func (c *Cache) LoadByTemplateId(id types.Hash) (result []*sidechain.PoolBlock) {
	blocks := make([][]byte, 0, 1)
	if err := c.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(refByTemplateId)
		r := multiRecordFromBytes(b.Get(id[:]))
		for _, h := range r {
			if e := c.loadByMainId(tx, h); e != nil {
				blocks = append(blocks, e)
			} else {
				return fmt.Errorf("could not find block %s", h.String())
			}
		}
		return nil
	}); err != nil {
		log.Printf("[Archive Cache] error fetching blocks with template id %s, %s", id.String(), err)
		return nil
	}
	for _, buf := range blocks {
		if b := c.decodeBlock(buf); b != nil {
			result = append(result, b)
		}
	}
	return result
}

func (c *Cache) existsBySideChainHeightRange(startHeight, endHeight uint64) (result bool) {
	_ = c.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(refBySideHeight)
		var startHeightBytes, endHeightBytes [8]byte

		cursor := b.Cursor()
		binary.BigEndian.PutUint64(startHeightBytes[:], startHeight)
		binary.BigEndian.PutUint64(endHeightBytes[:], endHeight)

		expectedHeight := startHeight
		k, v := cursor.Seek(startHeightBytes[:])
		for {
			if k == nil {
				return nil
			}
			h := binary.BigEndian.Uint64(k)
			//log.Printf("height check for %d -> %d: %d, expected %d, len %d", startHeight, endHeight, h, expectedHeight, len(v))
			if v == nil || h != expectedHeight {
				return nil
			}
			if bytes.Compare(k, endHeightBytes[:]) > 0 {
				return nil
			} else if bytes.Compare(k, endHeightBytes[:]) == 0 {
				break
			}
			expectedHeight++
			k, v = cursor.Next()
		}
		result = true
		return nil
	})
	return result
}

func (c *Cache) LoadBySideChainHeight(height uint64) (result []*sidechain.PoolBlock) {

	var sideHeight [8]byte
	binary.BigEndian.PutUint64(sideHeight[:], height)

	blocks := make([][]byte, 0, 1)
	if err := c.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(refBySideHeight)
		r := multiRecordFromBytes(b.Get(sideHeight[:]))
		for _, h := range r {
			if e := c.loadByMainId(tx, h); e != nil {
				blocks = append(blocks, e)
			} else {
				return fmt.Errorf("could not find block %s", h.String())
			}
		}
		return nil
	}); err != nil {
		log.Printf("[Archive Cache] error fetching blocks with sidechain height %d, %s", height, err)
		return nil
	}
	for _, buf := range blocks {
		if b := c.decodeBlock(buf); b != nil {
			result = append(result, b)
		}
	}
	return result
}
func (c *Cache) LoadByMainChainHeight(height uint64) (result []*sidechain.PoolBlock) {
	var mainHeight [8]byte
	binary.BigEndian.PutUint64(mainHeight[:], height)

	blocks := make([][]byte, 0, 1)
	if err := c.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(refByMainHeight)
		r := multiRecordFromBytes(b.Get(mainHeight[:]))
		for _, h := range r {
			if e := c.loadByMainId(tx, h); e != nil {
				blocks = append(blocks, e)
			} else {
				return fmt.Errorf("could not find block %s", h.String())
			}
		}
		return nil
	}); err != nil {
		log.Printf("[Archive Cache] error fetching blocks with sidechain height %d, %s", height, err)
		return nil
	}
	for _, buf := range blocks {
		if b := c.decodeBlock(buf); b != nil {
			result = append(result, b)
		}
	}
	return result
}

func (c *Cache) Close() {
	_ = c.db.Close()
}
