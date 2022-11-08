package crypto

import (
	"encoding/hex"
	"filippo.io/edwards25519"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"testing"
)

func TestDeterministicTransactionPrivateKey(t *testing.T) {
	expectedPrivateKey := "c93cbd34c66ba4d5b3ddcccd3f550a0169e02225c8d045bc6418dbca4819260b"

	previousId, _ := types.HashFromString("b64ec18bf2dfa4658693d7f35836d212e66dee47af6f7263ab2bf00e422bcd68")
	publicSpendKeyBytes, _ := hex.DecodeString("f2be6705a034f8f485ee9bc3c21b6309cd0d9dd2111441cc32753ba2bac41b6d")
	p, _ := (&edwards25519.Point{}).SetBytes(publicSpendKeyBytes)
	spendPublicKey := crypto.PublicKeyFromPoint(p)

	calculatedPrivateKey := GetDeterministicTransactionPrivateKey(spendPublicKey, previousId)
	if calculatedPrivateKey.String() != expectedPrivateKey {
		t.Fatalf("got %s, expected %s", calculatedPrivateKey.String(), expectedPrivateKey)
	}
}
