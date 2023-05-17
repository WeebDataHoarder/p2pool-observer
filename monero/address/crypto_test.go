package address

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"os"
	"path"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func init() {
	_, filename, _, _ := runtime.Caller(0)
	// The ".." may change depending on you folder structure
	dir := path.Join(path.Dir(filename), "../..")
	err := os.Chdir(dir)
	if err != nil {
		panic(err)
	}

}

func GetTestEntries(name string, n int) chan []string {
	buf, err := os.ReadFile("testdata/crypto_tests.txt")
	if err != nil {
		return nil
	}
	result := make(chan []string)
	go func() {
		defer close(result)
		for _, line := range strings.Split(string(buf), "\n") {
			entries := strings.Split(strings.TrimSpace(line), " ")
			if entries[0] == name && len(entries) >= (n+1) {
				result <- entries[1:]
			}
		}
	}()
	return result
}

func TestDerivePublicKey(t *testing.T) {
	results := GetTestEntries("derive_public_key", 4)
	if results == nil {
		t.Fatal()
	}
	for e := range results {
		var expectedDerivedKey types.Hash

		derivation := crypto.PublicKeyBytes(types.MustHashFromString(e[0]))
		outputIndex, _ := strconv.ParseUint(e[1], 10, 0)

		base := crypto.PublicKeyBytes(types.MustHashFromString(e[2]))

		result := e[3] == "true"
		if result {
			expectedDerivedKey = types.MustHashFromString(e[4])
		}

		point2 := base.AsPoint()

		if result == false && point2 == nil {
			//expected failure
			continue
		} else if point2 == nil {
			t.Fatalf("invalid point %s / %s", derivation.String(), base.String())
		}

		sharedData := crypto.GetDerivationSharedDataForOutputIndex(&derivation, outputIndex)

		var addr PackedAddress
		addr[0] = base
		derivedKey := GetPublicKeyForSharedData(&addr, sharedData)

		if result {
			if expectedDerivedKey.String() != derivedKey.String() {
				t.Fatalf("expected %s, got %s", expectedDerivedKey.String(), derivedKey.String())
			}
		}
	}
}
