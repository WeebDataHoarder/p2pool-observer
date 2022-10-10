package utils

import "fmt"

func SiUnits(number float64, decimals int) string {
	if number >= 1000000000 {
		return fmt.Sprintf("%.*f G", decimals, number/1000000000)
	} else if number >= 1000000 {
		return fmt.Sprintf("%.*f M", decimals, number/1000000)
	} else if number >= 1000 {
		return fmt.Sprintf("%.*f K", decimals, number/1000)
	}

	return fmt.Sprintf("%.*f", decimals, number)
}
