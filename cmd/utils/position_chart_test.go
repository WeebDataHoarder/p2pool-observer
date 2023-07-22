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

func TestChartIncrement(t *testing.T) {
	p := NewPositionChart(32, 32)
	for i := 0; i < 32; i++ {
		p.Add(i, uint64(i))
	}
	t.Log(p.String())

}
