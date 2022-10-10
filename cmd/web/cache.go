package main

import (
	"sync"
	"time"
)

var cache = make(map[string]*cachedResult)
var cacheLock sync.RWMutex

type cachedResult struct {
	t      time.Time
	result any
}

func cacheResult(k string, cacheTime time.Duration, result func() any) any {
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
