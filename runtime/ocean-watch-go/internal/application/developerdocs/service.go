// Package developerdocs is independent of business OAuth and account data.
package developerdocs

import (
	"context"
	"errors"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/policy"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/contracts"
	"strings"
)

const MetadataAccount = "oceanengine-developer-docs-metadata"

var ErrMetadata = errors.New("DEVELOPER_DOCS_CONFIGURATION_REQUIRED")
var ErrSchema = errors.New("DEVELOPER_DOCS_SCHEMA_REVIEW_REQUIRED")
var ErrConnection = errors.New("DEVELOPER_DOCS_CONNECTION_FAILED")

type Failure struct{ Stage string }

func (failure *Failure) Error() string { return "DEVELOPER_DOCS_QUERY_FAILED" }
func (failure *Failure) Unwrap() error { return ErrConnection }

type Metadata struct {
	AppID                  string `json:"-"`
	DeveloperID            string `json:"-"`
	AuthorizationReference string `json:"-"`
}
type AppReferences interface {
	AppID(context.Context, string) (string, error)
}
type Store interface {
	Read(context.Context, string) (map[string]any, error)
	Write(context.Context, string, map[string]any) (string, error)
	BackendName() string
}
type Backend interface {
	Validate(context.Context, Metadata) error
	Query(context.Context, Metadata, Request) (Result, error)
}
type Settings struct {
	Store         Store
	Backend       Backend
	AppReferences AppReferences
}
type Status struct {
	Configured     bool `json:"configured"`
	Protected      bool `json:"protected"`
	RemoteVerified bool `json:"remote_verified"`
}

func metadataID(value string) bool {
	if value == "" || len(value) > 19 || value[0] == '0' {
		return false
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(value) < 19 || value <= "9223372036854775807"
}
func protected(backend string) bool {
	return backend == "macos-keychain" || backend == "linux-secret-service" || backend == "windows-credential-manager"
}
func (settings Settings) Load(ctx context.Context) (Metadata, error) {
	if ctx == nil || ctx.Err() != nil || settings.Store == nil || !protected(settings.Store.BackendName()) {
		return Metadata{}, ErrMetadata
	}
	entry, err := settings.Store.Read(ctx, MetadataAccount)
	if err != nil {
		return Metadata{}, ErrMetadata
	}
	app, _ := entry["app_id"].(string)
	developer, _ := entry["developer_id"].(string)
	if !metadataID(app) || !metadataID(developer) {
		return Metadata{}, ErrMetadata
	}
	reference, _ := entry["authorization_reference"].(string)
	metadata := Metadata{AppID: app, DeveloperID: developer, AuthorizationReference: reference}
	if reference != "" {
		if settings.AppReferences == nil || !validAppReference(reference) {
			return Metadata{}, ErrMetadata
		}
		current, err := settings.AppReferences.AppID(ctx, reference)
		if err != nil || current != app {
			return Metadata{}, ErrMetadata
		}
	}
	return metadata, nil
}
func (settings Settings) Status(ctx context.Context) Status {
	_, err := settings.Load(ctx)
	return Status{Configured: err == nil, Protected: settings.Store != nil && protected(settings.Store.BackendName())}
}
func (settings Settings) Configure(ctx context.Context, metadata Metadata) (Status, error) {
	if ctx == nil || ctx.Err() != nil {
		return Status{}, ErrMetadata
	}
	if metadata.AuthorizationReference != "" {
		if settings.AppReferences == nil || !validAppReference(metadata.AuthorizationReference) {
			return Status{}, ErrMetadata
		}
		app, err := settings.AppReferences.AppID(ctx, metadata.AuthorizationReference)
		if err != nil || metadata.AppID != "" && metadata.AppID != app {
			return Status{}, ErrMetadata
		}
		metadata.AppID = app
	}
	if !metadataID(metadata.AppID) || !metadataID(metadata.DeveloperID) || settings.Store == nil || !protected(settings.Store.BackendName()) || settings.Backend == nil {
		return Status{}, ErrMetadata
	}
	if err := settings.Backend.Validate(ctx, metadata); err != nil {
		return Status{}, err
	}
	entry := map[string]any{"app_id": metadata.AppID, "developer_id": metadata.DeveloperID, "schema_version": 1}
	if metadata.AuthorizationReference != "" {
		entry["authorization_reference"] = metadata.AuthorizationReference
	}
	backend, err := settings.Store.Write(ctx, MetadataAccount, entry)
	if err != nil || !protected(backend) {
		return Status{}, ErrMetadata
	}
	saved, err := settings.Load(ctx)
	if err != nil || saved != metadata {
		return Status{}, ErrMetadata
	}
	return Status{Configured: true, Protected: true, RemoteVerified: true}, nil
}

func validAppReference(value string) bool {
	return value != "" && len(value) <= 256 && !strings.ContainsAny(value, "/\\?#\x00\r\n\t ")
}

type Request struct {
	Kind     string `json:"-"`
	Query    string `json:"query,omitempty"`
	APIPath  string `json:"api_path,omitempty"`
	Language string `json:"language,omitempty"`
}
type Result struct {
	Source    string   `json:"source"`
	Kind      string   `json:"kind"`
	Language  string   `json:"language,omitempty"`
	Snippets  []string `json:"snippets"`
	Truncated bool     `json:"truncated"`
}
type Service struct {
	Policy   policy.Instance
	Settings Settings
	Backend  Backend
}

func (service Service) Query(ctx context.Context, request Request) (Result, error) {
	if service.Policy.Check([]string{"developer_docs"}, contracts.QueryModeRead) != nil {
		return Result{}, policy.ErrDenied
	}
	if ctx == nil || len(request.Query) > 2048 || len(request.APIPath) > 256 {
		return Result{}, ErrSchema
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	switch request.Kind {
	case "search":
		if strings.TrimSpace(request.Query) == "" {
			return Result{}, ErrSchema
		}
	case "schema", "example":
		if !strings.HasPrefix(request.APIPath, "/open_api/") || strings.ContainsAny(request.APIPath, "?#%\\\r\n ") || strings.Contains(request.APIPath, "..") {
			return Result{}, ErrSchema
		}
		if request.Kind == "example" && request.Language != "go" && request.Language != "java" {
			return Result{}, ErrSchema
		}
	default:
		return Result{}, ErrSchema
	}
	metadata, err := service.Settings.Load(ctx)
	if err != nil {
		return Result{}, err
	}
	if service.Backend == nil {
		return Result{}, ErrConnection
	}
	return service.Backend.Query(ctx, metadata, request)
}
