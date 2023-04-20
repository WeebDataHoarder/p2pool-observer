package main

import (
	"sync"
	"time"
)

var cache = make(map[string]any)
var cacheLock sync.RWMutex

type cachedResult[T any] struct {
	t      time.Time
	result T
}

func cacheResult[T any](k string, cacheTime time.Duration, result func() T) T {
	var zeroVal T
	if r, ok := func() (T, bool) {
		if cacheTime > 0 {
			cacheLock.RLock()
			defer cacheLock.RUnlock()

			if r := cache[k]; r != nil {
				if r2, ok := r.(*cachedResult[T]); ok && r2 != nil && r2.t.Add(cacheTime).After(time.Now()) {
					return r2.result, true
				}
			}
		}

		return zeroVal, false
	}(); ok {
		return r
	}

	r := result()

	if cacheTime > 0 {
		cacheLock.Lock()
		defer cacheLock.Unlock()
		cache[k] = &cachedResult[T]{
			t:      time.Now(),
			result: r,
		}
	}

	return r
}
