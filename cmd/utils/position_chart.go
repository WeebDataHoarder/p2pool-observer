package utils

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"slices"
)

type PositionChart struct {
	totalItems uint64
	bucket     []uint64
	perBucket  int
	idle       byte
}

func (p *PositionChart) Add(index int, value uint64) {
	if index < 0 || index >= int(p.totalItems) {
		return
	}
	if len(p.bucket) == 1 {
		p.bucket[0] += value
		return
	}

	p.bucket[p.indexOf(index)] += value
}

func (p *PositionChart) indexOf(index int) int {
	if len(p.bucket) == 1 {
		return 0
	}
	i := (index*len(p.bucket) - 1) / int(p.totalItems-1)

	return i
}

func (p *PositionChart) Total() (result uint64) {
	for _, e := range p.bucket {
		result += e
	}
	return
}

func (p *PositionChart) Size() uint64 {
	return uint64(len(p.bucket))
}

func (p *PositionChart) Resolution() uint64 {
	return p.totalItems / uint64(len(p.bucket))
}

func (p *PositionChart) SetIdle(idleChar byte) {
	p.idle = idleChar
}

func (p *PositionChart) String() string {
	position := make([]byte, 2*2+len(p.bucket))
	position[0], position[1] = '[', '<'
	position[len(position)-2], position[len(position)-1] = '<', ']'
	for i, e := range utils.ReverseSlice(slices.Clone(p.bucket)) {
		if e > 0 {
			if e > 9 {
				position[2+i] = '+'
			} else {
				position[2+i] = 0x30 + byte(e)
			}
		} else {
			position[2+i] = p.idle
		}
	}

	return string(position)
}

func (p *PositionChart) StringWithoutDelimiters() string {
	position := make([]byte, len(p.bucket))
	for i, e := range utils.ReverseSlice(slices.Clone(p.bucket)) {
		if e > 0 {
			if e > 9 {
				position[i] = '+'
			} else {
				position[i] = 0x30 + byte(e)
			}
		} else {
			position[i] = p.idle
		}
	}

	return string(position)
}

func (p *PositionChart) StringWithSeparator(index int) string {
	if index < 0 || index >= int(p.totalItems) {
		return p.String()
	}
	separatorIndex := p.indexOf(index)
	position := make([]byte, 1+2*2+len(p.bucket))
	position[0], position[1] = '[', '<'
	position[2+separatorIndex] = '|'
	position[len(position)-2], position[len(position)-1] = '<', ']'
	for i, e := range utils.ReverseSlice(slices.Clone(p.bucket)) {
		if i >= separatorIndex {
			i++
		}
		if e > 0 {
			if e > 9 {
				position[2+i] = '+'
			} else {
				position[2+i] = 0x30 + byte(e)
			}
		} else {
			position[2+i] = p.idle
		}
	}

	return string(position)
}

func NewPositionChart(size uint64, totalItems uint64) *PositionChart {
	if size < 1 {
		size = 1
	}
	perBucket := int(totalItems / size)
	if totalItems%size > 0 {
		perBucket += 1
	}
	return &PositionChart{
		totalItems: totalItems,
		bucket:     make([]uint64, size),
		perBucket:  perBucket,
		idle:       '.',
	}
}
