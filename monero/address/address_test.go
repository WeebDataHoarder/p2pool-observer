package address

import (
	"bytes"
	"encoding/hex"
	"git.gammaspectra.live/P2Pool/edwards25519"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"sync/atomic"
	"testing"
)

var privateKey = edwards25519.NewScalar()

var testAddress = FromBase58("42HEEF3NM9cHkJoPpDhNyJHuZ6DFhdtymCohF9CwP5KPM1Mp3eH2RVXCPRrxe4iWRogT7299R8PP7drGvThE8bHmRDq1qWp")
var testAddress2 = FromBase58("4AQ3YkqG2XdWsPHEgrDGdyQLq1qMMGFqWTFJfrVQW99qPmCzZKvJqzxgf5342KC17o9bchfJcUzLhVW9QgNKTYUBLg876Gt")
var testAddress3 = FromBase58("47Eqp7fsvVnPPSU4rsXrKJhyAme6LhDRZDzFky9xWsWUS9pd6FPjJCMDCNX1NnNiDzTwfbAgGMk2N6A1aucNcrkhLffta1p")

var ephemeralPubKey, _ = hex.DecodeString("20efc1310db960b0e8d22c8b85b3414fcaa1ed9aab40cf757321dd6099a62d5e")

func init() {
	h, _ := hex.DecodeString("74b98b1e7ce5fc50d1634f8634622395ec2a19a4698a016fedd8139df374ac00")
	if _, err := privateKey.SetCanonicalBytes(h); err != nil {
		utils.Panic(err)
	}
}

func TestAddress(t *testing.T) {
	derivation := crypto.PrivateKeyFromScalar(privateKey).GetDerivationCofactor(testAddress.ViewPublicKey())

	sharedData := crypto.GetDerivationSharedDataForOutputIndex(derivation, 37)
	ephemeralPublicKey := GetPublicKeyForSharedData(testAddress, sharedData)

	if bytes.Compare(ephemeralPublicKey.AsSlice(), ephemeralPubKey) != 0 {
		t.Fatalf("ephemeral key mismatch, expected %s, got %s", hex.EncodeToString(ephemeralPubKey), ephemeralPublicKey.String())
	}
}

var previousId, _ = types.HashFromString("d59abce89ce8131eba025988d1ea372937f2acf85b86b46993b80c4354563a8b")
var detTxPriv, _ = types.HashFromString("10f3941fd50ca266d3350984004a804c887c36ec11620080fe0b7c4a2a208605")

func TestDeterministic(t *testing.T) {
	detTx := types.HashFromBytes(GetDeterministicTransactionPrivateKey(types.Hash(testAddress2.SpendPublicKey().AsBytes()), previousId).AsSlice())
	if detTx != detTxPriv {
		t.Fatal()
	}
}

func TestSort(t *testing.T) {
	if testAddress2.Compare(testAddress3) != -1 {
		t.Fatalf("expected address2 < address3, got %d", testAddress2.Compare(testAddress3))
	}
}

func BenchmarkCoinbaseDerivation(b *testing.B) {
	b.ReportAllocs()
	packed := testAddress3.ToPackedAddress()
	txKey := crypto.PrivateKeyFromScalar(privateKey)
	var i atomic.Uint64
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			GetEphemeralPublicKeyAndViewTag(&packed, txKey, i.Add(1))
		}
	})
	b.ReportAllocs()
}

func BenchmarkCoinbaseDerivationInline(b *testing.B) {
	spendPub, viewPub := testAddress3.SpendPublicKey().AsPoint().Point(), testAddress3.ViewPublicKey().AsPoint().Point()

	var i atomic.Uint64
	b.RunParallel(func(pb *testing.PB) {
		p := new(edwards25519.Point)
		for pb.Next() {
			getEphemeralPublicKeyInline(spendPub, viewPub, privateKey, i.Add(1), p)
		}
	})
	b.ReportAllocs()
}

func BenchmarkCoinbaseDerivationNoAllocate(b *testing.B) {

	spendPub, viewPub := testAddress3.SpendPublicKey().AsPoint().Point(), testAddress3.ViewPublicKey().AsPoint().Point()

	txKey := privateKey

	var i atomic.Uint64
	b.RunParallel(func(pb *testing.PB) {
		hasher := crypto.GetKeccak256Hasher()
		defer crypto.PutKeccak256Hasher(hasher)
		for pb.Next() {
			GetEphemeralPublicKeyAndViewTagNoAllocate(spendPub, GetDerivationNoAllocate(viewPub, txKey), txKey, i.Add(1), hasher)
		}
	})
	b.ReportAllocs()
}

func BenchmarkPackedAddress_ComparePacked(b *testing.B) {
	a1, a2 := testAddress.ToPackedAddress(), testAddress2.ToPackedAddress()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if a1.ComparePacked(&a2) == 0 {
				panic("cannot be equal")
			}
		}
	})
	b.ReportAllocs()
}
