package templates

import (
	"context"
	"errors"

	domaintemplates "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/templates"
)

type ConfigReader interface {
	Read(context.Context) (map[string]any, error)
}

type VersionedConfigReader interface {
	ReadWithRevision(context.Context) (map[string]any, string, error)
}

type ConfigStore interface {
	ConfigReader
	ReadWithRevision(context.Context) (map[string]any, string, error)
	CompareAndSwap(context.Context, string, map[string]any) error
}

type Query struct {
	Store          ConfigReader
	VersionedStore VersionedConfigReader
}

type VersionedResult struct {
	Value        map[string]any
	StateVersion string
}

func (query Query) List(ctx context.Context, channel string, includeDetails bool) (map[string]any, error) {
	config, err := query.Store.Read(ctx)
	if err != nil {
		return nil, err
	}
	result, err := domaintemplates.List(config, channel, includeDetails)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (query Query) Show(ctx context.Context, channel, selector string) (map[string]any, error) {
	config, err := query.Store.Read(ctx)
	if err != nil {
		return nil, err
	}
	result, err := domaintemplates.Show(config, channel, selector)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (query Query) ListVersioned(ctx context.Context, channel string, includeDetails bool) (VersionedResult, error) {
	config, revision, err := query.readVersioned(ctx)
	if err != nil {
		return VersionedResult{}, err
	}
	result, err := domaintemplates.List(config, channel, includeDetails)
	if err != nil {
		return VersionedResult{}, err
	}
	return VersionedResult{Value: result, StateVersion: revision}, nil
}

func (query Query) ShowExactVersioned(ctx context.Context, channel, templateID string) (VersionedResult, error) {
	config, revision, err := query.readVersioned(ctx)
	if err != nil {
		return VersionedResult{}, err
	}
	result, err := domaintemplates.ShowExact(config, channel, templateID)
	if err != nil {
		return VersionedResult{}, err
	}
	return VersionedResult{Value: result, StateVersion: revision}, nil
}

func (query Query) readVersioned(ctx context.Context) (map[string]any, string, error) {
	store := query.VersionedStore
	if store == nil {
		if candidate, ok := query.Store.(VersionedConfigReader); ok {
			store = candidate
		}
	}
	if store == nil {
		return nil, "", errors.New("versioned template configuration reader is unavailable")
	}
	return store.ReadWithRevision(ctx)
}

func (query Query) ResolveQianchuanProductBinding(
	ctx context.Context,
	selector string,
) (domaintemplates.QianchuanProductBinding, error) {
	config, err := query.Store.Read(ctx)
	if err != nil {
		return domaintemplates.QianchuanProductBinding{}, err
	}
	return domaintemplates.ResolveQianchuanProductBinding(config, selector)
}
