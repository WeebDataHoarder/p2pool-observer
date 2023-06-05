package stratum

import (
	"bytes"
	"encoding/hex"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	unsafeRandom "math/rand"
	"testing"
)

func TestTemplate(t *testing.T) {
	b := preLoadedPoolBlock
	buf, _ := b.MarshalBinary()
	tpl, err := TemplateFromPoolBlock(b)
	if err != nil {
		t.Fatal(err)
	}

	preAllocatedBuffer := make([]byte, 0, len(tpl.Buffer))

	blockTemplateId := types.HashFromBytes(b.CoinbaseExtra(sidechain.SideTemplateId))

	if tplBuf := tpl.Blob(preAllocatedBuffer, b.Main.Nonce, b.ExtraNonce(), b.Side.ExtraBuffer.RandomNumber, b.Side.ExtraBuffer.SideChainExtraNonce, blockTemplateId); bytes.Compare(tplBuf, buf) != 0 {
		if len(tplBuf) == len(buf) {
			for i := range buf {
				if buf[i] != tplBuf[i] {
					t.Logf("%s %s *** @ %d", hex.EncodeToString(buf[i:i+1]), hex.EncodeToString(tplBuf[i:i+1]), i)
				} else {
					t.Logf("%s %s @ %d", hex.EncodeToString(buf[i:i+1]), hex.EncodeToString(tplBuf[i:i+1]), i)
				}
			}
		}
		t.Fatal("not matching blob buffers")
	}

	writer := bytes.NewBuffer(nil)

	if err := tpl.Write(writer, b.Main.Nonce, b.ExtraNonce(), b.Side.ExtraBuffer.RandomNumber, b.Side.ExtraBuffer.SideChainExtraNonce, blockTemplateId); err != nil {
		t.Fatal(err)
	} else if bytes.Compare(writer.Bytes(), buf) != 0 {
		t.Fatal("not matching writer buffers")
	}

	hasher := crypto.GetKeccak256Hasher()
	defer crypto.PutKeccak256Hasher(hasher)

	bHashingBlob := b.Main.HashingBlob(nil)
	if tplHashingBlob := tpl.HashingBlob(hasher, preAllocatedBuffer, b.Main.Nonce, b.ExtraNonce(), blockTemplateId); bytes.Compare(tplHashingBlob, bHashingBlob) != 0 {
		if len(tplHashingBlob) == len(bHashingBlob) {
			for i := range buf {
				if bHashingBlob[i] != tplHashingBlob[i] {
					t.Logf("%s %s *** @ %d", hex.EncodeToString(bHashingBlob[i:i+1]), hex.EncodeToString(tplHashingBlob[i:i+1]), i)
				} else {
					t.Logf("%s %s @ %d", hex.EncodeToString(bHashingBlob[i:i+1]), hex.EncodeToString(tplHashingBlob[i:i+1]), i)
				}
			}
		}
		t.Fatal("not matching hashing blob buffers")
	}

	bCoinbaseBlob, _ := b.Main.Coinbase.MarshalBinary()
	if tplCoinbaseBlob := tpl.CoinbaseBlob(preAllocatedBuffer, b.ExtraNonce(), blockTemplateId); bytes.Compare(tplCoinbaseBlob, bCoinbaseBlob) != 0 {
		if len(tplCoinbaseBlob) == len(bCoinbaseBlob) {
			for i := range buf {
				if bCoinbaseBlob[i] != tplCoinbaseBlob[i] {
					t.Logf("%s %s *** @ %d", hex.EncodeToString(bCoinbaseBlob[i:i+1]), hex.EncodeToString(tplCoinbaseBlob[i:i+1]), i)
				} else {
					t.Logf("%s %s @ %d", hex.EncodeToString(bCoinbaseBlob[i:i+1]), hex.EncodeToString(tplCoinbaseBlob[i:i+1]), i)
				}
			}
		}
		t.Fatal("not matching coinbase blob buffers")
	}

	var coinbaseId types.Hash

	if tpl.CoinbaseId(hasher, b.ExtraNonce(), blockTemplateId, &coinbaseId); coinbaseId != b.Main.Coinbase.Id() {
		t.Fatal("different coinbase ids")
	}

	if tpl.CoinbaseBlobId(hasher, preAllocatedBuffer, b.ExtraNonce(), blockTemplateId, &coinbaseId); coinbaseId != b.Main.Coinbase.Id() {
		t.Fatal("different coinbase blob ids")
	}

	var templateId types.Hash
	if tpl.TemplateId(hasher, preAllocatedBuffer, sidechain.ConsensusDefault, b.Side.ExtraBuffer.RandomNumber, b.Side.ExtraBuffer.SideChainExtraNonce, &templateId); templateId != blockTemplateId {
		t.Fatal("different template ids")
	}

	if tpl.Timestamp() != b.Main.Timestamp {
		t.Fatal("different timestamps")
	}

	if tpl.HashingBlobBufferLength() != b.Main.HashingBlobBufferLength() {
		t.Fatal("different hashing blob buffer length")
	}

	if tpl.HashingBlobBufferLength() != len(tpl.HashingBlob(hasher, preAllocatedBuffer, 0, 0, types.ZeroHash)) {
		t.Fatal("different hashing blob buffer length from blob")
	}

}

func BenchmarkTemplate_CoinbaseId(b *testing.B) {
	tpl, err := TemplateFromPoolBlock(preLoadedPoolBlock)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		hasher := crypto.GetKeccak256Hasher()
		defer crypto.PutKeccak256Hasher(hasher)
		var counter = unsafeRandom.Uint32()
		var coinbaseId types.Hash
		for pb.Next() {
			tpl.CoinbaseId(hasher, counter, types.ZeroHash, &coinbaseId)
			counter++
		}
	})
	b.ReportAllocs()
}

func BenchmarkTemplate_CoinbaseBlobId(b *testing.B) {
	tpl, err := TemplateFromPoolBlock(preLoadedPoolBlock)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		preAllocatedBuffer := make([]byte, 0, tpl.CoinbaseBufferLength())
		hasher := crypto.GetKeccak256Hasher()
		defer crypto.PutKeccak256Hasher(hasher)
		var counter = unsafeRandom.Uint32()
		var coinbaseId types.Hash
		for pb.Next() {
			tpl.CoinbaseBlobId(hasher, preAllocatedBuffer, counter, types.ZeroHash, &coinbaseId)
			counter++
		}
	})
	b.ReportAllocs()
}

func BenchmarkTemplate_HashingBlob(b *testing.B) {
	tpl, err := TemplateFromPoolBlock(preLoadedPoolBlock)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		preAllocatedBuffer := make([]byte, 0, tpl.HashingBlobBufferLength())
		hasher := crypto.GetKeccak256Hasher()
		defer crypto.PutKeccak256Hasher(hasher)
		var counter = unsafeRandom.Uint32()
		for pb.Next() {
			tpl.HashingBlob(hasher, preAllocatedBuffer, counter, counter, types.ZeroHash)
			counter++
		}
	})
	b.ReportAllocs()
}

func BenchmarkTemplate_TemplateId(b *testing.B) {
	tpl, err := TemplateFromPoolBlock(preLoadedPoolBlock)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		preAllocatedBuffer := make([]byte, 0, len(tpl.Buffer))
		hasher := crypto.GetKeccak256Hasher()
		defer crypto.PutKeccak256Hasher(hasher)
		var counter = unsafeRandom.Uint32()
		var templateId types.Hash
		for pb.Next() {
			tpl.TemplateId(hasher, preAllocatedBuffer, sidechain.ConsensusDefault, counter, counter+1, &templateId)
			counter++
		}
	})
	b.ReportAllocs()
}
