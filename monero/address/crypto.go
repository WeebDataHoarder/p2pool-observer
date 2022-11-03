package address

import (
	"encoding/binary"
	"filippo.io/edwards25519"
	"git.gammaspectra.live/P2Pool/moneroutil"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	p2poolcrypto "git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"strings"
)

func (a *Address) GetDerivationForPrivateKey(privateKey *edwards25519.Scalar) *edwards25519.Point {
	return crypto.GetDerivationForPrivateKey(a.ViewPub, privateKey)
}

func (a *Address) GetDeterministicTransactionPrivateKey(prevId types.Hash) *edwards25519.Scalar {
	return p2poolcrypto.GetDeterministicTransactionPrivateKey(a.SpendPub, prevId)
}

func (a *Address) GetPublicKeyForSharedData(sharedData *edwards25519.Scalar) *edwards25519.Point {
	sG := (&edwards25519.Point{}).ScalarBaseMult(sharedData)

	return (&edwards25519.Point{}).Add(sG, a.SpendPub)

}

func (a *Address) GetEphemeralPublicKey(txKey types.Hash, outputIndex uint64) (result types.Hash) {
	pK, _ := edwards25519.NewScalar().SetCanonicalBytes(txKey[:])
	copy(result[:], a.GetPublicKeyForSharedData(crypto.GetDerivationSharedDataForOutputIndex(a.GetDerivationForPrivateKey(pK), outputIndex)).Bytes())

	return
}

func (a *Address) GetTxProofV2(txId types.Hash, txKey types.Hash, message string) string {

	var prefixData []byte
	prefixData = append(prefixData, txId[:]...)
	prefixData = append(prefixData, []byte(message)...)
	prefixHash := types.Hash(moneroutil.Keccak256(prefixData))

	sharedSecret, signature := crypto.GenerateTxProofV2(prefixHash, crypto.NewKeyPairFromPrivateHash(txKey), a.ViewPub, nil)

	return "OutProofV2" + moneroutil.EncodeMoneroBase58(sharedSecret.Bytes()) + moneroutil.EncodeMoneroBase58(signature.Bytes())
}

func (a *Address) GetTxProofV1(txId types.Hash, txKey types.Hash, message string) string {

	var prefixData []byte
	prefixData = append(prefixData, txId[:]...)
	prefixData = append(prefixData, []byte(message)...)
	prefixHash := types.Hash(moneroutil.Keccak256(prefixData))

	sharedSecret, signature := crypto.GenerateTxProofV1(prefixHash, crypto.NewKeyPairFromPrivateHash(txKey), a.ViewPub, nil)

	return "OutProofV1" + moneroutil.EncodeMoneroBase58(sharedSecret.Bytes()) + moneroutil.EncodeMoneroBase58(signature.Bytes())
}

type SignatureVerifyResult int

const (
	ResultFail SignatureVerifyResult = iota
	ResultSuccessSpend
	ResultSuccessView
)

func (a *Address) getMessageHash(message []byte, mode uint8) types.Hash {
	buf := make([]byte, 0, 23+types.HashSize*2+1+len(message)+binary.MaxVarintLen64)
	buf = append(buf, []byte("MoneroMessageSignature")...)
	buf = append(buf, []byte{0}...) //null byte for previous string
	buf = append(buf, a.SpendPub.Bytes()...)
	buf = append(buf, a.ViewPub.Bytes()...)
	buf = append(buf, []byte{mode}...) //mode, 0 = sign with spend key, 1 = sign with view key
	buf = binary.AppendUvarint(buf, uint64(len(message)))
	buf = append(buf, message...)
	return types.Hash(moneroutil.Keccak256(buf))
}

func (a *Address) Verify(message []byte, signature string) SignatureVerifyResult {
	var hash types.Hash

	if strings.HasPrefix(signature, "SigV1") {
		hash = types.Hash(moneroutil.Keccak256(message))
	} else if strings.HasPrefix(signature, "SigV2") {
		hash = a.getMessageHash(message, 0)
	} else {
		return ResultFail
	}
	raw := moneroutil.DecodeMoneroBase58(signature[5:])

	sig := crypto.NewSignatureFromBytes(raw)

	if sig == nil {
		return ResultFail
	}

	if crypto.CheckSignature(hash, a.SpendPub, sig) {
		return ResultSuccessSpend
	}

	if strings.HasPrefix(signature, "SigV2") {
		hash = a.getMessageHash(message, 1)
	}

	if crypto.CheckSignature(hash, a.ViewPub, sig) {
		return ResultSuccessView
	}

	return ResultFail
}
