package stratum

import (
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	p2pooltypes "git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"golang.org/x/exp/slices"
	unsafeRandom "math/rand"
	"os"
	"path"
	"runtime"
	"testing"
	"time"
)

var preLoadedMiniSideChain *sidechain.SideChain

var preLoadedPoolBlock *sidechain.PoolBlock

func init() {
	_, filename, _, _ := runtime.Caller(0)
	// The ".." may change depending on you folder structure
	dir := path.Join(path.Dir(filename), "../..")
	err := os.Chdir(dir)
	if err != nil {
		panic(err)
	}
}

func getMinerData() *p2pooltypes.MinerData {
	if d, err := client.GetDefaultClient().GetMinerData(); err != nil {
		return nil
	} else {
		return &p2pooltypes.MinerData{
			MajorVersion:          d.MajorVersion,
			Height:                d.Height,
			PrevId:                types.MustHashFromString(d.PrevId),
			SeedHash:              types.MustHashFromString(d.SeedHash),
			Difficulty:            types.MustDifficultyFromString(d.Difficulty),
			MedianWeight:          d.MedianWeight,
			AlreadyGeneratedCoins: d.AlreadyGeneratedCoins,
			MedianTimestamp:       d.MedianTimestamp,
			TimeReceived:          time.Now(),
			TxBacklog:             nil,
		}
	}
}

func TestMain(m *testing.M) {
	if buf, err := os.ReadFile("testdata/block.dat"); err != nil {
		panic(err)
	} else {
		preLoadedPoolBlock = &sidechain.PoolBlock{}
		if err = preLoadedPoolBlock.UnmarshalBinary(sidechain.ConsensusDefault, &sidechain.NilDerivationCache{}, buf); err != nil {
			panic(err)
		}
	}

	_ = sidechain.ConsensusMini.InitHasher(2)
	client.SetDefaultClientSettings(os.Getenv("MONEROD_RPC_URL"))

	preLoadedMiniSideChain = sidechain.NewSideChain(sidechain.GetFakeTestServer(sidechain.ConsensusMini))

	f, err := os.Open("testdata/sidechain_dump_mini.dat")
	if err != nil {
		panic(err)
	}
	defer f.Close()

	if err = preLoadedMiniSideChain.LoadTestData(f); err != nil {
		panic(err)
	}

	code := m.Run()

	os.Exit(code)
}

func TestStratumServer(t *testing.T) {
	stratumServer := NewServer(preLoadedMiniSideChain, func(block *sidechain.PoolBlock) error {
		return nil
	})
	minerData := getMinerData()
	tip := preLoadedMiniSideChain.GetChainTip()
	stratumServer.HandleMinerData(minerData)
	stratumServer.HandleTip(tip)

	func() {
		//Process all incoming changes first
		for {
			select {
			case f := <-stratumServer.incomingChanges:
				if f() {
					stratumServer.Update()
				}
			default:
				return
			}
		}
	}()

	tpl, _, _, seedHash, err := stratumServer.BuildTemplate(address.FromBase58(types.DonationAddress).ToPackedAddress(), false)
	if err != nil {
		t.Fatal(err)
	}

	if seedHash != minerData.SeedHash {
		t.Fatal()
	}

	if tpl.MainHeight != minerData.Height {
		t.Fatal()
	}

	if tpl.MainParent != minerData.PrevId {
		t.Fatal()
	}

	if tpl.SideHeight != (tip.Side.Height + 1) {
		t.Fatal()
	}

	if tpl.SideParent != tip.SideTemplateId(preLoadedMiniSideChain.Consensus()) {
		t.Fatal()
	}
}

func TestShuffleMapping(t *testing.T) {
	const n = 16
	const shareVersion = sidechain.ShareVersion_V2
	var seed = zeroExtraBaseRCTHash
	mappings := BuildShuffleMapping(n, shareVersion, seed)

	seq := make([]int, n)
	for i := range seq {
		seq[i] = i
	}

	seq1 := slices.Clone(seq)

	sidechain.ShuffleShares(seq1, shareVersion, seed)
	seq2 := ApplyShuffleMapping(seq, mappings)

	if slices.Compare(seq1, seq2) != 0 {
		for i := range seq1 {
			if seq1[i] != seq2[i] {
				t.Logf("%d %d *** @ %d", seq1[i], seq2[i], i)
			} else {
				t.Logf("%d %d @ %d", seq1[i], seq2[i], i)
			}
		}

		t.Fatal()
	}

}

func BenchmarkServer_FillTemplate(b *testing.B) {
	stratumServer := NewServer(preLoadedMiniSideChain, func(block *sidechain.PoolBlock) error {
		return nil
	})
	minerData := getMinerData()
	tip := preLoadedMiniSideChain.GetChainTip()
	stratumServer.minerData = minerData
	stratumServer.tip = tip

	b.ResetTimer()

	b.Run("New", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if err := stratumServer.fillNewTemplateData(minerData.Difficulty); err != nil {
				b.Fatal(err)
			}
		}
		b.ReportAllocs()
	})

	b.Run("Cached", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if err := stratumServer.fillNewTemplateData(types.ZeroDifficulty); err != nil {
				b.Fatal(err)
			}
		}
		b.ReportAllocs()
	})

}

func BenchmarkServer_BuildTemplate(b *testing.B) {
	stratumServer := NewServer(preLoadedMiniSideChain, func(block *sidechain.PoolBlock) error {
		return nil
	})
	minerData := getMinerData()
	tip := preLoadedMiniSideChain.GetChainTip()
	stratumServer.minerData = minerData
	stratumServer.tip = tip

	if err := stratumServer.fillNewTemplateData(minerData.Difficulty); err != nil {
		b.Fatal(err)
	}

	const randomPoolSize = 512
	var randomKeys [randomPoolSize]address.PackedAddress

	//generate random keys deterministically
	for i := range randomKeys {
		spendPriv, viewPriv := crypto.DeterministicScalar([]byte(fmt.Sprintf("BenchmarkBuildTemplate_%d_spend", i))), crypto.DeterministicScalar([]byte(fmt.Sprintf("BenchmarkBuildTemplate_%d_spend", i)))
		randomKeys[i][0] = (*crypto.PrivateKeyScalar)(spendPriv).PublicKey().AsBytes()
		randomKeys[i][1] = (*crypto.PrivateKeyScalar)(viewPriv).PublicKey().AsBytes()
	}

	b.ResetTimer()
	b.ReportAllocs()

	b.Run("Cached", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			counter := unsafeRandom.Intn(randomPoolSize)
			for pb.Next() {
				a := randomKeys[counter%randomPoolSize]
				if _, _, _, _, err := stratumServer.BuildTemplate(a, false); err != nil {
					b.Fatal(err)
				}
				counter++
			}
		})
		b.ReportAllocs()
	})

	b.Run("Forced", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			counter := unsafeRandom.Intn(randomPoolSize)
			for pb.Next() {
				a := randomKeys[counter%randomPoolSize]
				if _, _, _, _, err := stratumServer.BuildTemplate(a, true); err != nil {
					b.Fatal(err)
				}
				counter++
			}
		})
		b.ReportAllocs()
	})
}
