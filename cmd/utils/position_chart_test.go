package utils

import (
	"testing"
)

func TestChart(t *testing.T) {
	p := NewPositionChart(32, 4096)
	for i := 0; i < 4096; i++ {
		p.Add(i, 1)
	}
	t.Log(p.String())

}
