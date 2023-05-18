package legacy

import (
	"encoding/binary"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/cache"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"io"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

const blockSize = 96 * 1024
const numBlocks = 4608
const cacheSize = blockSize * numBlocks

type Cache struct {
	f                 *os.File
	flushRunning      atomic.Bool
	storeIndex        atomic.Uint32
	loadingStarted    sync.Once
	loadingInProgress atomic.Bool
	consensus         *sidechain.Consensus
}

func NewCache(consensus *sidechain.Consensus, path string) (*Cache, error) {
	if f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0666); err != nil {
		return nil, err
	} else {
		if _, err = f.Seek(cacheSize-1, io.SeekStart); err != nil {
			_ = f.Close()
			return nil, err
		}
		//create sparse file
		if _, err = f.Write([]byte{0}); err != nil {
			_ = f.Close()
			return nil, err
		}

		return &Cache{
			f:         f,
			consensus: consensus,
		}, nil
	}
}

func (c *Cache) Store(block *sidechain.PoolBlock) {
	if c.loadingInProgress.Load() {
		return
	}
	if blob, err := block.AppendBinaryFlags(make([]byte, 0, block.BufferLength()), false, false); err != nil {
		return
	} else {
		if (len(blob) + 4) > blockSize {
			//block too big
			return
		}
		storeIndex := (c.storeIndex.Add(1) % numBlocks) * blockSize
		_, _ = c.f.WriteAt(binary.LittleEndian.AppendUint32(nil, uint32(len(blob))), int64(storeIndex))
		_, _ = c.f.WriteAt(blob, int64(storeIndex)+4)
	}
}

func (c *Cache) LoadAll(l cache.Loadee) {
	c.loadingStarted.Do(func() {
		c.loadingInProgress.Store(true)
		defer c.loadingInProgress.Store(false)
		log.Print("[Cache] Loading cached blocks")

		var blobLen [4]byte
		buf := make([]byte, 0, blockSize)

		var blocksLoaded int
		for i := 0; i < numBlocks; i++ {
			storeIndex := (c.storeIndex.Add(1) % numBlocks) * blockSize

			if _, err := c.f.ReadAt(blobLen[:], int64(storeIndex)); err != nil {
				return
			}
			blobLength := binary.LittleEndian.Uint32(blobLen[:])
			if (blobLength + 4) > blockSize {
				//block too big
				continue
			}
			if _, err := c.f.ReadAt(buf[:blobLength], int64(storeIndex)+4); err != nil {
				continue
			}

			block := &sidechain.PoolBlock{
				LocalTimestamp: uint64(time.Now().Unix()),
			}

			if err := block.UnmarshalBinary(c.consensus, &sidechain.NilDerivationCache{}, buf[:blobLength]); err != nil {
				continue
			}

			l.AddCachedBlock(block)

			blocksLoaded++
		}

		log.Printf("[Cache] Loaded %d cached blocks", blocksLoaded)
	})
}

func (c *Cache) Close() {
	_ = c.f.Close()
}

func (c *Cache) Flush() {
	if !c.flushRunning.Swap(true) {
		defer c.flushRunning.Store(false)
		_ = c.f.Sync()
	}
}
