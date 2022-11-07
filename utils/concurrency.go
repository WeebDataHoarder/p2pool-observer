package utils

import (
	"runtime"
	"sync"
	"sync/atomic"
)

func SplitWork(routines int, workSize uint64, do func(workIndex uint64, routineIndex int) error) []error {
	if routines < 0 {
		routines = Max(runtime.NumCPU()-routines, 4)
	}

	if workSize < uint64(routines) {
		routines = int(workSize)
	}

	var counter atomic.Uint64

	results := make([]error, routines)

	var wg sync.WaitGroup
	for routineIndex := 0; routineIndex < routines; routineIndex++ {
		wg.Add(1)
		go func(routineIndex int) {
			defer wg.Done()
			var err error
			for {
				workIndex := counter.Add(1)
				if workIndex > workSize {
					return
				}

				if err = do(workIndex-1, routineIndex); err != nil {
					results[routineIndex] = err
					return
				}
			}
		}(routineIndex)
	}
	wg.Wait()

	return results
}
