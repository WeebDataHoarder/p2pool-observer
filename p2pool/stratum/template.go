package stratum

import (
	"encoding/binary"
	"errors"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"git.gammaspectra.live/P2Pool/sha3"
	"io"
)

type Template struct {
	Buffer []byte

	// NonceOffset offset of an uint32
	NonceOffset int

	CoinbaseOffset int

	// ExtraNonceOffset offset of an uint32
	ExtraNonceOffset int

	// TemplateIdOffset offset of a types.Hash
	TemplateIdOffset int

	// TransactionsOffset Start of transactions section
	TransactionsOffset int

	// TemplateExtraBufferOffset offset of 4*uint32
	TemplateExtraBufferOffset int

	MainHeight uint64
	MainParent types.Hash

	SideHeight     uint64
	SideParent     types.Hash
	SideDifficulty types.Difficulty

	MerkleTreeMainBranch []types.Hash
}

func (tpl *Template) Write(writer io.Writer, nonce, extraNonce, sideRandomNumber, sideExtraNonce uint32, templateId types.Hash) error {
	var uint32Buf [4]byte

	if _, err := writer.Write(tpl.Buffer[:tpl.NonceOffset]); err != nil {
		return err
	}

	binary.LittleEndian.PutUint32(uint32Buf[:], nonce)
	if _, err := writer.Write(uint32Buf[:]); err != nil {
		return err
	}

	if _, err := writer.Write(tpl.Buffer[tpl.NonceOffset+4 : tpl.ExtraNonceOffset]); err != nil {
		return err
	}

	binary.LittleEndian.PutUint32(uint32Buf[:], extraNonce)
	if _, err := writer.Write(uint32Buf[:]); err != nil {
		return err
	}

	if _, err := writer.Write(tpl.Buffer[tpl.ExtraNonceOffset+4 : tpl.TemplateIdOffset]); err != nil {
		return err
	}
	if _, err := writer.Write(templateId[:]); err != nil {
		return err
	}
	if _, err := writer.Write(tpl.Buffer[tpl.TemplateIdOffset+types.HashSize : tpl.TemplateExtraBufferOffset+4*2]); err != nil {
		return err
	}

	binary.LittleEndian.PutUint32(uint32Buf[:], sideRandomNumber)
	if _, err := writer.Write(uint32Buf[:]); err != nil {
		return err
	}

	binary.LittleEndian.PutUint32(uint32Buf[:], sideExtraNonce)
	if _, err := writer.Write(uint32Buf[:]); err != nil {
		return err
	}

	return nil
}

func (tpl *Template) Blob(preAllocatedBuffer []byte, nonce, extraNonce, sideRandomNumber, sideExtraNonce uint32, templateId types.Hash) []byte {
	buf := append(preAllocatedBuffer, tpl.Buffer...)

	// Overwrite nonce
	binary.LittleEndian.PutUint32(buf[tpl.NonceOffset:], nonce)
	// Overwrite extra nonce
	binary.LittleEndian.PutUint32(buf[tpl.ExtraNonceOffset:], extraNonce)
	// Overwrite template id
	copy(buf[tpl.TemplateIdOffset:], templateId[:])
	// Overwrite sidechain random number
	binary.LittleEndian.PutUint32(buf[tpl.TemplateExtraBufferOffset+4*2:], sideRandomNumber)
	// Overwrite sidechain extra nonce number
	binary.LittleEndian.PutUint32(buf[tpl.TemplateExtraBufferOffset+4*3:], sideExtraNonce)

	return buf
}

func (tpl *Template) TemplateId(hasher *sha3.HasherState, preAllocatedBuffer []byte, consensus *sidechain.Consensus, sideRandomNumber, sideExtraNonce uint32, result *types.Hash) {
	buf := tpl.Blob(preAllocatedBuffer, 0, 0, sideRandomNumber, sideExtraNonce, types.ZeroHash)

	_, _ = hasher.Write(buf)
	_, _ = hasher.Write(consensus.Id[:])
	crypto.HashFastSum(hasher, (*result)[:])
	hasher.Reset()
}

func (tpl *Template) Timestamp() uint64 {
	t, _ := binary.Uvarint(tpl.Buffer[2:])
	return t
}

func (tpl *Template) CoinbaseBufferLength() int {
	return tpl.TransactionsOffset - tpl.CoinbaseOffset
}

func (tpl *Template) CoinbaseBlob(preAllocatedBuffer []byte, extraNonce uint32, templateId types.Hash) []byte {
	buf := append(preAllocatedBuffer, tpl.Buffer[tpl.CoinbaseOffset:tpl.TransactionsOffset]...)

	// Overwrite extra nonce
	binary.LittleEndian.PutUint32(buf[tpl.ExtraNonceOffset-tpl.CoinbaseOffset:], extraNonce)
	// Overwrite template id
	copy(buf[tpl.TemplateIdOffset-tpl.CoinbaseOffset:], templateId[:])

	return buf
}

func (tpl *Template) CoinbaseBlobId(hasher *sha3.HasherState, preAllocatedBuffer []byte, extraNonce uint32, templateId types.Hash, result *types.Hash) {

	buf := tpl.CoinbaseBlob(preAllocatedBuffer, extraNonce, templateId)
	_, _ = hasher.Write(buf[:len(buf)-1])
	crypto.HashFastSum(hasher, (*result)[:])
	hasher.Reset()

	CoinbaseIdHash(hasher, *result, result)
}

func (tpl *Template) CoinbaseId(hasher *sha3.HasherState, extraNonce uint32, templateId types.Hash, result *types.Hash) {

	var extraNonceBuf [4]byte

	_, _ = hasher.Write(tpl.Buffer[tpl.CoinbaseOffset:tpl.ExtraNonceOffset])
	// extra nonce
	binary.LittleEndian.PutUint32(extraNonceBuf[:], extraNonce)
	_, _ = hasher.Write(extraNonceBuf[:])

	_, _ = hasher.Write(tpl.Buffer[tpl.ExtraNonceOffset+4 : tpl.TemplateIdOffset])
	// template id
	_, _ = hasher.Write(templateId[:])

	_, _ = hasher.Write(tpl.Buffer[tpl.TemplateIdOffset+types.HashSize : tpl.TransactionsOffset-1])

	crypto.HashFastSum(hasher, (*result)[:])
	hasher.Reset()

	CoinbaseIdHash(hasher, *result, result)
}

var zeroExtraBaseRCTHash = crypto.PooledKeccak256([]byte{0})

func CoinbaseIdHash(hasher *sha3.HasherState, coinbaseBlobMinusBaseRTC types.Hash, result *types.Hash) {
	_, _ = hasher.Write(coinbaseBlobMinusBaseRTC[:])
	// Base RCT, single 0 byte in miner tx
	_, _ = hasher.Write(zeroExtraBaseRCTHash[:])
	// Prunable RCT, empty in miner tx
	_, _ = hasher.Write(types.ZeroHash[:])
	crypto.HashFastSum(hasher, (*result)[:])
	hasher.Reset()
}

func (tpl *Template) HashingBlobBufferLength() int {
	_, n := binary.Uvarint(tpl.Buffer[tpl.TransactionsOffset:])

	return tpl.NonceOffset + 4 + types.HashSize + n
}

func (tpl *Template) HashingBlob(hasher *sha3.HasherState, preAllocatedBuffer []byte, nonce, extraNonce uint32, templateId types.Hash) []byte {

	var rootHash types.Hash
	tpl.CoinbaseId(hasher, extraNonce, templateId, &rootHash)

	buf := append(preAllocatedBuffer, tpl.Buffer[:tpl.NonceOffset]...)
	buf = binary.LittleEndian.AppendUint32(buf, nonce)

	numTransactions, n := binary.Uvarint(tpl.Buffer[tpl.TransactionsOffset:])

	if numTransactions < 1 {
	} else if numTransactions < 2 {
		_, _ = hasher.Write(rootHash[:])
		_, _ = hasher.Write(tpl.Buffer[tpl.TransactionsOffset+n : tpl.TransactionsOffset+n+types.HashSize])
		crypto.HashFastSum(hasher, rootHash[:])
		hasher.Reset()
	} else {
		for i := range tpl.MerkleTreeMainBranch {
			_, _ = hasher.Write(rootHash[:])
			_, _ = hasher.Write(tpl.MerkleTreeMainBranch[i][:])
			crypto.HashFastSum(hasher, rootHash[:])
			hasher.Reset()
		}
	}

	buf = append(buf, rootHash[:]...)
	buf = binary.AppendUvarint(buf, numTransactions+1)
	return buf
}

func TemplateFromPoolBlock(b *sidechain.PoolBlock) (tpl *Template, err error) {
	if b.ShareVersion() < sidechain.ShareVersion_V1 {
		return nil, errors.New("unsupported share version")
	}
	totalLen := b.BufferLength()
	buf := make([]byte, 0, b.BufferLength())
	if buf, err = b.AppendBinaryFlags(buf, false, false); err != nil {
		return nil, err
	}

	tpl = &Template{
		Buffer: buf,
	}

	mainBufferLength := b.Main.BufferLength()
	coinbaseLength := b.Main.Coinbase.BufferLength()
	tpl.NonceOffset = mainBufferLength - (4 + coinbaseLength + utils.UVarInt64Size(len(b.Main.Transactions)) + types.HashSize*len(b.Main.Transactions))

	tpl.CoinbaseOffset = mainBufferLength - (coinbaseLength + utils.UVarInt64Size(len(b.Main.Transactions)) + types.HashSize*len(b.Main.Transactions))

	tpl.TransactionsOffset = mainBufferLength - (utils.UVarInt64Size(len(b.Main.Transactions)) + types.HashSize*len(b.Main.Transactions))

	tpl.ExtraNonceOffset = tpl.NonceOffset + 4 + (coinbaseLength - (b.Main.Coinbase.Extra[1].BufferLength() + b.Main.Coinbase.Extra[2].BufferLength() + 1)) + 1 + utils.UVarInt64Size(b.Main.Coinbase.Extra[1].VarInt)

	tpl.TemplateIdOffset = tpl.NonceOffset + 4 + (coinbaseLength - (b.Main.Coinbase.Extra[2].BufferLength() + 1)) + 1 + utils.UVarInt64Size(b.Main.Coinbase.Extra[2].VarInt)
	tpl.TemplateExtraBufferOffset = totalLen - 4*4

	// Set places to zeroes where necessary
	tpl.Buffer = tpl.Blob(make([]byte, 0, len(tpl.Buffer)), 0, 0, 0, 0, types.ZeroHash)

	if len(b.Main.Transactions) > 1 {
		merkleTree := make(crypto.BinaryTreeHash, len(b.Main.Transactions)+1)
		copy(merkleTree[1:], b.Main.Transactions)
		tpl.MerkleTreeMainBranch = merkleTree.MainBranch()
	}

	tpl.MainHeight = b.Main.Coinbase.GenHeight
	tpl.MainParent = b.Main.PreviousId

	tpl.SideHeight = b.Side.Height
	tpl.SideParent = b.Side.Parent
	tpl.SideDifficulty = b.Side.Difficulty

	return tpl, nil
}
