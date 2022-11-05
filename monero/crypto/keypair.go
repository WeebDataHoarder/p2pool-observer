package crypto

type KeyPair struct {
	PrivateKey PrivateKey
	PublicKey  PublicKey
}

func NewKeyPairFromPrivate(privateKey PrivateKey) *KeyPair {
	return &KeyPair{
		PrivateKey: privateKey,
		PublicKey:  privateKey.PublicKey(),
	}
}