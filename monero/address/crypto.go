package address

import (
	"bytes"
	"encoding/binary"
	"filippo.io/edwards25519"
	"git.gammaspectra.live/P2Pool/moneroutil"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"strings"
)

func (a *Address) GetDerivationForPrivateKey(privateKey *edwards25519.Scalar) *edwards25519.Point {
	point := (&edwards25519.Point{}).ScalarMult(privateKey, &a.ViewPub)
	return (&edwards25519.Point{}).ScalarMult(scalar8, point)
}

func GetDerivationSharedDataForOutputIndex(derivation *edwards25519.Point, outputIndex uint64) *edwards25519.Scalar {
	varIntBuf := make([]byte, binary.MaxVarintLen64)
	data := append(derivation.Bytes(), varIntBuf[:binary.PutUvarint(varIntBuf, outputIndex)]...)
	return crypto.HashToScalar(data)
}

func GetDerivationViewTagForOutputIndex(derivation *edwards25519.Point, outputIndex uint64) uint8 {
	buf := make([]byte, 8+types.HashSize+binary.MaxVarintLen64)
	copy(buf, "view_tag")
	copy(buf[8:], derivation.Bytes())
	binary.PutUvarint(buf[8+types.HashSize:], outputIndex)

	h := moneroutil.Keccak256(buf)
	return h[0]
}

func (a *Address) GetDeterministicTransactionPrivateKey(prevId types.Hash) *edwards25519.Scalar {
	//TODO: cache
	entropy := make([]byte, 0, 13+types.HashSize*2)
	entropy = append(entropy, "tx_secret_key"...)
	entropy = append(entropy, a.SpendPub.Bytes()...)
	entropy = append(entropy, prevId[:]...)
	return crypto.DeterministicScalar(entropy)
}

func (a *Address) GetPublicKeyForSharedData(sharedData *edwards25519.Scalar) *edwards25519.Point {
	sG := (&edwards25519.Point{}).ScalarBaseMult(sharedData)

	return (&edwards25519.Point{}).Add(sG, &a.SpendPub)

}

func (a *Address) GetEphemeralPublicKey(txKey types.Hash, outputIndex uint64) (result types.Hash) {
	pK, _ := edwards25519.NewScalar().SetCanonicalBytes(txKey[:])
	copy(result[:], a.GetPublicKeyForSharedData(GetDerivationSharedDataForOutputIndex(a.GetDerivationForPrivateKey(pK), outputIndex)).Bytes())

	return
}

func (a *Address) GetTxProof(txId types.Hash, txKey types.Hash, message string) string {

	var prefixData []byte
	prefixData = append(prefixData, txId[:]...)
	prefixData = append(prefixData, []byte(message)...)
	prefixHash := types.Hash(moneroutil.Keccak256(prefixData))

	txKeyS, _ := edwards25519.NewScalar().SetCanonicalBytes(txKey[:])

	sharedSecret := (&edwards25519.Point{}).ScalarMult(txKeyS, &a.ViewPub)
	txPublicKey := (&edwards25519.Point{}).ScalarBaseMult(txKeyS)

	// pick random k
	k := crypto.RandomScalar()
	//k := crypto.HashToScalar(txId)

	buf := make([]byte, 0)
	buf = append(buf, prefixHash[:]...)        //msg
	buf = append(buf, sharedSecret.Bytes()...) //D

	X := (&edwards25519.Point{}).ScalarBaseMult(k)
	buf = append(buf, X.Bytes()...) //X

	Y := (&edwards25519.Point{}).ScalarMult(k, &a.ViewPub)
	buf = append(buf, Y.Bytes()...) //Y

	sep := types.Hash(moneroutil.Keccak256([]byte("TXPROOF_V2"))) // HASH_KEY_TXPROOF_V2
	buf = append(buf, sep[:]...)                                  //sep

	buf = append(buf, txPublicKey.Bytes()...)         //R
	buf = append(buf, a.ViewPub.Bytes()...)           //A
	buf = append(buf, bytes.Repeat([]byte{0}, 32)...) //B

	sig := &crypto.Signature{}

	sig.C = crypto.HashToScalar(buf)

	sig.R = edwards25519.NewScalar().Subtract(k, edwards25519.NewScalar().Multiply(sig.C, txKeyS))

	return "OutProofV2" + moneroutil.EncodeMoneroBase58(sharedSecret.Bytes()) + moneroutil.EncodeMoneroBase58(append(sig.C.Bytes(), sig.R.Bytes()...))
}

func (a *Address) GetTxProofV1(txId types.Hash, txKey types.Hash, message string) string {

	var prefixData []byte
	prefixData = append(prefixData, txId[:]...)
	prefixData = append(prefixData, []byte(message)...)
	prefixHash := types.Hash(moneroutil.Keccak256(prefixData))

	txKeyS, _ := edwards25519.NewScalar().SetCanonicalBytes(txKey[:])

	sharedSecret := (&edwards25519.Point{}).ScalarMult(txKeyS, &a.ViewPub)

	// pick random k
	k := crypto.RandomScalar()
	//k := crypto.HashToScalar(txId)

	buf := make([]byte, 0)
	buf = append(buf, prefixHash[:]...)        //msg
	buf = append(buf, sharedSecret.Bytes()...) //D

	X := (&edwards25519.Point{}).ScalarBaseMult(k)
	buf = append(buf, X.Bytes()...) //X

	Y := (&edwards25519.Point{}).ScalarMult(k, &a.ViewPub)
	buf = append(buf, Y.Bytes()...) //Y

	sig := &crypto.Signature{}

	sig.C = crypto.HashToScalar(buf)

	sig.R = edwards25519.NewScalar().Subtract(k, edwards25519.NewScalar().Multiply(sig.C, txKeyS))

	return "OutProofV1" + moneroutil.EncodeMoneroBase58(sharedSecret.Bytes()) + moneroutil.EncodeMoneroBase58(append(sig.C.Bytes(), sig.R.Bytes()...))
}

type SignatureVerifyResult int

const (
	ResultFail SignatureVerifyResult = iota
	ResultSuccessSpend
	ResultSuccessView
)

func (a *Address) getMessageHash(message []byte, mode uint8) types.Hash {
	buf := make([]byte, 0)
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

	if crypto.CheckSignature(hash, &a.SpendPub, sig) {
		return ResultSuccessSpend
	}

	if strings.HasPrefix(signature, "SigV2") {
		hash = a.getMessageHash(message, 1)
	}

	if crypto.CheckSignature(hash, &a.ViewPub, sig) {
		return ResultSuccessView
	}

	return ResultFail
}
