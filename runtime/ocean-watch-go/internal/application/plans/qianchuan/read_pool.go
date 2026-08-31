package qianchuan

import (
	"context"
	"errors"
	"sync"
)

type ReadPool struct {
	slots chan struct{}
	mu    sync.Mutex
	reads map[string]*sharedRead
}

type sharedRead struct {
	done  chan struct{}
	value any
	err   error
}

func NewReadPool(concurrency int) (*ReadPool, error) {
	if concurrency < 1 || concurrency > 10 {
		return nil, errors.New("Qianchuan read concurrency must be between 1 and 10")
	}
	return &ReadPool{
		slots: make(chan struct{}, concurrency),
		reads: make(map[string]*sharedRead),
	}, nil
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

func runReadOnce[T any](
	ctx context.Context,
	pool *ReadPool,
	key string,
	action func(context.Context) (T, error),
) (T, error) {
	var zero T
	if action == nil {
		return zero, errors.New("Qianchuan read action is required")
	}
	if pool == nil {
		return action(ctx)
	}
	if key == "" {
		return zero, errors.New("Qianchuan shared read key is required")
	}

	pool.mu.Lock()
	call, exists := pool.reads[key]
	if !exists {
		call = &sharedRead{done: make(chan struct{})}
		pool.reads[key] = call
	}
	pool.mu.Unlock()

	if exists {
		select {
		case <-call.done:
			if call.err != nil {
				return zero, call.err
			}
			value, ok := call.value.(T)
			if !ok {
				return zero, errors.New("Qianchuan shared read key was reused with a different result type")
			}
			return value, nil
		case <-ctx.Done():
			return zero, ctx.Err()
		}
	}

	value, err := action(ctx)
	pool.mu.Lock()
	call.value, call.err = value, err
	close(call.done)
	pool.mu.Unlock()
	return value, err
}

func (pool *ReadPool) forgetRead(key string) {
	if pool == nil || key == "" {
		return
	}
	pool.mu.Lock()
	delete(pool.reads, key)
	pool.mu.Unlock()
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
