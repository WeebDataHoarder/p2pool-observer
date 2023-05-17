package crypto

import (
	"encoding/hex"
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

func TestGenerateKeyDerivation(t *testing.T) {
	results := GetTestEntries("generate_key_derivation", 3)
	if results == nil {
		t.Fatal()
	}
	for e := range results {
		var expectedDerivation types.Hash

		key1 := PublicKeyBytes(types.MustHashFromString(e[0]))
		key2 := PrivateKeyBytes(types.MustHashFromString(e[1]))
		result := e[2] == "true"
		if result {
			expectedDerivation = types.MustHashFromString(e[3])
		}

		point := key1.AsPoint()
		scalar := key2.AsScalar()

		if result == false && (point == nil || scalar == nil) {
			//expected failure
			continue
		} else if point == nil || scalar == nil {
			t.Fatalf("invalid point %s / scalar %s", key1.String(), key2.String())
		}

		derivation := scalar.GetDerivationCofactor(point)
		if result {
			if expectedDerivation.String() != derivation.String() {
				t.Fatalf("expected %s, got %s", expectedDerivation.String(), derivation.String())
			}
		}
	}
}

func TestDeriveViewTag(t *testing.T) {
	results := GetTestEntries("derive_view_tag", 3)
	if results == nil {
		t.Fatal()
	}
	for e := range results {
		derivation := PublicKeyBytes(types.MustHashFromString(e[0]))
		outputIndex, _ := strconv.ParseUint(e[1], 10, 0)
		result, _ := hex.DecodeString(e[2])

		viewTag := GetDerivationViewTagForOutputIndex(&derivation, outputIndex)

		if result[0] != viewTag {
			t.Fatalf("expected %s, got %s", hex.EncodeToString(result), hex.EncodeToString([]byte{viewTag}))
		}
	}
}
