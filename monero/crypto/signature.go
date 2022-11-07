package crypto

import (
	"filippo.io/edwards25519"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
)

// Signature Schnorr signature
type Signature struct {
	// C hash of data in signature, also called e
	C *edwards25519.Scalar
	// R result of the signature, also called s
	R *edwards25519.Scalar
}

// SignatureSigningHandler receives k, inserts it or a pubkey into its data, and produces a []byte buffer for Signing/Verifying
type SignatureSigningHandler func(r PrivateKey) []byte

// SignatureVerificationHandler receives r = pubkey(k), inserts it into its data, and produces a []byte buffer for Signing/Verifying
type SignatureVerificationHandler func(r PublicKey) []byte

func NewSignatureFromBytes(buf []byte) *Signature {
	if len(buf) != types.HashSize*2 {
		return nil
	}
	signature := &Signature{}
	var err error
	if signature.C, err = GetEdwards25519Scalar().SetCanonicalBytes(buf[:32]); err != nil {
		return nil
	} else if signature.R, err = GetEdwards25519Scalar().SetCanonicalBytes(buf[32:]); err != nil {
		return nil
	} else {
		return signature
	}
}

func (s *Signature) Bytes() []byte {
	buf := make([]byte, 0, types.HashSize*2)
	buf = append(buf, s.C.Bytes()...)
	buf = append(buf, s.R.Bytes()...)
	return buf
}

// Verify checks a Schnorr Signature using H = keccak
func (s *Signature) Verify(handler SignatureVerificationHandler, publicKey PublicKey) (ok bool, r *PublicKeyPoint) {
	//s = C * k, R * G
	sp := GetEdwards25519Point().VarTimeDoubleScalarBaseMult(s.C, publicKey.AsPoint().Point(), s.R)
	if sp.Equal(infinityPoint) == 1 {
		return false, nil
	}
	r = PublicKeyFromPoint(sp)
	return s.C.Equal(HashToScalar(handler(r))) == 1, r
}

// CreateSignature produces a Schnorr Signature using H = keccak
func CreateSignature(handler SignatureSigningHandler, privateKey PrivateKey) *Signature {
	k := PrivateKeyFromScalar(RandomScalar())

	signature := &Signature{
		// e
		C: HashToScalar(handler(k)),
		R: &edwards25519.Scalar{},
	}

	// s = k - x * e
	// EdDSA is an altered version, with addition instead of subtraction
	signature.R = signature.R.Subtract(k.Scalar(), GetEdwards25519Scalar().Multiply(signature.C, privateKey.AsScalar().Scalar()))
	return signature
}

func CreateMessageSignature(prefixHash types.Hash, key PrivateKey) *Signature {
	buf := &SignatureComm{}
	buf.Hash = prefixHash
	buf.Key = key.PublicKey()

	return CreateSignature(func(k PrivateKey) []byte {
		buf.Comm = k.PublicKey()
		return buf.Bytes()
	}, key)
}

func VerifyMessageSignature(prefixHash types.Hash, publicKey PublicKey, signature *Signature) bool {
	buf := &SignatureComm{}
	buf.Hash = prefixHash
	buf.Key = publicKey

	ok, _ := signature.Verify(func(r PublicKey) []byte {
		buf.Comm = r
		return buf.Bytes()
	}, publicKey)
	return ok
}
