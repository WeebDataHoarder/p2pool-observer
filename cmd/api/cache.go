package main

import (
	"sync"
	"time"
)

const (
	CacheTotalKnownBlocksAndMiners = iota
	totalSize
)

var cache = make([]*cachedResult, totalSize)
var cacheLock sync.RWMutex

type cachedResult struct {
	t      time.Time
	result any
}

func cacheResult(k uint, cacheTime time.Duration, result func() any) any {
	if k >= totalSize {
		return result()
	}
	if r := func() any {
		if cacheTime > 0 {
			cacheLock.RLock()
			defer cacheLock.RUnlock()

			if r := cache[k]; r != nil && r.t.Add(cacheTime).After(time.Now()) {
				return r.result
			}
		}

		return nil
	}(); r != nil {
		return r
	}

	r := result()

	if cacheTime > 0 && r != nil {
		cacheLock.Lock()
		defer cacheLock.Unlock()
		cache[k] = &cachedResult{
			t:      time.Now(),
			result: r,
		}
	}

	return r
}
