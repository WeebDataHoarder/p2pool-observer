package sidechain

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"io"
	"log"
	"os"
	"path"
	"runtime"
	"slices"
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

	_ = ConsensusDefault.InitHasher(2)
	_ = ConsensusMini.InitHasher(2)
	client.SetDefaultClientSettings(os.Getenv("MONEROD_RPC_URL"))
}

func testSideChain(s *SideChain, t *testing.T, reader io.Reader, sideHeight, mainHeight uint64, patchedBlocks ...[]byte) {

	if err := s.LoadTestData(reader, patchedBlocks...); err != nil {
		t.Fatal(err)
	}

	tip := s.GetChainTip()

	if tip == nil {
		t.Fatal()
	}

	if !tip.Verified.Load() {
		t.Fatal()
	}

	if tip.Invalid.Load() {
		t.Fatal()
	}

	if tip.Main.Coinbase.GenHeight != mainHeight {
		t.Fatal()
	}

	if tip.Side.Height != sideHeight {
		t.Fatal()
	}

	hits, misses := s.DerivationCache().ephemeralPublicKeyCache.Stats()
	total := hits + misses
	log.Printf("Ephemeral Public Key Cache hits = %d (%.02f%%), misses = %d (%.02f%%), total = %d", hits, (float64(hits)/float64(total))*100, misses, (float64(misses)/float64(total))*100, total)

	hits, misses = s.DerivationCache().deterministicKeyCache.Stats()
	total = hits + misses
	log.Printf("Deterministic Key Cache hits = %d (%.02f%%), misses = %d (%.02f%%), total = %d", hits, (float64(hits)/float64(total))*100, misses, (float64(misses)/float64(total))*100, total)

	hits, misses = s.DerivationCache().derivationCache.Stats()
	total = hits + misses
	log.Printf("Derivation Cache hits = %d (%.02f%%), misses = %d (%.02f%%), total = %d", hits, (float64(hits)/float64(total))*100, misses, (float64(misses)/float64(total))*100, total)

	hits, misses = s.DerivationCache().pubKeyToPointCache.Stats()
	total = hits + misses
	log.Printf("PubKeyToPoint Key Cache hits = %d (%.02f%%), misses = %d (%.02f%%), total = %d", hits, (float64(hits)/float64(total))*100, misses, (float64(misses)/float64(total))*100, total)
}

func TestSideChainDefault(t *testing.T) {

	s := NewSideChain(GetFakeTestServer(ConsensusDefault))

	f, err := os.Open("testdata/sidechain_dump.dat")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	testSideChain(s, t, f, 4957203, 2870010)
}

func TestSideChainDefaultPreFork(t *testing.T) {

	s := NewSideChain(GetFakeTestServer(ConsensusDefault))

	f, err := os.Open("testdata/old_sidechain_dump.dat")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	testSideChain(s, t, f, 522805, 2483901)
}

func TestSideChainMini(t *testing.T) {

	s := NewSideChain(GetFakeTestServer(ConsensusMini))

	f, err := os.Open("testdata/sidechain_dump_mini.dat")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	testSideChain(s, t, f, 4414446, 2870010)
}

func TestSideChainMiniPreFork(t *testing.T) {

	s := NewSideChain(GetFakeTestServer(ConsensusMini))

	f, err := os.Open("testdata/old_sidechain_dump_mini.dat")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	//patch in missing blocks that are needed to newer reach sync range
	block2420028, err := os.ReadFile("testdata/old_sidechain_dump_mini_2420028.dat")
	if err != nil {
		t.Fatal(err)
	}

	block2420027, err := os.ReadFile("testdata/old_sidechain_dump_mini_2420027.dat")
	if err != nil {
		t.Fatal(err)
	}

	testSideChain(s, t, f, 2424349, 2696040, block2420028, block2420027)
}

func benchmarkResetState(tip, parent *PoolBlock, templateId types.Hash, fullId FullId, difficulty types.Difficulty, blocksByHeightKeys []uint64, s *SideChain) {
	//Remove states in maps
	s.blocksByHeight.Delete(tip.Side.Height)
	s.blocksByHeightKeys = blocksByHeightKeys
	s.blocksByHeightKeysSorted = true
	s.blocksByTemplateId.Delete(templateId)
	s.seenBlocks.Delete(fullId)

	// Update tip and depths
	tip.Depth.Store(0)
	parent.Depth.Store(0)
	s.chainTip.Store(parent)
	s.syncTip.Store(parent)
	s.currentDifficulty.Store(&difficulty)
	s.updateDepths(parent)

	// Update verification state
	tip.Verified.Store(false)
	tip.Invalid.Store(false)
	tip.iterationCache = nil
}

func benchSideChain(b *testing.B, s *SideChain, tipHash types.Hash) {
	b.StopTimer()

	tip := s.GetChainTip()

	for tip.SideTemplateId(s.Consensus()) != tipHash {
		s.blocksByHeight.Delete(tip.Side.Height)
		s.blocksByHeightKeys = slices.DeleteFunc(s.blocksByHeightKeys, func(u uint64) bool {
			return u == tip.Side.Height
		})
		s.blocksByTemplateId.Delete(tip.SideTemplateId(s.Consensus()))
		s.seenBlocks.Delete(tip.FullId())

		tip = s.GetParent(tip)
		if tip == nil {
			b.Error("nil tip")
			return
		}
	}
	templateId := tip.SideTemplateId(s.Consensus())
	fullId := tip.FullId()

	parent := s.GetParent(tip)

	difficulty, _, _ := s.GetDifficulty(parent)

	slices.Sort(s.blocksByHeightKeys)
	blocksByHeightKeys := slices.DeleteFunc(s.blocksByHeightKeys, func(u uint64) bool {
		return u == tip.Side.Height
	})

	benchmarkResetState(tip, parent, templateId, fullId, difficulty, slices.Clone(blocksByHeightKeys), s)

	var err error

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		benchmarkResetState(tip, parent, templateId, fullId, difficulty, slices.Clone(blocksByHeightKeys), s)
		b.StartTimer()
		_, err, _ = s.AddPoolBlockExternal(tip)
		if err != nil {
			b.Error(err)
			return
		}
	}
}

var benchLoadedSideChain *SideChain

func TestMain(m *testing.M) {

	var isBenchmark bool
	for _, arg := range os.Args {
		if arg == "-test.bench" {
			isBenchmark = true
		}
	}

	if isBenchmark {
		benchLoadedSideChain = NewSideChain(GetFakeTestServer(ConsensusDefault))

		f, err := os.Open("testdata/sidechain_dump.dat")
		if err != nil {
			panic(err)
		}
		defer f.Close()

		testSideChain(benchLoadedSideChain, nil, f, 4957203, 2870010)

		tip := benchLoadedSideChain.GetChainTip()

		// Pre-calculate PoW
		for i := 0; i < 5; i++ {
			tip.PowHashWithError(benchLoadedSideChain.Consensus().GetHasher(), benchLoadedSideChain.getSeedByHeightFunc())
			tip = benchLoadedSideChain.GetParent(tip)
		}
	}

	os.Exit(m.Run())
}

func BenchmarkSideChainDefault_AddPoolBlockExternal(b *testing.B) {
	b.ReportAllocs()
	benchSideChain(b, benchLoadedSideChain, types.MustHashFromString("61ecfc1c7738eacd8b815d2e28f124b31962996ae3af4121621b5c5501f19c5d"))
}

func BenchmarkSideChainDefault_GetDifficulty(b *testing.B) {
	b.ReportAllocs()
	tip := benchLoadedSideChain.GetChainTip()

	b.ResetTimer()
	var verifyError, invalidError error
	for i := 0; i < b.N; i++ {
		_, verifyError, invalidError = benchLoadedSideChain.getDifficulty(tip)
		if verifyError != nil {
			b.Error(verifyError)
			return
		}
		if invalidError != nil {
			b.Error(invalidError)
			return
		}
	}
}

func BenchmarkSideChainDefault_CalculateOutputs(b *testing.B) {
	b.ReportAllocs()
	tip := benchLoadedSideChain.GetChainTip()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		outputs, _ := benchLoadedSideChain.calculateOutputs(tip)
		if outputs == nil {
			b.Error("nil outputs")
			return
		}
	}
}

func BenchmarkSideChainDefault_GetShares(b *testing.B) {
	b.ReportAllocs()
	tip := benchLoadedSideChain.GetChainTip()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		shares, _ := benchLoadedSideChain.getShares(tip, benchLoadedSideChain.preAllocatedShares)
		if shares == nil {
			b.Error("nil shares")
			return
		}
	}
}

func BenchmarkSideChainDefault_BlocksInPPLNSWindow(b *testing.B) {
	b.ReportAllocs()
	tip := benchLoadedSideChain.GetChainTip()

	b.ResetTimer()

	var err error
	for i := 0; i < b.N; i++ {
		_, err = BlocksInPPLNSWindow(tip, benchLoadedSideChain.Consensus(), benchLoadedSideChain.server.GetDifficultyByHeight, benchLoadedSideChain.getPoolBlockByTemplateId, func(b *PoolBlock, weight types.Difficulty) {

		})
		if err != nil {
			b.Error(err)
			return
		}
	}
}
