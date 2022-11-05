package address

import (
	"encoding/binary"
	"git.gammaspectra.live/P2Pool/moneroutil"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	p2poolcrypto "git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"strings"
)

func GetDeterministicTransactionPrivateKey(a Interface, prevId types.Hash) crypto.PrivateKey {
	return p2poolcrypto.GetDeterministicTransactionPrivateKey(a.SpendPublicKey(), prevId)
}

func GetPublicKeyForSharedData(a Interface, sharedData crypto.PrivateKey) crypto.PublicKey {
	return sharedData.PublicKey().AsPoint().Add(a.SpendPublicKey().AsPoint())
}

func GetEphemeralPublicKey(a Interface, txKey crypto.PrivateKey, outputIndex uint64) crypto.PublicKey {
	return GetPublicKeyForSharedData(a, crypto.GetDerivationSharedDataForOutputIndex(txKey.GetDerivation8(a.ViewPublicKey()), outputIndex))
}

func GetTxProofV2(a Interface, txId types.Hash, txKey crypto.PrivateKey, message string) string {

	var prefixData []byte
	prefixData = append(prefixData, txId[:]...)
	prefixData = append(prefixData, []byte(message)...)
	prefixHash := types.Hash(moneroutil.Keccak256(prefixData))

	sharedSecret, signature := crypto.GenerateTxProofV2(prefixHash, txKey, a.ViewPublicKey(), nil)

	return "OutProofV2" + moneroutil.EncodeMoneroBase58(sharedSecret.AsSlice()) + moneroutil.EncodeMoneroBase58(signature.Bytes())
}

func GetTxProofV1(a Interface, txId types.Hash, txKey crypto.PrivateKey, message string) string {

	var prefixData []byte
	prefixData = append(prefixData, txId[:]...)
	prefixData = append(prefixData, []byte(message)...)
	prefixHash := types.Hash(moneroutil.Keccak256(prefixData))

	sharedSecret, signature := crypto.GenerateTxProofV1(prefixHash, txKey, a.ViewPublicKey(), nil)

	return "OutProofV1" + moneroutil.EncodeMoneroBase58(sharedSecret.AsSlice()) + moneroutil.EncodeMoneroBase58(signature.Bytes())
}

type SignatureVerifyResult int

const (
	ResultFail SignatureVerifyResult = iota
	ResultSuccessSpend
	ResultSuccessView
)

func GetMessageHash(a Interface, message []byte, mode uint8) types.Hash {
	buf := make([]byte, 0, 23+types.HashSize*2+1+len(message)+binary.MaxVarintLen64)
	buf = append(buf, []byte("MoneroMessageSignature")...)
	buf = append(buf, []byte{0}...) //null byte for previous string
	buf = append(buf, a.SpendPublicKey().AsSlice()...)
	buf = append(buf, a.ViewPublicKey().AsSlice()...)
	buf = append(buf, []byte{mode}...) //mode, 0 = sign with spend key, 1 = sign with view key
	buf = binary.AppendUvarint(buf, uint64(len(message)))
	buf = append(buf, message...)
	return types.Hash(moneroutil.Keccak256(buf))
}

func Verify(a Interface, message []byte, signature string) SignatureVerifyResult {
	var hash types.Hash

	if strings.HasPrefix(signature, "SigV1") {
		hash = types.Hash(moneroutil.Keccak256(message))
	} else if strings.HasPrefix(signature, "SigV2") {
		hash = GetMessageHash(a, message, 0)
	} else {
		return ResultFail
	}
	raw := moneroutil.DecodeMoneroBase58(signature[5:])

	sig := crypto.NewSignatureFromBytes(raw)

	if sig == nil {
		return ResultFail
	}

	if crypto.VerifyMessageSignature(hash, a.SpendPublicKey(), sig) {
		return ResultSuccessSpend
	}

	if strings.HasPrefix(signature, "SigV2") {
		hash = GetMessageHash(a, message, 1)
	}

	if crypto.VerifyMessageSignature(hash, a.ViewPublicKey(), sig) {
		return ResultSuccessView
	}

	return ResultFail
}
