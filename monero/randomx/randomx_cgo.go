//go:build cgo && !disable_randomx_library

package randomx

import (
	"bytes"
	"encoding/hex"
	"errors"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/randomx-go-bindings"
	"github.com/go-faster/xor"
	"log"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"
)

type hasherCollection struct {
	lock  sync.RWMutex
	index int
	cache []*hasherState
}

func (h *hasherCollection) Hash(key []byte, input []byte) (types.Hash, error) {
	if hash, err := func() (types.Hash, error) {
		h.lock.RLock()
		defer h.lock.RUnlock()
		for _, c := range h.cache {
			if len(c.key) > 0 && bytes.Compare(c.key, key) == 0 {
				return c.Hash(input), nil
			}
		}

		return types.ZeroHash, errors.New("no hasher")
	}(); err == nil && hash != types.ZeroHash {
		return hash, nil
	} else {
		h.lock.Lock()
		defer h.lock.Unlock()
		index := h.index
		h.index = (h.index + 1) % len(h.cache)
		if err = h.cache[index].Init(key); err != nil {
			return types.ZeroHash, err
		}
		return h.cache[index].Hash(input), nil
	}
}

func (h *hasherCollection) Close() {
	h.lock.RLock()
	defer h.lock.RUnlock()
	for _, c := range h.cache {
		c.Close()
	}
}

type hasherState struct {
	lock    sync.Mutex
	dataset *randomx.RxDataset
	vm      *randomx.RxVM
	key     []byte
}

func ConsensusHash(buf []byte) types.Hash {
	dataset, err := randomx.AllocDataset(randomx.FlagDefault)
	if err != nil {
		return types.ZeroHash
	}
	defer randomx.ReleaseDataset(dataset)
	cache, err := randomx.AllocCache(randomx.FlagDefault)
	if err != nil {
		return types.ZeroHash
	}
	defer randomx.ReleaseCache(cache)

	randomx.InitCache(cache, buf)
	// Intentionally not a power of 2
	const ScratchpadSize = 1009

	const RandomxArgonMemory = 262144
	n := RandomxArgonMemory * 1024

	const Vec128Size = 128 / 8

	type Vec128 [Vec128Size]byte
	scratchpad := unsafe.Slice((*byte)(randomx.GetCacheMemory(cache)), n)

	cachePtr := scratchpad[ScratchpadSize*Vec128Size:]
	scratchpadTopPtr := scratchpad[:ScratchpadSize*Vec128Size]
	for i := ScratchpadSize * Vec128Size; i < n; i += ScratchpadSize * Vec128Size {
		stride := ScratchpadSize * Vec128Size
		if stride > len(cachePtr) {
			stride = len(cachePtr)
		}
		xor.Bytes(scratchpadTopPtr, scratchpadTopPtr, cachePtr[:stride])
		cachePtr = cachePtr[stride:]
	}

	return crypto.Keccak256(scratchpadTopPtr)
}

var UseFullMemory atomic.Bool

func NewRandomX() Hasher {
	collection := &hasherCollection{
		cache: make([]*hasherState, 4),
	}

	var err error

	for i := range collection.cache {
		if collection.cache[i], err = newRandomXState(); err != nil {
			return nil
		}
	}
	return collection
}

func newRandomXState() (*hasherState, error) {

	h := &hasherState{}
	if dataset, err := randomx.NewRxDataset(randomx.FlagJIT); err != nil {
		return nil, err
	} else {
		h.dataset = dataset
	}

	return h, nil
}

func (h *hasherState) Init(key []byte) (err error) {
	h.lock.Lock()
	defer h.lock.Unlock()
	h.key = make([]byte, len(key))
	copy(h.key, key)

	log.Printf("[RandomX] Initializing to seed %s", hex.EncodeToString(h.key))
	if h.dataset.GoInit(h.key, uint32(runtime.NumCPU())) == false {
		return errors.New("could not initialize dataset")
	}
	if h.vm != nil {
		h.vm.Close()
	}

	if UseFullMemory.Load() {
		if h.vm, err = randomx.NewRxVM(h.dataset, randomx.FlagFullMEM, randomx.FlagHardAES, randomx.FlagJIT, randomx.FlagSecure); err != nil {
			return err
		}
	} else {
		if h.vm, err = randomx.NewRxVM(h.dataset, randomx.FlagHardAES, randomx.FlagJIT, randomx.FlagSecure); err != nil {
			return err
		}
	}
	log.Printf("[RandomX] Initialized to seed %s", hex.EncodeToString(h.key))

	return nil
}

func (h *hasherState) Hash(input []byte) (output types.Hash) {
	h.lock.Lock()
	defer h.lock.Unlock()
	outputBuf := h.vm.CalcHash(input)
	copy(output[:], outputBuf)
	runtime.KeepAlive(input)
	return
}

func (h *hasherState) Close() {
	if h.vm != nil {
		h.vm.Close()
	}
	h.dataset.Close()
}
