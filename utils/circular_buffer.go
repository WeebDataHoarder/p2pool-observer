package utils

import (
	"slices"
	"sync"
	"sync/atomic"
)

type CircularBuffer[T comparable] struct {
	buffer []T
	index  atomic.Uint32
	lock   sync.RWMutex
}

func NewCircularBuffer[T comparable](size int) *CircularBuffer[T] {
	return &CircularBuffer[T]{
		buffer: make([]T, size),
	}
}

func (b *CircularBuffer[T]) Push(value T) {
	b.lock.Lock()
	defer b.lock.Unlock()
	b.buffer[b.index.Add(1)%uint32(len(b.buffer))] = value
}

func (b *CircularBuffer[T]) Current() T {
	b.lock.RLock()
	defer b.lock.RUnlock()
	return b.buffer[b.index.Load()%uint32(len(b.buffer))]
}

func (b *CircularBuffer[T]) Get(index uint32) T {
	b.lock.RLock()
	defer b.lock.RUnlock()
	return b.buffer[index%uint32(len(b.buffer))]
}

func (b *CircularBuffer[T]) Replace(value, replace T) {
	b.lock.Lock()
	defer b.lock.Unlock()

	for i, v := range b.buffer {
		if value == v {
			b.buffer[i] = replace
		}
	}
}

func (b *CircularBuffer[T]) PushUnique(value T) bool {
	b.lock.Lock()
	defer b.lock.Unlock()

	if len(b.buffer) == 0 || slices.Contains(b.buffer, value) {
		return false
	}

	b.buffer[b.index.Add(1)%uint32(len(b.buffer))] = value

	return true
}

func (b *CircularBuffer[T]) Reverse() {
	b.lock.Lock()
	defer b.lock.Unlock()

	for i, j := 0, len(b.buffer)-1; i < j; i, j = i+1, j-1 {
		b.buffer[i], b.buffer[j] = b.buffer[j], b.buffer[i]
	}
}

func (b *CircularBuffer[T]) Exists(value T) bool {
	b.lock.RLock()
	defer b.lock.RUnlock()

	return slices.Contains(b.buffer, value)
}

func (b *CircularBuffer[T]) Slice() []T {
	s := make([]T, len(b.buffer))
	b.lock.RLock()
	defer b.lock.RUnlock()
	copy(s, b.buffer)
	return s
}
