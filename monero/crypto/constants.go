package crypto

import "filippo.io/edwards25519"

var infinityPoint = edwards25519.NewIdentityPoint()
var zeroScalar = edwards25519.NewScalar()
var scalar8, _ = edwards25519.NewScalar().SetCanonicalBytes([]byte{8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
