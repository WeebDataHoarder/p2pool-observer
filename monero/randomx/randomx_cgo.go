//go:build cgo && !disable_randomx_library

package randomx

import (
	"bytes"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/randomx-go-bindings"
	"log"
	"runtime"
	"slices"
	"sync"
	"unsafe"
)

type hasherCollection struct {
	lock  sync.RWMutex
	index int
	flags []Flag
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

func (h *hasherCollection) initStates(size int) (err error) {
	for _, c := range h.cache {
		c.Close()
	}
	h.cache = make([]*hasherState, size)
	for i := range h.cache {
		if h.cache[i], err = newRandomXState(h.flags...); err != nil {
			return err
		}
	}
	return nil
}

func (h *hasherCollection) OptionFlags(flags ...Flag) error {
	h.lock.Lock()
	defer h.lock.Unlock()
	if slices.Compare(h.flags, flags) != 0 {
		h.flags = flags
		return h.initStates(len(h.cache))
	}
	return nil
}
func (h *hasherCollection) OptionNumberOfCachedStates(n int) error {
	h.lock.Lock()
	defer h.lock.Unlock()
	if len(h.cache) != n {
		return h.initStates(n)
	}
	return nil
}

func (h *hasherCollection) Close() {
	h.lock.Lock()
	defer h.lock.Unlock()
	for _, c := range h.cache {
		c.Close()
	}
}

type hasherState struct {
	lock    sync.Mutex
	dataset *randomx.RxDataset
	vm      *randomx.RxVM
	flags   randomx.Flag
	key     []byte
}

func ConsensusHash(buf []byte) types.Hash {
	cache, err := randomx.AllocCache(randomx.GetFlags())
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
		subtle.XORBytes(scratchpadTopPtr, scratchpadTopPtr, cachePtr[:stride])
		cachePtr = cachePtr[stride:]
	}

	return crypto.Keccak256(scratchpadTopPtr)
}

func NewRandomX(n int, flags ...Flag) (Hasher, error) {
	collection := &hasherCollection{
		flags: flags,
	}

	if err := collection.initStates(n); err != nil {
		return nil, err
	}
	return collection, nil
}

func newRandomXState(flags ...Flag) (*hasherState, error) {

	applyFlags := randomx.GetFlags()
	for _, f := range flags {
		if f == FlagLargePages {
			applyFlags |= randomx.FlagLargePages
		} else if f == FlagFullMemory {
			applyFlags |= randomx.FlagFullMEM
		} else if f == FlagSecure {
			applyFlags |= randomx.FlagSecure
		}
	}
	h := &hasherState{
		flags: applyFlags,
	}
	if dataset, err := randomx.NewRxDataset(h.flags); err != nil {
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

	if h.vm, err = randomx.NewRxVM(h.dataset, h.flags); err != nil {
		return err
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
	h.lock.Lock()
	defer h.lock.Unlock()
	if h.vm != nil {
		h.vm.Close()
	}
	h.dataset.Close()
}
