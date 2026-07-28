package application

import (
	"context"
	"errors"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
)

type ConfigUpdater = func(map[string]any) (result any, changed bool, err error)

type WorkMetadataStore interface {
	Read(context.Context) (map[string]any, error)
	Update(context.Context, ConfigUpdater) (any, error)
}

type WorkMetadataStatus struct {
	OK         bool    `json:"ok"`
	Mode       string  `json:"mode"`
	Config     string  `json:"config"`
	Configured bool    `json:"configured"`
	Endpoint   *string `json:"endpoint"`
}

type WorkMetadata struct {
	Store         WorkMetadataStore
	Path          string
	RequestedPath string
}

func (service WorkMetadata) Status(ctx context.Context) (WorkMetadataStatus, error) {
	config, err := service.Store.Read(ctx)
	if err != nil {
		return WorkMetadataStatus{}, err
	}
	return workMetadataStatus(config, service.Path)
}

func (service WorkMetadata) Set(ctx context.Context, endpoint string) (WorkMetadataStatus, error) {
	validated, err := domain.ValidateWorkMetadataEndpoint(endpoint)
	if err != nil {
		return WorkMetadataStatus{}, err
	}
	if validated == "" {
		return WorkMetadataStatus{}, errors.New("--endpoint cannot be empty")
	}
	result, err := service.Store.Update(ctx, func(config map[string]any) (any, bool, error) {
		if err := domain.SetWorkMetadataEndpoint(config, validated); err != nil {
			return nil, false, err
		}
		status, err := workMetadataStatus(config, service.Path)
		return status, true, err
	})
	if err != nil {
		return WorkMetadataStatus{}, err
	}
	return result.(WorkMetadataStatus), nil
}

func (service WorkMetadata) Clear(ctx context.Context) (WorkMetadataStatus, error) {
	result, err := service.Store.Update(ctx, func(config map[string]any) (any, bool, error) {
		domain.ClearWorkMetadataEndpoint(config)
		status, err := workMetadataStatus(config, service.Path)
		return status, true, err
	})
	if err != nil {
		return WorkMetadataStatus{}, err
	}
	return result.(WorkMetadataStatus), nil
}

func workMetadataStatus(config map[string]any, path string) (WorkMetadataStatus, error) {
	endpoint, err := domain.WorkMetadataEndpoint(config)
	if err != nil {
		return WorkMetadataStatus{}, err
	}
	status := WorkMetadataStatus{
		OK: true, Mode: "qianchuan_work_metadata_status", Config: path,
		Configured: endpoint != "",
	}
	if endpoint != "" {
		redacted := "<configured locally>"
		status.Endpoint = &redacted
	}
	return status, nil
}
