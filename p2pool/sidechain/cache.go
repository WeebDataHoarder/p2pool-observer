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

type DerivationCache struct {
	deterministicKeyCache   atomic.Pointer[lru.LRU[deterministicTransactionCacheKey, *crypto.KeyPair]]
	ephemeralPublicKeyCache atomic.Pointer[lru.LRU[ephemeralPublicKeyCacheKey, ephemeralPublicKeyWithViewTag]]
}

func NewDerivationCache() *DerivationCache {
	d := &DerivationCache{}
	d.Clear()
	return d
}

func (d *DerivationCache) Clear() {
	//keep a few recent blocks from the past few for uncles, and reused window miners
	//~10s per share, keys change every Monero block (2m). around 2160 max shares per 6h (window), plus uncles. 6 shares per minute.
	//each share can have up to 2160 outputs, plus uncles. each miner has its own private key per Monero block

	const pplnsSize = 2160
	const pplnsDurationInMinutes = 60 * 6
	const sharesPerMinute = pplnsSize / pplnsDurationInMinutes
	const cacheForNMinutesOfShares = sharesPerMinute * 5
	const knownMinersPerPplns = pplnsSize / 4
	const outputIdsPerMiner = 2

	d.deterministicKeyCache.Store(lru.New[deterministicTransactionCacheKey, *crypto.KeyPair](cacheForNMinutesOfShares))
	d.ephemeralPublicKeyCache.Store(lru.New[ephemeralPublicKeyCacheKey, ephemeralPublicKeyWithViewTag](pplnsSize * knownMinersPerPplns * outputIdsPerMiner))
}

func (d *DerivationCache) GetEphemeralPublicKey(a address.Interface, txKeySlice crypto.PrivateKeySlice, txKeyScalar *crypto.PrivateKeyScalar, outputIndex uint64) (crypto.PublicKeyBytes, uint8) {
	var key ephemeralPublicKeyCacheKey
	copy(key[:], txKeySlice)
	copy(key[crypto.PrivateKeySize:], a.ToPackedAddress().Bytes())
	binary.LittleEndian.PutUint64(key[crypto.PrivateKeySize+crypto.PublicKeySize*2:], outputIndex)

	ephemeralPublicKeyCache := d.ephemeralPublicKeyCache.Load()
	if ephemeralPubKey := ephemeralPublicKeyCache.Get(key); ephemeralPubKey == nil {
		ephemeralPubKey, viewTag := address.GetEphemeralPublicKeyAndViewTag(a, txKeyScalar, outputIndex)
		pKB := ephemeralPubKey.AsBytes()
		ephemeralPublicKeyCache.Set(key, ephemeralPublicKeyWithViewTag{PublicKey: pKB, ViewTag: viewTag})
		return pKB, viewTag
	} else {
		return ephemeralPubKey.PublicKey, ephemeralPubKey.ViewTag
	}
}

func (d *DerivationCache) GetDeterministicTransactionKey(seed types.Hash, prevId types.Hash) *crypto.KeyPair {
	var key deterministicTransactionCacheKey
	copy(key[:], seed[:])
	copy(key[types.HashSize:], prevId[:])

	deterministicKeyCache := d.deterministicKeyCache.Load()
	if kp := deterministicKeyCache.Get(key); kp == nil {
		data := crypto.NewKeyPairFromPrivate(address.GetDeterministicTransactionPrivateKey(seed, prevId))
		deterministicKeyCache.Set(key, data)
		return data
	} else {
		return *kp
	}
}
