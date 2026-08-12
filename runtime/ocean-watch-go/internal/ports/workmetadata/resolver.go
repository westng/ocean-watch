package workmetadata

import (
	"context"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

type Resolver interface {
	Resolve(context.Context, string) (domain.ResolvedWorkLink, error)
}

type MetadataResolver interface {
	ResolveMany(context.Context, []string, int) (MetadataResult, error)
}

type MetadataResult struct {
	Rows        map[string]MetadataRow
	Errors      map[string]MetadataError
	Performance map[string]any
}

type MetadataRow struct {
	AwemeItemID string
	CreatorName string
	AwemeID     string
	AwemeShowID string
	ProductID   string
	ProductName string
	Metadata    []byte
}

type MetadataError struct {
	Code    string
	Message string
}
