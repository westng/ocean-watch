package bootstrap

import (
	"context"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/sqlite"
	applicationops "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/operations"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans/qianchuan"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/policy"
	"path/filepath"
)

// Ordinary local inspection never starts a broker. The caller owns the store.
func NewQianchuanOperations(ctx context.Context, options QianchuanOptions, action string) (qianchuan.OperationService, func() error, error) {
	instance := policy.Default()
	if options.Policy != nil {
		instance = *options.Policy
	}
	storeOptions := sqlite.Options{Path: filepath.Join(options.StateRoot, "ocean-watch.db"), RuntimeVersion: options.RuntimeVersion}
	var store *sqlite.Store
	var err error
	if action == "prepare" {
		store, err = sqlite.Initialize(ctx, storeOptions)
	} else {
		store, err = sqlite.Open(ctx, storeOptions)
	}
	if err != nil {
		return qianchuan.OperationService{}, nil, err
	}
	core := applicationops.Service{Repository: store, Snapshots: store, Now: options.Now}
	if action == "confirm" {
		core, err = NewOperations(store, store)
		core.Now = options.Now
		if err != nil {
			store.Close()
			return qianchuan.OperationService{}, nil, err
		}
	}
	options.OnlineRead = action == "prepare" || action == "execute" || action == "reconcile"
	options.Submit = action == "execute"
	commands, err := NewQianchuanCommandService(options)
	if err != nil {
		store.Close()
		return qianchuan.OperationService{}, nil, err
	}
	return qianchuan.OperationService{Commands: commands, Core: core, Recovery: store, Policy: instance, RuntimeVersion: options.RuntimeVersion}, store.Close, nil
}
