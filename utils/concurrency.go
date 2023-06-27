package utils

import (
	"runtime"
	"sync"
	"sync/atomic"
)

func SplitWork(routines int, workSize uint64, do func(workIndex uint64, routineIndex int) error, init func(routines, routineIndex int) error, errorFunc func(routineIndex int, err error)) bool {
	if routines <= 0 {
		routines = max(runtime.NumCPU()-routines, 4)
	}

	if workSize < uint64(routines) {
		routines = int(workSize)
	}

	var counter atomic.Uint64

	var wg sync.WaitGroup
	var failed atomic.Bool
	for routineIndex := 0; routineIndex < routines; routineIndex++ {
		if init != nil {
			if err := init(routines, routineIndex); err != nil {
				if errorFunc != nil {
					errorFunc(routineIndex, err)
				}
				failed.Store(true)
				continue
			}
		}
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
					if errorFunc != nil {
						errorFunc(routineIndex, err)
						failed.Store(true)
					}
					return
				}
			}
		}(routineIndex)
	}
	wg.Wait()

	return !failed.Load()
}
