package sidechain

import (
	"context"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/randomx"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"log"
	"os"
	"testing"
)

func TestPoolBlockDecode(t *testing.T) {

	if buf, err := os.ReadFile("testdata/block.dat"); err != nil {
		t.Fatal(err)
	} else {
		b := &PoolBlock{}
		if err = b.UnmarshalBinary(ConsensusDefault, &NilDerivationCache{}, buf); err != nil {
			t.Fatal(err)
		}

		if buf, _ = b.Main.MarshalBinary(); len(buf) != 1757 {
			t.Fatal()
		}

		powHash := b.PowHash(ConsensusDefault.GetHasher(), func(height uint64) (hash types.Hash) {
			seedHeight := randomx.SeedHeight(height)
			if h, err := client.GetDefaultClient().GetBlockByHeight(seedHeight, context.Background()); err != nil {
				return types.ZeroHash
			} else {
				return types.MustHashFromString(h.BlockHeader.Hash)
			}
		})

		if b.SideTemplateId(ConsensusDefault).String() != "15ecd7e99e78e93bf8bffb1f597046cfa2d604decc32070e36e3caca01597c7d" {
			log.Print(b.SideTemplateId(ConsensusDefault))
			t.Fatal()
		}

		if b.MainId().String() != "fc63db007b94b3eba51bbc6e2076b0ac37b49ea554cc310c5e0fa595889960f3" {
			t.Fatal()
		}

		if powHash.String() != "aa7a3c4a2d67cb6a728e244288219bf038024f3b511b0da197a19ec601000000" {
			t.Fatal()
		}

		if !b.IsProofHigherThanDifficulty(ConsensusDefault.GetHasher(), func(height uint64) (hash types.Hash) {
			seedHeight := randomx.SeedHeight(height)
			if h, err := client.GetDefaultClient().GetBlockByHeight(seedHeight, context.Background()); err != nil {
				return types.ZeroHash
			} else {
				return types.MustHashFromString(h.BlockHeader.Hash)
			}
		}) {
			t.Fatal()
		}

		if b.Side.CoinbasePrivateKey.String() != "ba757262420e8bfa7c1931c75da051955cd2d4dff35dff7bfff42a1569941405" {
			t.Fatal()
		}

		if b.Side.CoinbasePrivateKeySeed.String() != "fd7b5f77c79e624e26b939028a15f14b250fdb217cd40b4ce524eab9b17082ca" {
			t.Fatal()
		}

		if b.Main.Coinbase.Id().String() != "b52a9222eb2712c43742ed3a598df34c821bfb5d3b5a25d41bb4bdb014505ca9" {
			t.Fatal()
		}

	}
}
