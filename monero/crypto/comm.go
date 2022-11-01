package crypto

import (
	"filippo.io/edwards25519"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
)

type SignatureComm struct {
	Hash types.Hash
	Key  *edwards25519.Point
	Comm *edwards25519.Point
}

func (s *SignatureComm) Bytes() []byte {
	buf := make([]byte, 0, types.HashSize*3)
	buf = append(buf, s.Hash[:]...)
	buf = append(buf, s.Key.Bytes()...)
	buf = append(buf, s.Comm.Bytes()...)
	return buf
}

// SignatureComm_2 Used in v1/v2 tx proofs
type SignatureComm_2 struct {
	Message types.Hash
	// KeyDerivation D
	KeyDerivation *edwards25519.Point
	// RandomPublicKey X
	RandomPublicKey *edwards25519.Point
	// RandomDerivation Y
	RandomDerivation *edwards25519.Point
	// Separator Domain Separation
	Separator types.Hash
	// TransactionPublicKey R
	TransactionPublicKey *edwards25519.Point
	// RecipientViewPublicKey A
	RecipientViewPublicKey  *edwards25519.Point
	RecipientSpendPublicKey *edwards25519.Point
}

func (s *SignatureComm_2) Bytes() []byte {
	buf := make([]byte, 0, types.HashSize*8)
	buf = append(buf, s.Message[:]...)
	buf = append(buf, s.KeyDerivation.Bytes()...)
	buf = append(buf, s.RandomPublicKey.Bytes()...)
	buf = append(buf, s.RandomDerivation.Bytes()...)
	buf = append(buf, s.Separator[:]...)
	buf = append(buf, s.TransactionPublicKey.Bytes()...)
	buf = append(buf, s.RecipientViewPublicKey.Bytes()...)
	if s.RecipientSpendPublicKey == nil {
		buf = append(buf, make([]byte, types.HashSize)...)
	} else {
		buf = append(buf, s.RecipientSpendPublicKey.Bytes()...)
	}
	return buf
}

// SignatureComm_2_V1 Used in v1 tx proofs
type SignatureComm_2_V1 struct {
	Message types.Hash
	// KeyDerivation D
	KeyDerivation *edwards25519.Point
	// RandomPublicKey X
	RandomPublicKey *edwards25519.Point
	// RandomDerivation Y
	RandomDerivation *edwards25519.Point
}

func (s *SignatureComm_2_V1) Bytes() []byte {
	buf := make([]byte, 0, types.HashSize*4)
	buf = append(buf, s.Message[:]...)
	buf = append(buf, s.KeyDerivation.Bytes()...)
	buf = append(buf, s.RandomPublicKey.Bytes()...)
	buf = append(buf, s.RandomDerivation.Bytes()...)
	return buf
}
