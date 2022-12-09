package utils

import (
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

func (b *CircularBuffer[T]) PushUnique(value T) bool {
	b.lock.Lock()
	defer b.lock.Unlock()

	for _, v := range b.buffer {
		if value == v {
			return false
		}
	}

	b.buffer[b.index.Add(1)%uint32(len(b.buffer))] = value

	return true
}

func (b *CircularBuffer[T]) Exists(value T) bool {
	b.lock.RLock()
	defer b.lock.RUnlock()
	for _, v := range b.buffer {
		if value == v {
			return true
		}
	}

	return false
}

func (b *CircularBuffer[T]) Slice() []T {
	s := make([]T, len(b.buffer))
	b.lock.RLock()
	defer b.lock.RUnlock()
	copy(s, b.buffer)
	return s
}
