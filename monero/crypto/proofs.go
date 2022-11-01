package crypto

import (
	"filippo.io/edwards25519"
	"git.gammaspectra.live/P2Pool/moneroutil"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
)

var TxProofV2DomainSeparatorHash = types.Hash(moneroutil.Keccak256([]byte("TXPROOF_V2"))) // HASH_KEY_TXPROOF_V2
func GenerateTxProofV2(prefixHash types.Hash, txKeyPair *KeyPair, recipientViewPublicKey *edwards25519.Point, recipientSpendPublicKey *edwards25519.Point) (derivation *edwards25519.Point, signature *Signature) {
	// pick random k
	k := RandomScalar()

	comm := &SignatureComm_2{}
	comm.Message = prefixHash

	//shared secret
	comm.KeyDerivation = (&edwards25519.Point{}).ScalarMult(txKeyPair.PrivateKey, recipientViewPublicKey)

	if recipientSpendPublicKey == nil {
		// compute RandomPublicKey = k*G
		comm.RandomPublicKey = (&edwards25519.Point{}).ScalarBaseMult(k)
		comm.RecipientSpendPublicKey = nil
	} else {
		// compute RandomPublicKey = k*B
		comm.RandomPublicKey = (&edwards25519.Point{}).ScalarMult(k, recipientSpendPublicKey)
		comm.RecipientSpendPublicKey = recipientSpendPublicKey
	}

	comm.RandomDerivation = (&edwards25519.Point{}).ScalarMult(k, recipientViewPublicKey)
	comm.Separator = TxProofV2DomainSeparatorHash
	comm.TransactionPublicKey = txKeyPair.PublicKey
	comm.RecipientViewPublicKey = recipientViewPublicKey

	signature = &Signature{}

	signature.C = HashToScalar(comm.Bytes())

	signature.R = edwards25519.NewScalar().Subtract(k, edwards25519.NewScalar().Multiply(signature.C, txKeyPair.PrivateKey))

	return comm.KeyDerivation, signature
}

func GenerateTxProofV1(prefixHash types.Hash, txKeyPair *KeyPair, recipientViewPublicKey *edwards25519.Point, recipientSpendPublicKey *edwards25519.Point) (derivation *edwards25519.Point, signature *Signature) {
	// pick random k
	k := RandomScalar()

	comm := &SignatureComm_2_V1{}
	comm.Message = prefixHash

	//shared secret
	comm.KeyDerivation = (&edwards25519.Point{}).ScalarMult(txKeyPair.PrivateKey, recipientViewPublicKey)

	if recipientSpendPublicKey == nil {
		// compute RandomPublicKey = k*G
		comm.RandomPublicKey = (&edwards25519.Point{}).ScalarBaseMult(k)
	} else {
		// compute RandomPublicKey = k*B
		comm.RandomPublicKey = (&edwards25519.Point{}).ScalarMult(k, recipientSpendPublicKey)
	}

	comm.RandomDerivation = (&edwards25519.Point{}).ScalarMult(k, recipientViewPublicKey)

	signature = &Signature{}

	signature.C = HashToScalar(comm.Bytes())

	signature.R = edwards25519.NewScalar().Subtract(k, edwards25519.NewScalar().Multiply(signature.C, txKeyPair.PrivateKey))

	return comm.KeyDerivation, signature
}
