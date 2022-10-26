//go:build cgo && !disable_randomx_library

package randomx

import (
	"bytes"
	"errors"
	"git.gammaspectra.live/P2Pool/moneroutil"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/randomx-go-bindings"
	"github.com/go-faster/xor"
	"runtime"
	"sync"
	"unsafe"
)

type hasher struct {
	lock sync.Mutex

	dataset *randomx.RxDataset
	vm      *randomx.RxVM
	key     []byte
}

func ConsensusHash(buf []byte) types.Hash {
	dataset, err := randomx.AllocDataset(randomx.FlagDefault)
	if err != nil {
		return types.Hash{}
	}
	defer randomx.ReleaseDataset(dataset)
	cache, err := randomx.AllocCache(randomx.FlagDefault)
	if err != nil {
		return types.Hash{}
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

	return types.Hash(moneroutil.Keccak256(scratchpadTopPtr))
}

func NewRandomX() Hasher {

	h := &hasher{}
	if dataset, err := randomx.NewRxDataset(randomx.FlagJIT); err != nil {
		return nil
	} else {

		h.dataset = dataset
	}

	return h
}

func (h *hasher) Hash(key []byte, input []byte) (output types.Hash, err error) {
	h.lock.Lock()
	defer h.lock.Unlock()

	if h.key == nil || bytes.Compare(h.key, key) != 0 {
		h.key = make([]byte, len(key))
		copy(h.key, key)

		if h.dataset.GoInit(h.key, uint32(runtime.NumGoroutine())) == false {
			return types.Hash{}, errors.New("could not initialize dataset")
		}
		if h.vm != nil {
			h.vm.Close()
		}

		if h.vm, err = randomx.NewRxVM(h.dataset, randomx.FlagFullMEM, randomx.FlagHardAES, randomx.FlagJIT, randomx.FlagSecure); err != nil {
			return types.Hash{}, err
		}
	}

	outputBuf := h.vm.CalcHash(input)
	copy(output[:], outputBuf)
	return
}

func (h *hasher) Close() {
	if h.vm != nil {
		h.vm.Close()
	}
	h.dataset.Close()
}
