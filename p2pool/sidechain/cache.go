package sidechain

import (
	"encoding/binary"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"github.com/floatdrop/lru"
	"sync/atomic"
)

type deterministicTransactionCacheKey [crypto.PublicKeySize + types.HashSize]byte
type ephemeralPublicKeyCacheKey [crypto.PrivateKeySize + crypto.PublicKeySize*2 + 8]byte

type ephemeralPublicKeyWithViewTag struct {
	PublicKey crypto.PublicKeyBytes
	ViewTag   uint8
}

type DerivationCacheInterface interface {
	GetEphemeralPublicKey(a address.Interface, txKeySlice crypto.PrivateKeySlice, txKeyScalar *crypto.PrivateKeyScalar, outputIndex uint64) (crypto.PublicKeyBytes, uint8)
	GetDeterministicTransactionKey(seed types.Hash, prevId types.Hash) *crypto.KeyPair
}

type DerivationCache struct {
	deterministicKeyCache         atomic.Pointer[lru.LRU[deterministicTransactionCacheKey, *crypto.KeyPair]]
	deterministicKeyCacheHits     atomic.Uint64
	deterministicKeyCacheMisses   atomic.Uint64
	ephemeralPublicKeyCache       atomic.Pointer[lru.LRU[ephemeralPublicKeyCacheKey, ephemeralPublicKeyWithViewTag]]
	ephemeralPublicKeyCacheHits   atomic.Uint64
	ephemeralPublicKeyCacheMisses atomic.Uint64
}

func NewDerivationCache() *DerivationCache {
	d := &DerivationCache{}
	d.Clear()
	return d
}

func (d *DerivationCache) Clear() {
	d.deterministicKeyCache.Store(lru.New[deterministicTransactionCacheKey, *crypto.KeyPair](32))
	d.ephemeralPublicKeyCache.Store(lru.New[ephemeralPublicKeyCacheKey, ephemeralPublicKeyWithViewTag](2000))
}

func (d *DerivationCache) GetEphemeralPublicKey(a address.Interface, txKeySlice crypto.PrivateKeySlice, txKeyScalar *crypto.PrivateKeyScalar, outputIndex uint64) (crypto.PublicKeyBytes, uint8) {
	var key ephemeralPublicKeyCacheKey
	copy(key[:], txKeySlice)
	copy(key[crypto.PrivateKeySize:], a.ToPackedAddress().Bytes())
	binary.LittleEndian.PutUint64(key[crypto.PrivateKeySize+crypto.PublicKeySize*2:], outputIndex)

	ephemeralPublicKeyCache := d.ephemeralPublicKeyCache.Load()
	if ephemeralPubKey := ephemeralPublicKeyCache.Get(key); ephemeralPubKey == nil {
		d.ephemeralPublicKeyCacheMisses.Add(1)
		ephemeralPubKey, viewTag := address.GetEphemeralPublicKeyAndViewTag(a, txKeyScalar, outputIndex)
		pKB := ephemeralPubKey.AsBytes()
		ephemeralPublicKeyCache.Set(key, ephemeralPublicKeyWithViewTag{PublicKey: pKB, ViewTag: viewTag})
		return pKB, viewTag
	} else {
		d.ephemeralPublicKeyCacheHits.Add(1)
		return ephemeralPubKey.PublicKey, ephemeralPubKey.ViewTag
	}
}

func (d *DerivationCache) GetDeterministicTransactionKey(seed types.Hash, prevId types.Hash) *crypto.KeyPair {
	var key deterministicTransactionCacheKey
	copy(key[:], seed[:])
	copy(key[types.HashSize:], prevId[:])

	deterministicKeyCache := d.deterministicKeyCache.Load()
	if kp := deterministicKeyCache.Get(key); kp == nil {
		d.deterministicKeyCacheMisses.Add(1)
		data := crypto.NewKeyPairFromPrivate(address.GetDeterministicTransactionPrivateKey(seed, prevId))
		deterministicKeyCache.Set(key, data)
		return data
	} else {
		d.deterministicKeyCacheHits.Add(1)
		return *kp
	}
}
