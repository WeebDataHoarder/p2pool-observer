package crypto

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
)

// SignatureComm Used in normal message signatures
type SignatureComm struct {
	Hash types.Hash
	Key  PublicKey
	Comm PublicKey
}

func (s *SignatureComm) Bytes() []byte {
	buf := make([]byte, 0, types.HashSize+PublicKeySize*2)
	buf = append(buf, s.Hash[:]...)
	buf = append(buf, s.Key.AsSlice()...)
	buf = append(buf, s.Comm.AsSlice()...)
	return buf
}

// SignatureComm_2 Used in v1/v2 tx proofs
type SignatureComm_2 struct {
	Message types.Hash
	// KeyDerivation D
	KeyDerivation PublicKey
	// RandomPublicKey X
	RandomPublicKey PublicKey
	// RandomDerivation Y
	RandomDerivation PublicKey
	// Separator Domain Separation
	Separator types.Hash
	// TransactionPublicKey R
	TransactionPublicKey PublicKey
	// RecipientViewPublicKey A
	RecipientViewPublicKey  PublicKey
	RecipientSpendPublicKey PublicKey
}

func (s *SignatureComm_2) Bytes() []byte {
	buf := make([]byte, 0, types.HashSize*2+PublicKeySize*6)
	buf = append(buf, s.Message[:]...)
	buf = append(buf, s.KeyDerivation.AsSlice()...)
	buf = append(buf, s.RandomPublicKey.AsSlice()...)
	buf = append(buf, s.RandomDerivation.AsSlice()...)
	buf = append(buf, s.Separator[:]...)
	buf = append(buf, s.TransactionPublicKey.AsSlice()...)
	buf = append(buf, s.RecipientViewPublicKey.AsSlice()...)
	if s.RecipientSpendPublicKey == nil {
		buf = append(buf, types.ZeroHash[:]...)
	} else {
		buf = append(buf, s.RecipientSpendPublicKey.AsSlice()...)
	}
	return buf
}

// SignatureComm_2_V1 Used in v1 tx proofs
type SignatureComm_2_V1 struct {
	Message types.Hash
	// KeyDerivation D
	KeyDerivation PublicKey
	// RandomPublicKey X
	RandomPublicKey PublicKey
	// RandomDerivation Y
	RandomDerivation PublicKey
}

func (s *SignatureComm_2_V1) Bytes() []byte {
	buf := make([]byte, 0, types.HashSize+PublicKeySize*3)
	buf = append(buf, s.Message[:]...)
	buf = append(buf, s.KeyDerivation.AsSlice()...)
	buf = append(buf, s.RandomPublicKey.AsSlice()...)
	buf = append(buf, s.RandomDerivation.AsSlice()...)
	return buf
}
