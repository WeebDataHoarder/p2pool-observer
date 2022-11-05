package address

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
)

type Interface interface {
	Compare(b Interface) int

	PublicKeys() (spend, view crypto.PublicKey)

	SpendPublicKey() crypto.PublicKey
	ViewPublicKey() crypto.PublicKey

	ToAddress() *Address
	ToPackedAddress() *PackedAddress

	ToBase58() string
}