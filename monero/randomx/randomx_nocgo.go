//go:build !cgo || disable_randomx_library

package randomx

import (
	"bytes"
	"git.gammaspectra.live/P2Pool/go-randomx"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"github.com/go-faster/xor"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"
)

type hasher struct {
	cache *randomx.Randomx_Cache
	lock  sync.Mutex

	key []byte
	vm  *randomx.VM
}

func ConsensusHash(buf []byte) types.Hash {
	cache := randomx.Randomx_alloc_cache(0)
	cache.Randomx_init_cache(buf)

	scratchpad := unsafe.Slice((*byte)(unsafe.Pointer(&cache.Blocks[0])), len(cache.Blocks)*len(cache.Blocks[0])*int(unsafe.Sizeof(uint64(0))))
	defer runtime.KeepAlive(cache)

	// Intentionally not a power of 2
	const ScratchpadSize = 1009

	const RandomxArgonMemory = 262144
	n := RandomxArgonMemory * 1024

	const Vec128Size = 128 / 8

	type Vec128 [Vec128Size]byte

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
	return &hasher{
		cache: randomx.Randomx_alloc_cache(0),
	}
}

func (h *hasher) Hash(key []byte, input []byte) (output types.Hash, err error) {
	h.lock.Lock()
	defer h.lock.Unlock()

	if h.key == nil || bytes.Compare(h.key, key) != 0 {
		h.key = make([]byte, len(key))
		copy(h.key, key)

		h.cache.Randomx_init_cache(h.key)

		gen := randomx.Init_Blake2Generator(h.key, 0)
		for i := 0; i < 8; i++ {
			h.cache.Programs[i] = randomx.Build_SuperScalar_Program(gen)
		}
		h.vm = h.cache.VM_Initialize()
	}

	outputBuf := make([]byte, types.HashSize)
	h.vm.CalculateHash(input, outputBuf)
	copy(output[:], outputBuf)
	return
}

func (h *hasher) Close() {

}
