package sidechain

import (
	"context"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/randomx"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"os"
	"testing"
)

func testPoolBlock(b *PoolBlock, t *testing.T, expectedBufferLength int, majorVersion, minorVersion uint8, sideHeight uint64, nonce uint32, templateId, mainId, expectedPowHash, privateKeySeed, coinbaseId types.Hash, privateKey crypto.PrivateKeyBytes) {
	if buf, _ := b.Main.MarshalBinary(); len(buf) != expectedBufferLength {
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

	if b.Main.MajorVersion != majorVersion || b.Main.MinorVersion != minorVersion {
		t.Fatal()
	}

	if b.Side.Height != sideHeight {
		t.Fatal()
	}

	if b.Main.Nonce != nonce {
		t.Fatal()
	}

	if b.SideTemplateId(ConsensusDefault) != templateId {
		t.Fatal()
	}

	if b.FullId().TemplateId() != templateId {
		t.Logf("%s != %s", b.FullId().TemplateId(), templateId)
		t.Fatal()
	}

	if b.MainId() != mainId {
		t.Fatal()
	}

	if powHash != expectedPowHash {
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

	if b.Side.CoinbasePrivateKey != privateKey {
		t.Fatal()
	}

	if b.Side.CoinbasePrivateKeySeed != privateKeySeed {
		t.Fatal()
	}

	if b.CoinbaseId() != coinbaseId {
		t.Fatal()
	}
}

func TestPoolBlockDecode(t *testing.T) {

	if buf, err := os.ReadFile("testdata/block.dat"); err != nil {
		t.Fatal(err)
	} else {
		b := &PoolBlock{}
		if err = b.UnmarshalBinary(ConsensusDefault, &NilDerivationCache{}, buf); err != nil {
			t.Fatal(err)
		}

		testPoolBlock(b, t,
			1757,
			16,
			16,
			4674483,
			1247,
			types.MustHashFromString("15ecd7e99e78e93bf8bffb1f597046cfa2d604decc32070e36e3caca01597c7d"),
			types.MustHashFromString("fc63db007b94b3eba51bbc6e2076b0ac37b49ea554cc310c5e0fa595889960f3"),
			types.MustHashFromString("aa7a3c4a2d67cb6a728e244288219bf038024f3b511b0da197a19ec601000000"),
			types.MustHashFromString("fd7b5f77c79e624e26b939028a15f14b250fdb217cd40b4ce524eab9b17082ca"),
			types.MustHashFromString("b52a9222eb2712c43742ed3a598df34c821bfb5d3b5a25d41bb4bdb014505ca9"),
			crypto.PrivateKeyBytes(types.MustHashFromString("ba757262420e8bfa7c1931c75da051955cd2d4dff35dff7bfff42a1569941405")),
		)
	}
}

func TestPoolBlockDecodePreFork(t *testing.T) {

	if buf, err := os.ReadFile("testdata/old_mainnet_test2_block.dat"); err != nil {
		t.Fatal(err)
	} else {
		b := &PoolBlock{}
		if err = b.UnmarshalBinary(ConsensusDefault, &NilDerivationCache{}, buf); err != nil {
			t.Fatal(err)
		}

		testPoolBlock(b, t,
			5607,
			14,
			14,
			53450,
			2432795907,
			types.MustHashFromString("bd39e2870edfd54838fe690f70178bfe4f433198ae0b3c8b0725bdbbddf7ed57"),
			types.MustHashFromString("5023d36d54141efae5895eae3d51478fe541c5898ad4d783cef33118b67051f2"),
			types.MustHashFromString("f76d731c61c9c9b6c3f46be2e60c9478930b49b4455feecd41ecb9420d000000"),
			types.ZeroHash,
			types.MustHashFromString("09f4ead399cd8357eff7403562e43fe472f79e47deb39148ff3d681fc8f113ea"),
			crypto.PrivateKeyBytes(types.MustHashFromString("fed259c99cb7d21ac94ade82f2909ad1ccabdeafd16eeb60db4ca5eedca86a0a")),
		)
	}
}
