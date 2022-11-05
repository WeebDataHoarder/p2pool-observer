package sidechain

import (
	"encoding/hex"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	block2 "git.gammaspectra.live/P2Pool/p2pool-observer/monero/block"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"io"
	"log"
	"os"
	"testing"
)

func TestPoolBlockDecode(t *testing.T) {
	client.SetClientSettings(os.Getenv("MONEROD_RPC_URL"))

	f, err := os.Open("testdata/1783223a701d16192ce9ff83c603b48b3e1785e3779b42079ede6e52ea7f0d2d.hex")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	buf, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := hex.DecodeString(string(buf))
	if err != nil {
		t.Fatal(err)
	}

	block, err := NewShareFromExportedBytes(contents)
	if err != nil {
		t.Fatal(err)
	}

	mainId := block.MainId()

	if hex.EncodeToString(mainId[:]) != "05892769e709b6cfebd5d71e5cadf38ba0abde8048a0eea3792d981861ad9a69" {
		t.Fatalf("expected main id 05892769e709b6cfebd5d71e5cadf38ba0abde8048a0eea3792d981861ad9a69, got %s", mainId.String())
	}
	if hex.EncodeToString(block.CoinbaseExtra(SideTemplateId)) != "1783223a701d16192ce9ff83c603b48b3e1785e3779b42079ede6e52ea7f0d2d" {
		t.Fatalf("expected side id 1783223a701d16192ce9ff83c603b48b3e1785e3779b42079ede6e52ea7f0d2d, got %s", hex.EncodeToString(block.CoinbaseExtra(SideTemplateId)))
	}

	b1, _ := block.Main.MarshalBinary()
	main2 := &block2.Block{}
	if err = main2.UnmarshalBinary(b1); err != nil {
		t.Fatal(err)
	}
	if main2.Id() != block.Main.Id() {
		t.Fatalf("expected main id %s, got %s", block.Main.Id().String(), main2.Id())
	}

	b2, _ := block.Main.MarshalBinary()
	side2 := &SideData{}
	if err = side2.UnmarshalBinary(b2); err != nil {
		t.Fatal(err)
	}

	t.Log(block.SideTemplateId(ConsensusDefault).String())

	t.Log(block.Side.CoinbasePrivateKey.String())
	t.Log(address.GetDeterministicTransactionPrivateKey(block.GetAddress(), block.Main.PreviousId).String())

	txId := block.Main.Coinbase.Id()

	if txId.String() != "41e8976fafbf9263996733b8f857a11ca385a78798c33617af8c77cfd989da60" {
		t.Fatalf("expected coinbase id 41e8976fafbf9263996733b8f857a11ca385a78798c33617af8c77cfd989da60, got %s", txId.String())
	}

	proofResult, _ := types.DifficultyFromString("00000000000000000000006ef6334490")

	if types.DifficultyFromPoW(block.PowHash()).Cmp(proofResult) != 0 {
		t.Fatalf("expected PoW difficulty %s, got %s", proofResult.String(), types.DifficultyFromPoW(block.PowHash()).String())
	}

	t.Log(block.Main.Id().String())
	//t.Log(block.Main.PowHash().String())
	//t.Log(block.Main.PowHash().String())

	if !block.IsProofHigherThanMainDifficulty() {
		t.Fatal("expected proof higher than difficulty")
	}

	block.cache.powHash[31] = 1

	if block.IsProofHigherThanMainDifficulty() {
		t.Fatal("expected proof lower than difficulty")
	}

	log.Print(types.DifficultyFromPoW(block.PowHash()).String())
	log.Print(block.MainDifficulty().String())
}
