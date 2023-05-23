package utils

import (
	"context"
	"time"
)

func ContextTick(ctx context.Context, d time.Duration) <-chan time.Time {
	ticker := time.Tick(d)
	c := make(chan time.Time, 1)
	go func() {
		defer close(c)
		for {
			select {
			case <-ctx.Done():
				return
			case tick := <-ticker:
				c <- tick
			}
		}
	}()
	return c
}
