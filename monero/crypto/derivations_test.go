package crypto

import (
	"encoding/hex"
	"git.gammaspectra.live/P2Pool/edwards25519"
	"testing"
)

func TestKeyImageRaw(t *testing.T) {
	sec, _ := hex.DecodeString("981d477fb18897fa1f784c89721a9d600bf283f06b89cb018a077f41dcefef0f")

	scalar, _ := (&edwards25519.Scalar{}).SetCanonicalBytes(sec)
	keyImage := GetKeyImage(NewKeyPairFromPrivate(PrivateKeyFromScalar(scalar)))

	if keyImage.String() != "a637203ec41eab772532d30420eac80612fce8e44f1758bc7e2cb1bdda815887" {
		t.Fatalf("key image expected %s, got %s", "a637203ec41eab772532d30420eac80612fce8e44f1758bc7e2cb1bdda815887", keyImage.String())
	}
}
