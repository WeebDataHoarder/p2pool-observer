package utils

func Shorten(value string, n int) string {
	if len(value) <= n*2+3 {
		return value
	} else {
		return value[:n] + "..." + value[len(value)-n:]
	}
}
