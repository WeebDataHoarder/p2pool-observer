package utils

func Shorten(value string, n int) string {
	if len(value) <= n*2+3 {
		return value
	} else {
		return value[:n] + "..." + value[len(value)-n:]
	}
}

func ShortenSlice(value []byte, n int) []byte {
	if len(value) <= n*2+3 {
		return value
	} else {
		copy(value[n+3:], value[len(value)-n:])
		value[n] = '.'
		value[n+1] = '.'
		value[n+2] = '.'
		return value[:n*2+3]
	}
}
