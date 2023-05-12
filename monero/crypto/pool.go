package crypto

import (
	"filippo.io/edwards25519"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/sha3"
	"runtime"
	"sync"
)

var hasherPool, pointPool, scalarPool sync.Pool

func init() {
	hasherPool.New = func() any {
		return sha3.NewLegacyKeccak256()
	}
	pointPool.New = func() any {
		p := new(edwards25519.Point)
		runtime.SetFinalizer(p, PutEdwards25519Point)
		return p
	}
	scalarPool.New = func() any {
		s := new(edwards25519.Scalar)
		runtime.SetFinalizer(s, PutEdwards25519Scalar)
		return s
	}
}

func GetKeccak256Hasher() *sha3.HasherState {
	return hasherPool.Get().(*sha3.HasherState)
}

func PutKeccak256Hasher(h *sha3.HasherState) {
	h.Reset()
	hasherPool.Put(h)
}

func PooledKeccak256(data ...[]byte) (result types.Hash) {
	h := GetKeccak256Hasher()
	defer PutKeccak256Hasher(h)
	for _, b := range data {
		h.Write(b)
	}
	HashFastSum(h, result[:])
	return
}

func GetEdwards25519Point() *edwards25519.Point {
	return pointPool.Get().(*edwards25519.Point)
}

func PutEdwards25519Point(p *edwards25519.Point) {
	pointPool.Put(p)
}

func GetEdwards25519Scalar() *edwards25519.Scalar {
	return scalarPool.Get().(*edwards25519.Scalar)
}

func PutEdwards25519Scalar(s *edwards25519.Scalar) {
	scalarPool.Put(s)
}
