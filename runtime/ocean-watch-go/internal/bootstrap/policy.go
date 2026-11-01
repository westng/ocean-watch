package bootstrap

import (
	"context"
	"errors"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/policy"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/contracts"
	"os"
)

// StartupPolicyEnv is passed only to the stable proxy's own child process.
// Ordinary tool arguments never affect this fixed configuration snapshot.
const StartupPolicyEnv = "OCEAN_WATCH_STARTUP_BUSINESS_POLICY"

type PolicyConfigReader interface {
	Read(context.Context) (map[string]any, error)
}

func StartupPolicy(ctx context.Context, store PolicyConfigReader, override string) (policy.Instance, error) {
	if override != "" {
		var options policy.Config
		if contracts.DecodeStrictJSON([]byte(override), &options) != nil {
			return policy.Instance{}, policy.ErrConfiguration
		}
		return policy.New(options)
	}
	if store == nil {
		return policy.Default(), nil
	}
	config, err := store.Read(ctx)
	if errors.Is(err, os.ErrNotExist) {
		return policy.Default(), nil
	}
	if err != nil {
		return policy.Instance{}, err
	}
	return policy.FromConfig(config)
}
