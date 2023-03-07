package utils

func XorShift64Star(x uint64) uint64 {
	x ^= x >> 12
	x ^= x << 25
	x ^= x >> 27
	return x * 0x2545F4914F6CDD1D
}
