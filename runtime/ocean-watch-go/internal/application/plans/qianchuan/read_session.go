package qianchuan

import (
	"context"
	"errors"
	"strings"
	"sync"

	authapplication "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
)

const accountReadConcurrency = 2

// Keep excess same-account work outside the HTTP admission deadline. This
// applies to the single-template path as well as routed account sessions.
func newAccountReadPool(concurrency int) (*ReadPool, error) {
	pool, err := NewReadPool(concurrency)
	if err != nil {
		return nil, err
	}
	pool.accountSlots = make(chan struct{}, min(accountReadConcurrency, concurrency))
	return pool, nil
}

// A session owns one immutable lease and one fact cache per account/grant
// selection. Only read capacity is shared across authorization boundaries.
type batchReadSession struct {
	root     context.Context
	base     authapplication.TokenProvider
	capacity *ReadPool
	mu       sync.Mutex
	pools    map[string]*ReadPool
	accounts map[string]chan struct{}
}

func newBatchReadSession(ctx context.Context, concurrency int, tokens authapplication.TokenProvider) (*batchReadSession, error) {
	pool, err := NewReadPool(concurrency)
	if err != nil {
		return nil, err
	}
	return &batchReadSession{root: ctx, base: tokens, capacity: pool,
		pools: map[string]*ReadPool{}, accounts: map[string]chan struct{}{}}, nil
}

func (session *batchReadSession) pool(advertiserID, authorizationSelector string) *ReadPool {
	advertiserID, authorizationSelector = strings.TrimSpace(advertiserID), strings.TrimSpace(authorizationSelector)
	key := advertiserID + "\x00" + authorizationSelector
	session.mu.Lock()
	defer session.mu.Unlock()
	if pool, ok := session.pools[key]; ok {
		return pool
	}
	account, ok := session.accounts[advertiserID]
	if !ok {
		account = make(chan struct{}, min(accountReadConcurrency, cap(session.capacity.slots)))
		session.accounts[advertiserID] = account
	}
	pool := &ReadPool{slots: session.capacity.slots, accountSlots: account,
		sharedRoot: session.root, reads: map[string]*sharedRead{}}
	session.pools[key] = pool
	return pool
}

func (session *batchReadSession) Ensure(ctx context.Context, query authapplication.TokenQuery) (authapplication.TokenLease, error) {
	if session.base == nil || query.Channel != "qianchuan" {
		return authapplication.TokenLease{}, errors.New("Qianchuan read token provider is required")
	}
	pool := session.pool(query.AdvertiserID, query.AuthAccountID)
	return runReadOnce(ctx, pool, "credential-lease", func(ctx context.Context) (authapplication.TokenLease, error) {
		return runRead(ctx, pool, func(ctx context.Context) (authapplication.TokenLease, error) {
			return session.base.Ensure(ctx, query)
		})
	})
}
