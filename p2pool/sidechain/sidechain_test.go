package sidechain

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	"io"
	"log"
	"os"
	"path"
	"runtime"
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
