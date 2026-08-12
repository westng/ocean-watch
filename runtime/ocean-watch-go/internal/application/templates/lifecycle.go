package templates

import (
	"context"

	domaintemplates "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/templates"
)

type Lifecycle struct {
	Store ConfigStore
	Path  string
}

func (lifecycle Lifecycle) Validate(ctx context.Context, channel, selector string) (map[string]any, error) {
	config, err := lifecycle.Store.Read(ctx)
	if err != nil {
		return nil, err
	}
	result, err := domaintemplates.Validate(config, channel, selector)
	if err != nil {
		return nil, err
	}
	result["config"] = lifecycle.Path
	return result, nil
}

func (lifecycle Lifecycle) Delete(
	ctx context.Context,
	channel string,
	selector string,
	force bool,
	submit bool,
) (map[string]any, error) {
	config, revision, err := lifecycle.Store.ReadWithRevision(ctx)
	if err != nil {
		return nil, err
	}
	updated, deletion, err := domaintemplates.Delete(config, channel, selector, force)
	if err != nil {
		return nil, err
	}
	if submit {
		if err := lifecycle.Store.CompareAndSwap(ctx, revision, updated); err != nil {
			return nil, err
		}
	}
	mode := "dry_run"
	if submit {
		mode = "submit"
	}
	return map[string]any{
		"ok":        true,
		"mode":      mode,
		"operation": "template_delete",
		"config":    lifecycle.Path,
		"changed":   submit,
		"deletion":  deletion,
	}, nil
}

func (lifecycle Lifecycle) SetCopy(
	ctx context.Context,
	templateName string,
	titles []string,
	fromTemplate string,
) (map[string]any, error) {
	config, revision, err := lifecycle.Store.ReadWithRevision(ctx)
	if err != nil {
		return nil, err
	}
	updated, err := domaintemplates.SetCopy(config, templateName, titles, fromTemplate)
	if err != nil {
		return nil, err
	}
	if err := lifecycle.Store.CompareAndSwap(ctx, revision, updated); err != nil {
		return nil, err
	}
	return domaintemplates.MarketingLifecycleResult(updated, lifecycle.Path, "set-copy", true)
}

func (lifecycle Lifecycle) MigrateMarketing(
	ctx context.Context,
	confirmRemoveLegacyMaterials bool,
) (map[string]any, error) {
	config, revision, err := lifecycle.Store.ReadWithRevision(ctx)
	if err != nil {
		return nil, err
	}
	updated, legacyError, err := domaintemplates.MigrateMarketing(config, confirmRemoveLegacyMaterials)
	if err != nil {
		return nil, err
	}
	if legacyError != nil {
		return nil, legacyError
	}
	if err := lifecycle.Store.CompareAndSwap(ctx, revision, updated); err != nil {
		return nil, err
	}
	return domaintemplates.MarketingLifecycleResult(updated, lifecycle.Path, "migrate", true)
}

func (lifecycle Lifecycle) ListQianchuanProduct(ctx context.Context) (map[string]any, error) {
	config, err := lifecycle.Store.Read(ctx)
	if err != nil {
		return nil, err
	}
	return domaintemplates.ListQianchuanProduct(config)
}

func (lifecycle Lifecycle) MigrateQianchuanProduct(ctx context.Context) (map[string]any, error) {
	config, revision, err := lifecycle.Store.ReadWithRevision(ctx)
	if err != nil {
		return nil, err
	}
	updated, result, changed, err := domaintemplates.MigrateQianchuanProduct(config)
	if err != nil {
		return nil, err
	}
	if changed {
		if err := lifecycle.Store.CompareAndSwap(ctx, revision, updated); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (lifecycle Lifecycle) ListQianchuanLive(ctx context.Context) (map[string]any, error) {
	config, err := lifecycle.Store.Read(ctx)
	if err != nil {
		return nil, err
	}
	result, err := domaintemplates.ListQianchuanLive(config)
	if err != nil {
		return nil, err
	}
	result["config"] = lifecycle.Path
	return result, nil
}

func (lifecycle Lifecycle) MigrateQianchuanLive(ctx context.Context) (map[string]any, error) {
	config, revision, err := lifecycle.Store.ReadWithRevision(ctx)
	if err != nil {
		return nil, err
	}
	updated, result, changed, err := domaintemplates.MigrateQianchuanLive(config)
	if err != nil {
		return nil, err
	}
	if changed {
		if err := lifecycle.Store.CompareAndSwap(ctx, revision, updated); err != nil {
			return nil, err
		}
	}
	result["config"] = lifecycle.Path
	return result, nil
}
