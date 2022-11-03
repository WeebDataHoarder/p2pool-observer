package sidechain

import (
	"encoding/binary"
	"filippo.io/edwards25519"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"github.com/floatdrop/lru"
)

type derivationCacheKey [types.HashSize * 2]byte
type sharedDataCacheKey [types.HashSize + 8]byte

type sharedDataWithTag struct {
	SharedData *edwards25519.Scalar
	ViewTag    uint8
}

type DerivationCache struct {
	deterministicKeyCache   *lru.LRU[derivationCacheKey, *crypto.KeyPair]
	derivationCache         *lru.LRU[derivationCacheKey, *edwards25519.Point]
	sharedDataCache         *lru.LRU[sharedDataCacheKey, sharedDataWithTag]
	ephemeralPublicKeyCache *lru.LRU[derivationCacheKey, types.Hash]
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
	d.deterministicKeyCache = lru.New[derivationCacheKey, *crypto.KeyPair](4096)
	d.derivationCache = lru.New[derivationCacheKey, *edwards25519.Point](4096)
	d.sharedDataCache = lru.New[sharedDataCacheKey, sharedDataWithTag](4096 * 2160)
	d.ephemeralPublicKeyCache = lru.New[derivationCacheKey, types.Hash](4096 * 2160)
}

func (d *DerivationCache) GetEphemeralPublicKey(address *address.Address, txKey types.Hash, outputIndex uint64) (types.Hash, uint8) {
	sharedData, viewTag := d.GetSharedData(address, txKey, outputIndex)

	var key derivationCacheKey
	copy(key[:], address.SpendPub.Bytes())
	copy(key[types.HashSize:], sharedData.Bytes())
	if ephemeralPubKey := d.ephemeralPublicKeyCache.Get(key); ephemeralPubKey == nil {
		copy((*ephemeralPubKey)[:], address.GetPublicKeyForSharedData(sharedData).Bytes())
		d.ephemeralPublicKeyCache.Set(key, *ephemeralPubKey)
		return *ephemeralPubKey, viewTag
	} else {
		return *ephemeralPubKey, viewTag
	}
}

func (d *DerivationCache) GetSharedData(address *address.Address, txKey types.Hash, outputIndex uint64) (*edwards25519.Scalar, uint8) {
	derivation := d.GetDerivation(address, txKey)

	var key sharedDataCacheKey
	copy(key[:], derivation.Bytes())
	binary.LittleEndian.PutUint64(key[types.HashSize:], outputIndex)

	if sharedData := d.sharedDataCache.Get(key); sharedData == nil {
		var data sharedDataWithTag
		data.SharedData = crypto.GetDerivationSharedDataForOutputIndex(derivation, outputIndex)
		data.ViewTag = crypto.GetDerivationViewTagForOutputIndex(derivation, outputIndex)
		d.sharedDataCache.Set(key, data)
		return data.SharedData, data.ViewTag
	} else {
		return sharedData.SharedData, sharedData.ViewTag
	}
}

func (d *DerivationCache) GetDeterministicTransactionKey(address *address.Address, prevId types.Hash) *crypto.KeyPair {
	var key derivationCacheKey
	copy(key[:], address.SpendPub.Bytes())
	copy(key[types.HashSize:], prevId[:])

	if kp := d.deterministicKeyCache.Get(key); kp == nil {
		data := crypto.NewKeyPairFromPrivate(address.GetDeterministicTransactionPrivateKey(prevId))
		d.deterministicKeyCache.Set(key, data)
		return data
	} else {
		return *kp
	}
}

func (d *DerivationCache) GetDerivation(address *address.Address, txKey types.Hash) *edwards25519.Point {
	var key derivationCacheKey
	copy(key[:], address.ViewPub.Bytes())
	copy(key[types.HashSize:], txKey[:])

	if derivation := d.derivationCache.Get(key); derivation != nil {
		pK, _ := edwards25519.NewScalar().SetCanonicalBytes(txKey[:])
		data := address.GetDerivationForPrivateKey(pK)
		d.derivationCache.Set(key, data)
		return data
	} else {
		return *derivation
	}
}
