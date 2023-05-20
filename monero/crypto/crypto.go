package crypto

import (
	"runtime"
	"unsafe"
)

// CompareConsensusPublicKeyBytes Compares public keys in a special consensus specific way
func CompareConsensusPublicKeyBytes(a, b PublicKeyBytes) int {
	aUint64 := (*[PublicKeySize / 8]uint64)(unsafe.Pointer(&a))
	bUint64 := (*[PublicKeySize / 8]uint64)(unsafe.Pointer(&b))

	if aUint64[3] < bUint64[3] {
		return -1
	}
	if aUint64[3] > bUint64[3] {
		return 1
	}

	if aUint64[2] < bUint64[2] {
		return -1
	}
	if aUint64[2] > bUint64[2] {
		return 1
	}

	if aUint64[1] < bUint64[1] {
		return -1
	}
	if aUint64[1] > bUint64[1] {
		return 1
	}

	if aUint64[0] < bUint64[0] {
		return -1
	}
	if aUint64[0] > bUint64[0] {
		return 1
	}

	//golang might free other otherwise
	runtime.KeepAlive(aUint64)
	runtime.KeepAlive(bUint64)
	return 0
}
