package address

import (
	"encoding/binary"
	"git.gammaspectra.live/P2Pool/edwards25519"
	"git.gammaspectra.live/P2Pool/moneroutil"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	p2poolcrypto "git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/sha3"
	"strings"
)

// ZeroPrivateKeyAddress Special address with private keys set to both zero.
// Useful to detect unsupported signatures from hardware wallets on Monero GUI
var ZeroPrivateKeyAddress PackedAddress

func init() {
	ZeroPrivateKeyAddress[PackedAddressSpend] = crypto.ZeroPrivateKeyBytes.PublicKey().AsBytes()
	ZeroPrivateKeyAddress[PackedAddressView] = crypto.ZeroPrivateKeyBytes.PublicKey().AsBytes()
}

func GetDeterministicTransactionPrivateKey(seed types.Hash, prevId types.Hash) crypto.PrivateKey {
	return p2poolcrypto.GetDeterministicTransactionPrivateKey(seed, prevId)
}

func GetPublicKeyForSharedData(a Interface, sharedData crypto.PrivateKey) crypto.PublicKey {
	return sharedData.PublicKey().AsPoint().Add(a.SpendPublicKey().AsPoint())
}

func GetEphemeralPublicKey(a Interface, txKey crypto.PrivateKey, outputIndex uint64) crypto.PublicKey {
	return GetPublicKeyForSharedData(a, crypto.GetDerivationSharedDataForOutputIndex(txKey.GetDerivationCofactor(a.ViewPublicKey()), outputIndex))
}

func getEphemeralPublicKeyInline(spendPub, viewPub *edwards25519.Point, txKey *edwards25519.Scalar, outputIndex uint64, p *edwards25519.Point) {
	//derivation
	p.UnsafeVarTimeScalarMult(txKey, viewPub).MultByCofactor(p)

	derivationAsBytes := p.Bytes()
	var varIntBuf [binary.MaxVarintLen64]byte

	sharedData := crypto.HashToScalarNoAllocate(derivationAsBytes, varIntBuf[:binary.PutUvarint(varIntBuf[:], outputIndex)])

	//public key + add
	p.ScalarBaseMult(&sharedData).Add(p, spendPub)
}

func GetEphemeralPublicKeyAndViewTag(a Interface, txKey crypto.PrivateKey, outputIndex uint64) (crypto.PublicKey, uint8) {
	pK, viewTag := crypto.GetDerivationSharedDataAndViewTagForOutputIndex(txKey.GetDerivationCofactor(a.ViewPublicKey()), outputIndex)
	return GetPublicKeyForSharedData(a, pK), viewTag
}

// GetEphemeralPublicKeyAndViewTagNoAllocate Special version of GetEphemeralPublicKeyAndViewTag
func GetEphemeralPublicKeyAndViewTagNoAllocate(spendPublicKeyPoint *edwards25519.Point, derivation crypto.PublicKeyBytes, txKey *edwards25519.Scalar, outputIndex uint64, hasher *sha3.HasherState) (crypto.PublicKeyBytes, uint8) {
	var intermediatePublicKey, ephemeralPublicKey edwards25519.Point
	derivationSharedData, viewTag := crypto.GetDerivationSharedDataAndViewTagForOutputIndexNoAllocate(derivation, outputIndex, hasher)

	intermediatePublicKey.ScalarBaseMult(&derivationSharedData)
	ephemeralPublicKey.Add(&intermediatePublicKey, spendPublicKeyPoint)

	var ephemeralPublicKeyBytes crypto.PublicKeyBytes
	copy(ephemeralPublicKeyBytes[:], ephemeralPublicKey.Bytes())

	return ephemeralPublicKeyBytes, viewTag
}

// GetDerivationNoAllocate Special version
func GetDerivationNoAllocate(viewPublicKeyPoint *edwards25519.Point, txKey *edwards25519.Scalar) crypto.PublicKeyBytes {
	var point, derivation edwards25519.Point
	point.UnsafeVarTimeScalarMult(txKey, viewPublicKeyPoint)
	derivation.MultByCofactor(&point)

	return crypto.PublicKeyBytes(derivation.Bytes())
}

func GetTxProofV2(a Interface, txId types.Hash, txKey crypto.PrivateKey, message string) string {
	prefixHash := crypto.Keccak256(txId[:], []byte(message))

	sharedSecret, signature := crypto.GenerateTxProofV2(prefixHash, txKey, a.ViewPublicKey(), nil)

	return "OutProofV2" + string(moneroutil.EncodeMoneroBase58(sharedSecret.AsSlice())) + string(moneroutil.EncodeMoneroBase58(signature.Bytes()))
}

func GetTxProofV1(a Interface, txId types.Hash, txKey crypto.PrivateKey, message string) string {
	prefixHash := crypto.Keccak256(txId[:], []byte(message))

	sharedSecret, signature := crypto.GenerateTxProofV1(prefixHash, txKey, a.ViewPublicKey(), nil)

	return "OutProofV1" + string(moneroutil.EncodeMoneroBase58(sharedSecret.AsSlice())) + string(moneroutil.EncodeMoneroBase58(signature.Bytes()))
}

type SignatureVerifyResult int

const (
	ResultFailZeroSpend SignatureVerifyResult = -2
	ResultFailZeroView  SignatureVerifyResult = -1
)
const (
	ResultFail = SignatureVerifyResult(iota)
	ResultSuccessSpend
	ResultSuccessView
)

func GetMessageHash(a Interface, message []byte, mode uint8) types.Hash {
	return crypto.Keccak256(
		[]byte("MoneroMessageSignature\x00"),
		a.SpendPublicKey().AsSlice(),
		a.ViewPublicKey().AsSlice(),
		[]byte{mode},
		binary.AppendUvarint(nil, uint64(len(message))),
		message,
	)
}

func VerifyMessage(a Interface, message []byte, signature string) SignatureVerifyResult {
	var hash types.Hash

	if strings.HasPrefix(signature, "SigV1") {
		hash = crypto.Keccak256(message)
	} else if strings.HasPrefix(signature, "SigV2") {
		hash = GetMessageHash(a, message, 0)
	} else {
		return ResultFail
	}
	raw := moneroutil.DecodeMoneroBase58([]byte(signature[5:]))

	sig := crypto.NewSignatureFromBytes(raw)

	if sig == nil {
		return ResultFail
	}

	if crypto.VerifyMessageSignature(hash, a.SpendPublicKey(), sig) {
		return ResultSuccessSpend
	}

	// Special mode: view wallets in Monero GUI could generate signatures with spend public key proper, with message hash of spend wallet mode, but zero spend private key
	if crypto.VerifyMessageSignatureSplit(hash, a.SpendPublicKey(), ZeroPrivateKeyAddress.SpendPublicKey(), sig) {
		return ResultFailZeroSpend
	}

	if strings.HasPrefix(signature, "SigV2") {
		hash = GetMessageHash(a, message, 1)
	}

	if crypto.VerifyMessageSignature(hash, a.ViewPublicKey(), sig) {
		return ResultSuccessView
	}

	return ResultFail
}

// VerifyMessageFallbackToZero Check for Monero GUI behavior to generate wrong signatures on view-only wallets
func VerifyMessageFallbackToZero(a Interface, message []byte, signature string) SignatureVerifyResult {
	var hash types.Hash

	if strings.HasPrefix(signature, "SigV1") {
		hash = crypto.Keccak256(message)
	} else if strings.HasPrefix(signature, "SigV2") {
		hash = GetMessageHash(a, message, 0)
	} else {
		return ResultFail
	}
	raw := moneroutil.DecodeMoneroBase58([]byte(signature[5:]))

	sig := crypto.NewSignatureFromBytes(raw)

	if sig == nil {
		return ResultFail
	}

	if crypto.VerifyMessageSignature(hash, a.SpendPublicKey(), sig) {
		return ResultSuccessSpend
	}

	// Special mode: view wallets in Monero GUI could generate signatures with spend public key proper, with message hash of spend wallet mode, but zero spend private key
	if crypto.VerifyMessageSignatureSplit(hash, a.SpendPublicKey(), ZeroPrivateKeyAddress.SpendPublicKey(), sig) {
		return ResultFailZeroSpend
	}

	if strings.HasPrefix(signature, "SigV2") {
		hash = GetMessageHash(a, message, 1)
	}

	if crypto.VerifyMessageSignature(hash, a.ViewPublicKey(), sig) {
		return ResultSuccessView
	}

	// Special mode
	if crypto.VerifyMessageSignatureSplit(hash, a.ViewPublicKey(), ZeroPrivateKeyAddress.ViewPublicKey(), sig) {
		return ResultFailZeroView
	}

	return ResultFail
}
