package crypto

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
)

type SignatureComm struct {
	Hash types.Hash
	Key  PublicKey
	Comm PublicKey
}

func (s *SignatureComm) Bytes() []byte {
	buf := make([]byte, 0, types.HashSize*3)
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
	buf := make([]byte, 0, types.HashSize*8)
	buf = append(buf, s.Message[:]...)
	buf = append(buf, s.KeyDerivation.AsSlice()...)
	buf = append(buf, s.RandomPublicKey.AsSlice()...)
	buf = append(buf, s.RandomDerivation.AsSlice()...)
	buf = append(buf, s.Separator[:]...)
	buf = append(buf, s.TransactionPublicKey.AsSlice()...)
	buf = append(buf, s.RecipientViewPublicKey.AsSlice()...)
	if s.RecipientSpendPublicKey == nil {
		buf = append(buf, make([]byte, types.HashSize)...)
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
	buf := make([]byte, 0, types.HashSize*4)
	buf = append(buf, s.Message[:]...)
	buf = append(buf, s.KeyDerivation.AsSlice()...)
	buf = append(buf, s.RandomPublicKey.AsSlice()...)
	buf = append(buf, s.RandomDerivation.AsSlice()...)
	return buf
}
