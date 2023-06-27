package utils

import "slices"

func ReverseSlice[S ~[]E, E any](s S) S {
	slices.Reverse(s)

	return s
}
