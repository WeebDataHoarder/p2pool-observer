package address

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
)

type Interface interface {
	Compare(b Interface) int

	PublicKeys() (spend, view crypto.PublicKey)

	SpendPublicKey() *crypto.PublicKeyBytes
	ViewPublicKey() *crypto.PublicKeyBytes

	ToAddress(network uint8, err ...error) *Address
	ToPackedAddress() PackedAddress
}
