package qianchuan

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestReadPoolSharesOneBoundedInFlightLimit(t *testing.T) {
	pool, err := NewReadPool(3)
	if err != nil {
		t.Fatal(err)
	}
	var active atomic.Int32
	var maximum atomic.Int32
	action := func(ctx context.Context, _ int) int {
		value, runErr := runRead(ctx, pool, func(context.Context) (int, error) {
			current := active.Add(1)
			for {
				observed := maximum.Load()
				if current <= observed || maximum.CompareAndSwap(observed, current) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			active.Add(-1)
			return 1, nil
		})
		if runErr != nil {
			return 0
		}
		return value
	}
	var waitGroup sync.WaitGroup
	for range 2 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			parallelOrdered(context.Background(), pool, 8, action)
		}()
	}
	waitGroup.Wait()
	if got := maximum.Load(); got != 3 {
		t.Fatalf("shared read pool maximum in-flight=%d want=3", got)
	}
}
