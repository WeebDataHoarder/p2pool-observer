package archive

import (
	"encoding/binary"
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
	binary.LittleEndian.PutUint64(sideHeight[:], block.Side.Height)
	binary.LittleEndian.PutUint64(mainHeight[:], block.Main.Coinbase.GenHeight)

	if c.existsByMainId(mainId) {
		return
	}

	var storePruned, storeCompact bool

	fullBlockTemplateHeight := block.Side.Height - (block.Side.Height % EpochSize)

	// store full blocks on epoch
	if block.Side.Height != fullBlockTemplateHeight {
		if len(block.Main.Transactions) == len(block.Main.TransactionParentIndices) && c.existsByTemplateId(block.Side.Parent) {
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
			if err = b1.Put(mainId[:], blob); err != nil {
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

func (c *Cache) LoadByMainId(id types.Hash) *sidechain.PoolBlock {
	//TODO
	return nil
}

func (c *Cache) existsByTemplateId(id types.Hash) (result bool) {
	_ = c.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(refByTemplateId)
		if b.Get(id[:]) != nil {
			result = true
		}
		return nil
	})
	return result
}

func (c *Cache) LoadByTemplateId(id types.Hash) []*sidechain.PoolBlock {
	//TODO
	return nil
}

func (c *Cache) existsBySideChainHeightRange(startHeight, endHeight uint64) (result bool) {
	_ = c.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(refBySideHeight)
		var sideHeight [8]byte
		for h := startHeight; h <= endHeight; h++ {
			binary.LittleEndian.PutUint64(sideHeight[:], h)
			if b.Get(sideHeight[:]) == nil {
				return nil
			}
		}
		result = true
		return nil
	})
	return result
}

func (c *Cache) LoadBySideChainHeight(height uint64) []*sidechain.PoolBlock {
	//TODO
	return nil
}
func (c *Cache) LoadByMainChainHeight(height uint64) []*sidechain.PoolBlock {
	//TODO
	return nil
}

func (c *Cache) Close() {
	_ = c.db.Close()
}
