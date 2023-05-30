package sidechain

import (
	"context"
	"encoding/binary"
	mainblock "git.gammaspectra.live/P2Pool/p2pool-observer/monero/block"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	p2pooltypes "git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"io"
	"log"
	"os"
	"path"
	"runtime"
	"sync/atomic"
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
	var err error
	buf := make([]byte, PoolBlockMaxTemplateSize)
	for {
		buf = buf[:0]
		var blockLen uint32
		if err = binary.Read(reader, binary.LittleEndian, &blockLen); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if _, err = io.ReadFull(reader, buf[:blockLen]); err != nil {
			t.Fatal(err)
		}
		b := &PoolBlock{}
		if err = b.UnmarshalBinary(s.Consensus(), s.DerivationCache(), buf[:blockLen]); err != nil {
			t.Fatal(err)
		}
		if err = s.AddPoolBlock(b); err != nil {
			t.Fatal(err)
		}
	}

	for _, buf := range patchedBlocks {
		b := &PoolBlock{}
		if err = b.UnmarshalBinary(s.Consensus(), s.DerivationCache(), buf); err != nil {
			t.Fatal(err)
		}
		if err = s.AddPoolBlock(b); err != nil {
			t.Fatal(err)
		}
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

	s := NewSideChain(&fakeServer{
		consensus: ConsensusDefault,
	})

	f, err := os.Open("testdata/sidechain_dump.dat")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	testSideChain(s, t, f, 4957203, 2870010)
}

func TestSideChainDefaultPreFork(t *testing.T) {

	s := NewSideChain(&fakeServer{
		consensus: ConsensusDefault,
	})

	f, err := os.Open("testdata/old_sidechain_dump.dat")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	testSideChain(s, t, f, 522805, 2483901)
}

func TestSideChainMini(t *testing.T) {

	s := NewSideChain(&fakeServer{
		consensus: ConsensusMini,
	})

	f, err := os.Open("testdata/sidechain_dump_mini.dat")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	testSideChain(s, t, f, 4414446, 2870010)
}

func TestSideChainMiniPreFork(t *testing.T) {

	s := NewSideChain(&fakeServer{
		consensus: ConsensusMini,
	})

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

type fakeServer struct {
	consensus  *Consensus
	lastHeader atomic.Pointer[mainblock.Header]
}

func (s *fakeServer) Context() context.Context {
	return context.Background()
}

func (s *fakeServer) Consensus() *Consensus {
	return s.consensus
}

func (s *fakeServer) GetBlob(key []byte) (blob []byte, err error) {
	return nil, nil
}

func (s *fakeServer) SetBlob(key, blob []byte) (err error) {
	return nil
}

func (s *fakeServer) RemoveBlob(key []byte) (err error) {
	return nil
}

func (s *fakeServer) UpdateTip(tip *PoolBlock) {

}
func (s *fakeServer) Broadcast(block *PoolBlock) {

}
func (s *fakeServer) ClientRPC() *client.Client {
	return client.GetDefaultClient()
}
func (s *fakeServer) GetChainMainByHeight(height uint64) *ChainMain {
	return nil
}
func (s *fakeServer) GetChainMainByHash(hash types.Hash) *ChainMain {
	return nil
}
func (s *fakeServer) GetMinimalBlockHeaderByHeight(height uint64) *mainblock.Header {
	if h := s.lastHeader.Load(); h != nil && h.Height == height {
		return h
	}
	if h, err := s.ClientRPC().GetBlockHeaderByHeight(height, context.Background()); err != nil {
		return nil
	} else {
		header := &mainblock.Header{
			MajorVersion: uint8(h.BlockHeader.MajorVersion),
			MinorVersion: uint8(h.BlockHeader.MinorVersion),
			Timestamp:    uint64(h.BlockHeader.Timestamp),
			PreviousId:   types.MustHashFromString(h.BlockHeader.PrevHash),
			Height:       h.BlockHeader.Height,
			Nonce:        uint32(h.BlockHeader.Nonce),
			Reward:       h.BlockHeader.Reward,
			Difficulty:   types.DifficultyFrom64(h.BlockHeader.Difficulty),
			Id:           types.MustHashFromString(h.BlockHeader.Hash),
		}
		s.lastHeader.Store(header)
		return header
	}
}
func (s *fakeServer) GetMinimalBlockHeaderByHash(hash types.Hash) *mainblock.Header {
	return nil
}
func (s *fakeServer) GetDifficultyByHeight(height uint64) types.Difficulty {
	return s.GetMinimalBlockHeaderByHeight(height).Difficulty
}
func (s *fakeServer) UpdateBlockFound(data *ChainMain, block *PoolBlock) {

}
func (s *fakeServer) SubmitBlock(block *mainblock.Block) {

}
func (s *fakeServer) GetChainMainTip() *ChainMain {
	return nil
}
func (s *fakeServer) GetMinerDataTip() *p2pooltypes.MinerData {
	return nil
}
func (s *fakeServer) Store(block *PoolBlock) {

}
func (s *fakeServer) ClearCachedBlocks() {

}
