package utils

import (
	"math"
)

func ProbabilityEffort(effort float64) float64 {
	return 1 - math.Exp(-effort/100)
}

func ProbabilityMode(i ...float64) (n float64) {
	//cannot use max as it's not variadic
	for _, item := range i {
		if item > n {
			n = item
		}
	}
	return n
}

func ProbabilityNShares(shares uint64, effort float64) float64 {
	num := math.Pow(effort/100, float64(shares))
	den := float64(Factorial(shares))

	return (num / den) * math.Exp(-effort/100)
}

// Factorial Valid for small n
func Factorial(n uint64) (result uint64) {
	if n > 0 {
		result = n * Factorial(n-1)
		return result
	}
	return 1
}
