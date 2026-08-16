package bootstrap

import (
	"context"
	"testing"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/oceanengine"
	authapplication "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	sharedplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans"
	applicationqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans/qianchuan"
	domainplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/plans"
	domainqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/qianchuan"
	portqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/qianchuan"
)

func TestNewQianchuanCommandServiceUsesSharedPorts(t *testing.T) {
	reader := bootstrapReader{}
	service, err := NewQianchuanCommandService(QianchuanOptions{
		Config: bootstrapConfig{}, StateRoot: t.TempDir(), CredentialStore: bootstrapCredentials{},
		Reader: reader, Writer: bootstrapWriter{}, Credentials: bootstrapPlanCredentials{},
		Locker: bootstrapLocker{}, Tokens: bootstrapTokens{}, OnlineRead: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if service.Config == nil || service.Tokens == nil || service.Links == nil || service.OwnerHints == nil ||
		service.Verifier.Reader == nil || service.Batch.Reader == nil || service.Batch.Writer == nil ||
		service.Batch.Reconciler == nil || service.Locks == nil || service.Journals == nil {
		t.Fatalf("shared Qianchuan service was not fully assembled: %#v", service)
	}
}

func TestNewQianchuanRuntimeSharesInjectedDependencies(t *testing.T) {
	reader := &bootstrapReader{}
	batchReader := &bootstrapReader{}
	credentials := bootstrapCredentials{}
	runtime, err := NewQianchuanRuntime(QianchuanOptions{
		Config: bootstrapConfig{}, StateRoot: t.TempDir(), CredentialStore: credentials,
		Reader: reader, BatchReader: batchReader, Writer: bootstrapWriter{}, Credentials: bootstrapPlanCredentials{},
		Locker: bootstrapLocker{}, Tokens: bootstrapTokens{}, OnlineRead: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Preflights.Tokens == nil || runtime.Reads.Tokens == nil || runtime.Reads.Reader == nil ||
		runtime.Reports.Tokens == nil || runtime.Reports.Reader == nil || runtime.Reports.UnifiedReader == nil ||
		runtime.Auth.Credentials == nil || runtime.Auth.Authorizations == nil {
		t.Fatalf("shared Qianchuan runtime was not fully assembled: %#v", runtime)
	}
	if runtime.Reads.Reader != reader {
		t.Fatal("ordinary Qianchuan reads did not retain the shared controlled reader")
	}
	if runtime.Preflights.Verifier.Reader != batchReader || runtime.Preflights.Batch.Reader != batchReader {
		t.Fatal("batch preflight did not use the dedicated concurrent reader")
	}
	if runtime.Preflights.Tokens != runtime.Reads.Tokens || runtime.Preflights.Tokens != runtime.Reports.Tokens {
		t.Fatal("Qianchuan services did not share one token manager")
	}
}

func TestNewQianchuanRuntimeSeparatesControlledAndBatchReaders(t *testing.T) {
	runtime, err := NewQianchuanRuntime(QianchuanOptions{
		Config: bootstrapConfig{}, StateRoot: t.TempDir(), CredentialStore: bootstrapCredentials{},
		BatchReadConcurrency: 3, OnlineRead: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	ordinary, ok := runtime.Reads.Reader.(oceanengine.QianchuanReadAdapter)
	if !ok {
		t.Fatalf("ordinary reader has unexpected type %T", runtime.Reads.Reader)
	}
	batch, ok := runtime.Preflights.Verifier.Reader.(oceanengine.QianchuanReadAdapter)
	if !ok {
		t.Fatalf("batch reader has unexpected type %T", runtime.Preflights.Verifier.Reader)
	}
	if ordinary.Factory == nil || batch.Factory == nil || ordinary.Factory == batch.Factory {
		t.Fatal("ordinary controlled reader and concurrent batch reader were not separated")
	}
	batchService, ok := runtime.Preflights.Batch.Reader.(oceanengine.QianchuanReadAdapter)
	if !ok || batchService.Factory != batch.Factory {
		t.Fatal("batch preflight components did not share the dedicated batch reader")
	}
}

type bootstrapConfig struct{}

func (bootstrapConfig) Read(context.Context) (map[string]any, error) { return map[string]any{}, nil }

type bootstrapCredentials struct{}

func (bootstrapCredentials) Read(context.Context, string) (map[string]any, error) {
	return map[string]any{}, nil
}
func (bootstrapCredentials) Write(context.Context, string, map[string]any) (string, error) {
	return "fixture", nil
}

type bootstrapTokens struct{}

func (bootstrapTokens) Ensure(context.Context, authapplication.TokenQuery) (authapplication.TokenLease, error) {
	return authapplication.TokenLease{}, nil
}

type bootstrapPlanCredentials struct{}

func (bootstrapPlanCredentials) AccessToken(context.Context, domainplans.Channel, string, string) (sharedplans.CredentialLease, error) {
	return sharedplans.CredentialLease{}, nil
}

type bootstrapLocker struct{}

func (bootstrapLocker) Acquire(context.Context, domainplans.WriteScope) (func() error, error) {
	return func() error { return nil }, nil
}

type bootstrapReader struct{}

func (bootstrapReader) FetchProducts(context.Context, portqianchuan.ProductPageRequest) (domainqianchuan.ProductPage, error) {
	return domainqianchuan.ProductPage{}, nil
}
func (bootstrapReader) FetchPlans(context.Context, portqianchuan.PlanPageRequest) (domainqianchuan.PlanPage, error) {
	return domainqianchuan.PlanPage{}, nil
}
func (bootstrapReader) FetchPlanDetail(context.Context, portqianchuan.PlanDetailRequest) (domainqianchuan.PlanDetail, error) {
	return domainqianchuan.PlanDetail{}, nil
}
func (bootstrapReader) FetchPlanMaterials(context.Context, portqianchuan.MaterialPageRequest) (domainqianchuan.MaterialPage, error) {
	return domainqianchuan.MaterialPage{}, nil
}
func (bootstrapReader) FetchAuthorizedCreators(context.Context, portqianchuan.AuthorizedCreatorPageRequest) (domainqianchuan.AuthorizedCreatorPage, error) {
	return domainqianchuan.AuthorizedCreatorPage{}, nil
}
func (bootstrapReader) FetchCreatorVideos(context.Context, portqianchuan.CreatorVideoPageRequest) (domainqianchuan.CreatorVideoPage, error) {
	return domainqianchuan.CreatorVideoPage{}, nil
}

type bootstrapWriter struct{}

func (bootstrapWriter) CreatePlan(context.Context, portqianchuan.CreatePlanRequest) (portqianchuan.WriteResult, error) {
	return portqianchuan.WriteResult{}, nil
}
func (bootstrapWriter) AddMaterials(context.Context, portqianchuan.MaterialWriteRequest) (portqianchuan.WriteResult, error) {
	return portqianchuan.WriteResult{}, nil
}
func (bootstrapWriter) DeleteMaterials(context.Context, portqianchuan.DeleteMaterialsRequest) (portqianchuan.WriteResult, error) {
	return portqianchuan.WriteResult{}, nil
}
func (bootstrapWriter) UpdatePlan(context.Context, portqianchuan.MutationRequest) (portqianchuan.WriteResult, error) {
	return portqianchuan.WriteResult{}, nil
}

var _ applicationqianchuan.CommandConfigReader = bootstrapConfig{}
