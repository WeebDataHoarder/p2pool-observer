package main

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"golang.org/x/exp/slices"
)

type PositionChart struct {
	totalItems uint64
	bucket     []uint64
	idle       byte
}

func (p *PositionChart) Add(index int, value uint64) {
	if index < 0 || index > int(p.totalItems) {
		return
	}
	if len(p.bucket) == 1 {
		p.bucket[0] += value
		return
	}
	i := uint64(index) / ((p.totalItems + uint64(len(p.bucket)) - 1) / uint64(len(p.bucket)))
	p.bucket[i] += value
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

func (p *PositionChart) StringWithSeparator(index int) string {
	if index < 0 || index >= int(p.totalItems) {
		return p.String()
	}
	separatorIndex := int(uint64(index) / ((p.totalItems + uint64(len(p.bucket)) - 1) / uint64(len(p.bucket))))
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
	return &PositionChart{
		totalItems: totalItems,
		bucket:     make([]uint64, size),
		idle:       '.',
	}
}
