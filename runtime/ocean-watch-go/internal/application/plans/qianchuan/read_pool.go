package qianchuan

import (
	"context"
	"errors"
	"sync"
)

type ReadPool struct {
	slots chan struct{}
}

func NewReadPool(concurrency int) (*ReadPool, error) {
	if concurrency < 1 || concurrency > 10 {
		return nil, errors.New("Qianchuan read concurrency must be between 1 and 10")
	}
	return &ReadPool{slots: make(chan struct{}, concurrency)}, nil
}

func runRead[T any](ctx context.Context, pool *ReadPool, action func(context.Context) (T, error)) (T, error) {
	var zero T
	if action == nil {
		return zero, errors.New("Qianchuan read action is required")
	}
	if pool == nil {
		return action(ctx)
	}
	select {
	case pool.slots <- struct{}{}:
		defer func() { <-pool.slots }()
	case <-ctx.Done():
		return zero, ctx.Err()
	}
	return action(ctx)
}

func parallelOrdered[T any](ctx context.Context, pool *ReadPool, count int, action func(context.Context, int) T) []T {
	results := make([]T, count)
	if count == 0 {
		return results
	}
	if pool == nil {
		for index := range count {
			results[index] = action(ctx, index)
		}
		return results
	}
	var waitGroup sync.WaitGroup
	for index := range count {
		index := index
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			results[index] = action(ctx, index)
		}()
	}
	waitGroup.Wait()
	return results
}
