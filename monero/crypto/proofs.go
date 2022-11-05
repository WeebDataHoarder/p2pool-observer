package crypto

import (
	"git.gammaspectra.live/P2Pool/moneroutil"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
)

var TxProofV2DomainSeparatorHash = types.Hash(moneroutil.Keccak256([]byte("TXPROOF_V2"))) // HASH_KEY_TXPROOF_V2
func GenerateTxProofV2(prefixHash types.Hash, txKey PrivateKey, recipientViewPublicKey PublicKey, recipientSpendPublicKey PublicKey) (derivation PublicKey, signature *Signature) {
	comm := &SignatureComm_2{}
	comm.Message = prefixHash

	//shared secret
	comm.KeyDerivation = txKey.GetDerivation(recipientViewPublicKey)

	comm.Separator = TxProofV2DomainSeparatorHash
	comm.TransactionPublicKey = txKey.PublicKey()
	comm.RecipientViewPublicKey = recipientViewPublicKey

	signature = CreateSignature(func(k PrivateKey) []byte {
		if recipientSpendPublicKey == nil {
			// compute RandomPublicKey = k*G
			comm.RandomPublicKey = k.PublicKey()
			comm.RecipientSpendPublicKey = nil
		} else {
			// compute RandomPublicKey = k*B
			comm.RandomPublicKey = k.GetDerivation(recipientSpendPublicKey)
			comm.RecipientSpendPublicKey = recipientSpendPublicKey
		}

		comm.RandomDerivation = k.GetDerivation(recipientViewPublicKey)

		return comm.Bytes()
	}, txKey)

	return comm.KeyDerivation, signature
}

func GenerateTxProofV1(prefixHash types.Hash, txKey PrivateKey, recipientViewPublicKey PublicKey, recipientSpendPublicKey PublicKey) (derivation PublicKey, signature *Signature) {
	comm := &SignatureComm_2_V1{}
	comm.Message = prefixHash

	//shared secret
	comm.KeyDerivation = txKey.GetDerivation(recipientViewPublicKey)

	signature = CreateSignature(func(k PrivateKey) []byte {
		if recipientSpendPublicKey == nil {
			// compute RandomPublicKey = k*G
			comm.RandomPublicKey = k.PublicKey()
		} else {
			// compute RandomPublicKey = k*B
			comm.RandomPublicKey = k.GetDerivation(recipientSpendPublicKey)
		}

		comm.RandomDerivation = k.GetDerivation(recipientViewPublicKey)

		return comm.Bytes()
	}, txKey)

	return comm.KeyDerivation, signature
}
