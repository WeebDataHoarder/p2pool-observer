package sidechain

import (
	"encoding/binary"
	"git.gammaspectra.live/P2Pool/edwards25519"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"git.gammaspectra.live/P2Pool/sha3"
)

type deterministicTransactionCacheKey [crypto.PublicKeySize + types.HashSize]byte
type ephemeralPublicKeyCacheKey [crypto.PrivateKeySize + crypto.PublicKeySize*2 + 8]byte

type ephemeralPublicKeyWithViewTag struct {
	PublicKey crypto.PublicKeyBytes
	ViewTag   uint8
}

type DerivationCacheInterface interface {
	GetEphemeralPublicKey(a *address.PackedAddress, txKeySlice crypto.PrivateKeySlice, txKeyScalar *crypto.PrivateKeyScalar, outputIndex uint64, hasher *sha3.HasherState) (crypto.PublicKeyBytes, uint8)
	GetDeterministicTransactionKey(seed types.Hash, prevId types.Hash) *crypto.KeyPair
}

type DerivationCache struct {
	deterministicKeyCache   utils.Cache[deterministicTransactionCacheKey, *crypto.KeyPair]
	ephemeralPublicKeyCache utils.Cache[ephemeralPublicKeyCacheKey, ephemeralPublicKeyWithViewTag]
	pubKeyToPointCache      utils.Cache[crypto.PublicKeyBytes, *edwards25519.Point]
}

func NewDerivationLRUCache() *DerivationCache {
	d := &DerivationCache{
		deterministicKeyCache:   utils.NewLRUCache[deterministicTransactionCacheKey, *crypto.KeyPair](32),
		ephemeralPublicKeyCache: utils.NewLRUCache[ephemeralPublicKeyCacheKey, ephemeralPublicKeyWithViewTag](2000),
		pubKeyToPointCache:      utils.NewLRUCache[crypto.PublicKeyBytes, *edwards25519.Point](2000),
	}
	return d
}

func NewDerivationMapCache() *DerivationCache {
	d := &DerivationCache{
		deterministicKeyCache:   utils.NewMapCache[deterministicTransactionCacheKey, *crypto.KeyPair](32),
		ephemeralPublicKeyCache: utils.NewMapCache[ephemeralPublicKeyCacheKey, ephemeralPublicKeyWithViewTag](2000),
		pubKeyToPointCache:      utils.NewLRUCache[crypto.PublicKeyBytes, *edwards25519.Point](2000),
	}
	return d
}

func (d *DerivationCache) Clear() {
	d.deterministicKeyCache.Clear()
	d.ephemeralPublicKeyCache.Clear()
	d.pubKeyToPointCache.Clear()
}

func (d *DerivationCache) GetEphemeralPublicKey(a *address.PackedAddress, txKeySlice crypto.PrivateKeySlice, txKeyScalar *crypto.PrivateKeyScalar, outputIndex uint64, hasher *sha3.HasherState) (crypto.PublicKeyBytes, uint8) {
	var key ephemeralPublicKeyCacheKey
	copy(key[:], txKeySlice)
	copy(key[crypto.PrivateKeySize:], a.ToPackedAddress().Bytes())
	binary.LittleEndian.PutUint64(key[crypto.PrivateKeySize+crypto.PublicKeySize*2:], outputIndex)

	if ephemeralPubKey, ok := d.ephemeralPublicKeyCache.Get(key); ok {
		return ephemeralPubKey.PublicKey, ephemeralPubKey.ViewTag
	} else {
		pKB, viewTag := address.GetEphemeralPublicKeyAndViewTagNoAllocate(d.getPublicKeyPoint(*a.SpendPublicKey()), d.getPublicKeyPoint(*a.ViewPublicKey()), txKeyScalar.Scalar(), outputIndex, hasher)
		d.ephemeralPublicKeyCache.Set(key, ephemeralPublicKeyWithViewTag{PublicKey: pKB, ViewTag: viewTag})
		return pKB, viewTag
	}
}

func (d *DerivationCache) GetDeterministicTransactionKey(seed types.Hash, prevId types.Hash) *crypto.KeyPair {
	var key deterministicTransactionCacheKey
	copy(key[:], seed[:])
	copy(key[types.HashSize:], prevId[:])

	if kp, ok := d.deterministicKeyCache.Get(key); ok {
		return kp
	} else {
		data := crypto.NewKeyPairFromPrivate(address.GetDeterministicTransactionPrivateKey(seed, prevId))
		d.deterministicKeyCache.Set(key, data)
		return data
	}
}

func (d *DerivationCache) getPublicKeyPoint(publicKey crypto.PublicKeyBytes) *edwards25519.Point {
	if point, ok := d.pubKeyToPointCache.Get(publicKey); ok {
		return point
	} else {
		point = publicKey.AsPoint().Point()
		d.pubKeyToPointCache.Set(publicKey, point)
		return point
	}
}
