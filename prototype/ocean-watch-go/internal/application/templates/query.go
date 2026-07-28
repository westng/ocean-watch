package templates

import (
	"context"

	domaintemplates "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/templates"
)

type ConfigReader interface {
	Read(context.Context) (map[string]any, error)
}

type ConfigStore interface {
	ConfigReader
	ReadWithRevision(context.Context) (map[string]any, string, error)
	CompareAndSwap(context.Context, string, map[string]any) error
}

type Query struct {
	Store ConfigReader
	Path  string
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
	result["config"] = query.Path
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
	result["config"] = query.Path
	return result, nil
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
