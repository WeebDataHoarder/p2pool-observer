package utils

func SliceCount[S ~[]E, E any](s S, f func(E) bool) (count int) {
	for i := range s {
		if f(s[i]) {
			count++
		}
	}

	return count
}
