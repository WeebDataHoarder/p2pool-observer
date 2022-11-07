package sidechain

import (
	"encoding/binary"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"github.com/floatdrop/lru"
)

type derivationCacheKey [crypto.PublicKeySize * 2]byte
type sharedDataCacheKey [crypto.PrivateKeySize + 8]byte

type sharedDataWithTag struct {
	SharedData *crypto.PrivateKeyScalar
	ViewTag    uint8
}

type DerivationCache struct {
	deterministicKeyCache   *lru.LRU[derivationCacheKey, *crypto.KeyPair]
	derivationCache         *lru.LRU[derivationCacheKey, *crypto.PublicKeyPoint]
	sharedDataCache         *lru.LRU[sharedDataCacheKey, sharedDataWithTag]
	ephemeralPublicKeyCache *lru.LRU[derivationCacheKey, crypto.PublicKeyBytes]
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

	d.deterministicKeyCache = lru.New[derivationCacheKey, *crypto.KeyPair](cacheForNMinutesOfShares)
	d.derivationCache = lru.New[derivationCacheKey, *crypto.PublicKeyPoint](pplnsSize * knownMinersPerPplns)
	d.sharedDataCache = lru.New[sharedDataCacheKey, sharedDataWithTag](pplnsSize * knownMinersPerPplns * outputIdsPerMiner)
	d.ephemeralPublicKeyCache = lru.New[derivationCacheKey, crypto.PublicKeyBytes](pplnsSize * knownMinersPerPplns * outputIdsPerMiner)
}

func (d *DerivationCache) GetEphemeralPublicKey(a address.Interface, txKey crypto.PrivateKey, outputIndex uint64) (crypto.PublicKeyBytes, uint8) {
	sharedData, viewTag := d.GetSharedData(a, txKey, outputIndex)

	var key derivationCacheKey
	copy(key[:], a.SpendPublicKey().AsSlice())
	copy(key[types.HashSize:], sharedData.AsSlice())
	if ephemeralPubKey := d.ephemeralPublicKeyCache.Get(key); ephemeralPubKey == nil {
		ephemeralPubKey := address.GetPublicKeyForSharedData(a, sharedData).AsBytes()
		d.ephemeralPublicKeyCache.Set(key, ephemeralPubKey)
		return ephemeralPubKey, viewTag
	} else {
		return *ephemeralPubKey, viewTag
	}
}

func (d *DerivationCache) GetSharedData(a address.Interface, txKey crypto.PrivateKey, outputIndex uint64) (*crypto.PrivateKeyScalar, uint8) {
	derivation := d.GetDerivation(a, txKey)

	var key sharedDataCacheKey
	copy(key[:], derivation.AsSlice())
	binary.LittleEndian.PutUint64(key[types.HashSize:], outputIndex)

	if sharedData := d.sharedDataCache.Get(key); sharedData == nil {
		var data sharedDataWithTag
		data.SharedData = crypto.GetDerivationSharedDataForOutputIndex(derivation, outputIndex).AsScalar()
		data.ViewTag = crypto.GetDerivationViewTagForOutputIndex(derivation, outputIndex)
		d.sharedDataCache.Set(key, data)
		return data.SharedData, data.ViewTag
	} else {
		return sharedData.SharedData, sharedData.ViewTag
	}
}

func (d *DerivationCache) GetDeterministicTransactionKey(a address.Interface, prevId types.Hash) *crypto.KeyPair {
	var key derivationCacheKey
	copy(key[:], a.SpendPublicKey().AsSlice())
	copy(key[types.HashSize:], prevId[:])

	if kp := d.deterministicKeyCache.Get(key); kp == nil {
		data := crypto.NewKeyPairFromPrivate(address.GetDeterministicTransactionPrivateKey(a, prevId))
		d.deterministicKeyCache.Set(key, data)
		return data
	} else {
		return *kp
	}
}

func (d *DerivationCache) GetDerivation(a address.Interface, txKey crypto.PrivateKey) *crypto.PublicKeyPoint {
	var key derivationCacheKey
	copy(key[:], a.ViewPublicKey().AsSlice())
	copy(key[types.HashSize:], txKey.AsSlice())

	if derivation := d.derivationCache.Get(key); derivation == nil {
		data := txKey.GetDerivationCofactor(a.ViewPublicKey())
		d.derivationCache.Set(key, data.AsPoint())
		return data.AsPoint()
	} else {
		return *derivation
	}
}
