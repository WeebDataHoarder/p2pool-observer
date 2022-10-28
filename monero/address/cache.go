package address

import (
	"encoding/binary"
	"filippo.io/edwards25519"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"github.com/Code-Hex/go-generics-cache/policy/lfu"
)

type derivationCacheKey [types.HashSize * 2]byte
type sharedDataCacheKey [types.HashSize + 8]byte

type keyPair struct {
	private *edwards25519.Scalar
	public  *edwards25519.Point
}

type DerivationCache struct {
	deterministicKeyCache   *lfu.Cache[derivationCacheKey, *keyPair]
	derivationCache         *lfu.Cache[derivationCacheKey, *edwards25519.Point]
	sharedDataCache         *lfu.Cache[sharedDataCacheKey, *edwards25519.Scalar]
	ephemeralPublicKeyCache *lfu.Cache[derivationCacheKey, types.Hash]
}

func NewDerivationCache() *DerivationCache {
	d := &DerivationCache{}
	d.Clear()
	return d
}

func (d *DerivationCache) Clear() {
	//keep a few recent blocks from the past few for uncles, and reused window miners
	//~10s per share, keys change every monero block (2m). around 2160 max shares per 6h (window), plus uncles. 6 shares per minute.
	//each share can have up to 2160 outputs, plus uncles. each miner has its own private key per monero block
	d.deterministicKeyCache = lfu.NewCache[derivationCacheKey, *keyPair](lfu.WithCapacity(4096))
	d.derivationCache = lfu.NewCache[derivationCacheKey, *edwards25519.Point](lfu.WithCapacity(4096))
	d.sharedDataCache = lfu.NewCache[sharedDataCacheKey, *edwards25519.Scalar](lfu.WithCapacity(4096 * 2160))
	d.ephemeralPublicKeyCache = lfu.NewCache[derivationCacheKey, types.Hash](lfu.WithCapacity(4096 * 2160))
}

func (d *DerivationCache) GetEphemeralPublicKey(address *Address, txKey types.Hash, outputIndex uint64) types.Hash {
	sharedData := d.GetSharedData(address, txKey, outputIndex)

	var key derivationCacheKey
	copy(key[:], address.SpendPub.Bytes())
	copy(key[types.HashSize:], sharedData.Bytes())
	if ephemeralPubKey, ok := d.ephemeralPublicKeyCache.Get(key); !ok {
		copy(ephemeralPubKey[:], address.GetPublicKeyForSharedData(sharedData).Bytes())
		d.ephemeralPublicKeyCache.Set(key, ephemeralPubKey)
		return ephemeralPubKey
	} else {
		return ephemeralPubKey
	}
}

func (d *DerivationCache) GetSharedData(address *Address, txKey types.Hash, outputIndex uint64) *edwards25519.Scalar {
	derivation := d.GetDerivation(address, txKey)

	var key sharedDataCacheKey
	copy(key[:], derivation.Bytes())
	binary.LittleEndian.PutUint64(key[types.HashSize:], outputIndex)

	if sharedData, ok := d.sharedDataCache.Get(key); !ok {
		sharedData = GetDerivationSharedDataForOutputIndex(derivation, outputIndex)
		d.sharedDataCache.Set(key, sharedData)
		return sharedData
	} else {
		return sharedData
	}
}

func (d *DerivationCache) GetDeterministicTransactionKey(address *Address, prevId types.Hash) (private *edwards25519.Scalar, public *edwards25519.Point) {
	var key derivationCacheKey
	copy(key[:], address.SpendPub.Bytes())
	copy(key[types.HashSize:], prevId[:])

	if kp, ok := d.deterministicKeyCache.Get(key); !ok {
		kp = &keyPair{
			private: address.GetDeterministicTransactionPrivateKey(prevId),
		}
		kp.public = edwards25519.NewIdentityPoint().ScalarBaseMult(kp.private)
		d.deterministicKeyCache.Set(key, kp)
		return kp.private, kp.public
	} else {
		return kp.private, kp.public
	}
}

func (d *DerivationCache) GetDerivation(address *Address, txKey types.Hash) *edwards25519.Point {
	var key derivationCacheKey
	copy(key[:], address.ViewPub.Bytes())
	copy(key[types.HashSize:], txKey[:])

	if derivation, ok := d.derivationCache.Get(key); !ok {
		pK, _ := edwards25519.NewScalar().SetCanonicalBytes(txKey[:])
		derivation = address.GetDerivationForPrivateKey(pK)
		d.derivationCache.Set(key, derivation)
		return derivation
	} else {
		return derivation
	}
}
