package utils

import (
	"cmp"
)

// NthElementSlice QuickSelect implementation
// k is the desired index value, where array[k] is the k+1 smallest element
// values between s[0, k-1] are guaranteed <= to s[k]
// values between s[k+1, len(s)] are guaranteed >= to s[k]
func NthElementSlice[S ~[]E, E cmp.Ordered](s S, k int) {
	left := 0
	right := len(s) - 1
	for {
		if left == right {
			return
		}
		pivotIndex := (left + right) >> 1 // Use middle point as pivot. This could probably use random index
		pivotIndex = partition(s, pivotIndex, left, right)
		if k == pivotIndex {
			return
		} else if k < pivotIndex {
			right = pivotIndex - 1
		} else {
			left = pivotIndex + 1
		}
	}
}

// partition Partition values into less than s[pivot] and greater than s[pivot]
func partition[S ~[]E, E cmp.Ordered](s S, pivot, left, right int) int {
	s[pivot], s[right] = s[right], s[pivot] // Move pivot to end
	storeIndex := left
	for i := left; i < right; i++ {
		if s[i] < s[right] { // Compare to pivot value
			s[storeIndex], s[i] = s[i], s[storeIndex]
			storeIndex++
		}
	}

	s[right], s[storeIndex] = s[storeIndex], s[right] // Return pivot Index to position
	return storeIndex
}

// NthElementSliceFunc QuickSelect implementation
// k is the desired index value, where array[k] is the k+1 smallest element
// values between s[0, k-1] are guaranteed <= to s[k]
// values between s[k+1, len(s)] are guaranteed >= to s[k]
func NthElementSliceFunc[S ~[]E, E any](s S, cmp func(a, b E) int, k int) {
	left := 0
	right := len(s) - 1
	for {
		if left == right {
			return
		}
		pivotIndex := (left + right) >> 1 // Use middle point as pivot. This could probably use random index
		pivotIndex = partitionFunc(s, cmp, pivotIndex, left, right)
		if k == pivotIndex {
			return
		} else if k < pivotIndex {
			right = pivotIndex - 1
		} else {
			left = pivotIndex + 1
		}
	}
}

// partitionFunc Partition values into less than s[pivot] and greater than s[pivot]
func partitionFunc[S ~[]E, E any](s S, cmp func(a, b E) int, pivot, left, right int) int {
	s[pivot], s[right] = s[right], s[pivot] // Move pivot to end
	storeIndex := left
	for i := left; i < right; i++ {
		if cmp(s[i], s[right]) < 0 { // Compare to pivot value
			s[storeIndex], s[i] = s[i], s[storeIndex]
			storeIndex++
		}
	}

	s[right], s[storeIndex] = s[storeIndex], s[right] // Return pivot Index to position
	return storeIndex
}
