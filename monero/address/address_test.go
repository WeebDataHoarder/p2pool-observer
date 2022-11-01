package address

import (
	"bytes"
	"encoding/hex"
	"filippo.io/edwards25519"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"log"
	"testing"
)

var privateKey = edwards25519.NewScalar()

var testAddress = FromBase58("42HEEF3NM9cHkJoPpDhNyJHuZ6DFhdtymCohF9CwP5KPM1Mp3eH2RVXCPRrxe4iWRogT7299R8PP7drGvThE8bHmRDq1qWp")

var ephemeralPubKey, _ = hex.DecodeString("20efc1310db960b0e8d22c8b85b3414fcaa1ed9aab40cf757321dd6099a62d5e")

func init() {
	h, _ := hex.DecodeString("74b98b1e7ce5fc50d1634f8634622395ec2a19a4698a016fedd8139df374ac00")
	if _, err := privateKey.SetCanonicalBytes(h); err != nil {
		log.Panic(err)
	}
}

func TestAddress(t *testing.T) {
	derivation := testAddress.GetDerivationForPrivateKey(privateKey)

	sharedData := crypto.GetDerivationSharedDataForOutputIndex(derivation, 37)
	ephemeralPublicKey := testAddress.GetPublicKeyForSharedData(sharedData)

	if bytes.Compare(ephemeralPublicKey.Bytes(), ephemeralPubKey) != 0 {
		t.Fatalf("ephemeral key mismatch, expected %s, got %s", hex.EncodeToString(ephemeralPubKey), hex.EncodeToString(ephemeralPublicKey.Bytes()))
	}
}
