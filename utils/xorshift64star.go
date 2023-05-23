package utils

// XorShift64Star Implementation of xorshift* https://en.wikipedia.org/wiki/Xorshift#xorshift*
// x must be initialized to a non-zero value
func XorShift64Star(x uint64) uint64 {
	x ^= x >> 12
	x ^= x << 25
	x ^= x >> 27
	return x * 0x2545F4914F6CDD1D
}
