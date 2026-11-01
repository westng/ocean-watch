package developerdocs

import (
	"context"
	"errors"
	"testing"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/policy"
)

type metadataStore struct {
	reads, writes int
	entry         map[string]any
	backend       string
}

func (s *metadataStore) BackendName() string { return s.backend }
func (s *metadataStore) Read(context.Context, string) (map[string]any, error) {
	s.reads++
	return s.entry, nil
}
func (s *metadataStore) Write(_ context.Context, account string, value map[string]any) (string, error) {
	if account != MetadataAccount {
		return "", ErrMetadata
	}
	s.writes++
	s.entry = value
	return s.backend, nil
}

type docsBackend struct {
	validations, calls int
	err                error
}

func (b *docsBackend) Validate(context.Context, Metadata) error { b.validations++; return b.err }
func (b *docsBackend) Query(context.Context, Metadata, Request) (Result, error) {
	b.calls++
	return Result{Source: "official_developer_mcp", Snippets: []string{"fixture"}}, b.err
}

func TestDocsDisabledAndInvalidLanguageDoNotReadMetadata(t *testing.T) {
	store := &metadataStore{backend: "macos-keychain"}
	backend := &docsBackend{}
	service := Service{Policy: policy.Default(), Settings: Settings{Store: store}, Backend: backend}
	if _, err := service.Query(context.Background(), Request{Kind: "search", Query: "千川"}); !errors.Is(err, policy.ErrDenied) {
		t.Fatal("default enabled developer docs")
	}
	service.Policy, _ = policy.New(policy.Config{Mode: "read", Toolsets: []string{"core", "developer_docs"}})
	if _, err := service.Query(context.Background(), Request{Kind: "example", APIPath: "/open_api/v1.0/qianchuan/ad/get/", Language: "python"}); !errors.Is(err, ErrSchema) {
		t.Fatal("unverified SDK language was accepted")
	}
	if store.reads != 0 || backend.calls != 0 {
		t.Fatal("denied documentation touched protected state or network")
	}
}

func TestMetadataRequiresProtectionAndRemoteContractBeforeSaving(t *testing.T) {
	store := &metadataStore{backend: "macos-keychain"}
	backend := &docsBackend{err: ErrSchema}
	settings := Settings{Store: store, Backend: backend}
	if _, err := settings.Configure(context.Background(), Metadata{AppID: "100", DeveloperID: "200"}); err == nil || store.writes != 0 {
		t.Fatal("unverified contract was saved")
	}
	backend.err = nil
	status, err := settings.Configure(context.Background(), Metadata{AppID: "100", DeveloperID: "200"})
	if err != nil || !status.Configured || !status.Protected || !status.RemoteVerified || store.writes != 1 {
		t.Fatal("protected metadata preparation failed")
	}
	store.backend = "protected-file"
	if _, err := settings.Load(context.Background()); !errors.Is(err, ErrMetadata) {
		t.Fatal("file storage was accepted for developer metadata")
	}
}

type mutableAppReference struct {
	app   string
	reads int
}

func (reference *mutableAppReference) AppID(context.Context, string) (string, error) {
	reference.reads++
	return reference.app, nil
}

func TestReferencedAppChangesInvalidateDocumentationBinding(t *testing.T) {
	store := &metadataStore{backend: "macos-keychain"}
	backend := &docsBackend{}
	references := &mutableAppReference{app: "100"}
	settings := Settings{Store: store, Backend: backend, AppReferences: references}
	if _, err := settings.Configure(context.Background(), Metadata{DeveloperID: "200", AuthorizationReference: "synthetic-grant"}); err != nil || store.entry["app_id"] != "100" {
		t.Fatal("referenced App could not configure documentation")
	}
	references.app = "101"
	if _, err := settings.Load(context.Background()); !errors.Is(err, ErrMetadata) {
		t.Fatal("changed App reused the old documentation binding")
	}
	before := backend.validations
	if _, err := settings.Configure(context.Background(), Metadata{AppID: "100", DeveloperID: "200", AuthorizationReference: "synthetic-grant"}); !errors.Is(err, ErrMetadata) || backend.validations != before {
		t.Fatal("contradictory App reached remote validation")
	}
	if _, err := settings.Configure(context.Background(), Metadata{AppID: "300", DeveloperID: "200"}); err != nil {
		t.Fatal("independent documentation setup required business OAuth")
	}
	references.app = "102"
	if _, err := settings.Load(context.Background()); err != nil {
		t.Fatal("independent App was bound to an unrelated grant")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before = store.reads
	if _, err := settings.Load(ctx); !errors.Is(err, ErrMetadata) || store.reads != before {
		t.Fatal("cancelled metadata read accessed the vault")
	}
}
