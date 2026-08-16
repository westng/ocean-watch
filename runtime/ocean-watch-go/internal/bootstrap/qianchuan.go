package bootstrap

import (
	"net/http"
	"path/filepath"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/filesystem"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/oceanengine"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/python"
	adapterworkmetadata "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/workmetadata"
	authapplication "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	sharedplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans"
	applicationqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans/qianchuan"
	applicationqianchuanread "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/qianchuan"
	applicationreports "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/reports"
	applicationtemplates "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/templates"
	applicationworkmetadata "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/workmetadata"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/requestcontrol"
	platformretry "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/retry"
	portqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/qianchuan"
	portreports "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/reports"
)

type QianchuanOptions struct {
	Config               applicationqianchuan.CommandConfigReader
	StateRoot            string
	CredentialStore      authapplication.CredentialStore
	Links                applicationqianchuan.WorkLinkResolver
	OwnerHints           applicationqianchuan.OwnerHintCache
	Authorizations       authapplication.AuthorizationStore
	RefreshLocker        authapplication.RefreshLocker
	OAuth                authapplication.OAuthAdapter
	ClientFactory        *oceanengine.ClientFactory
	HTTPClient           *http.Client
	PythonResolver       python.Resolver
	PluginRoot           string
	Reader               portqianchuan.Reader
	BatchReader          portqianchuan.Reader
	Writer               portqianchuan.Writer
	Credentials          sharedplans.CredentialProvider
	Locker               sharedplans.AdvertiserLocker
	Tokens               authapplication.TokenProvider
	Retry                platformretry.Policy
	Now                  func() time.Time
	Submit               bool
	OnlineRead           bool
	BatchReadConcurrency int
}

type QianchuanRuntime struct {
	Preflights applicationqianchuan.CommandService
	Reads      applicationqianchuanread.Service
	Reports    applicationreports.Service
	Auth       authapplication.QueryService
}

func NewQianchuanRuntime(options QianchuanOptions) (QianchuanRuntime, error) {
	factory := options.ClientFactory
	var err error
	if factory == nil {
		factory, err = oceanengine.NewClientFactory(oceanengine.FactoryOptions{
			SharedQianchuanControl: filesystem.QianchuanRequestController{Root: options.StateRoot},
		})
		if err != nil {
			return QianchuanRuntime{}, err
		}
	}
	authorizations := options.Authorizations
	if authorizations == nil {
		authorizations = filesystem.AuthorizationStore{Root: options.StateRoot}
	}
	refreshLocker := options.RefreshLocker
	if refreshLocker == nil {
		refreshLocker = filesystem.RefreshLocker{StateRoot: options.StateRoot}
	}
	oauth := options.OAuth
	if oauth == nil {
		oauth = oceanengine.OAuthAdapter{Factory: factory}
	}
	tokens := options.Tokens
	if tokens == nil {
		tokens = &authapplication.TokenManager{
			Credentials: options.CredentialStore, Authorizations: authorizations,
			Locks: refreshLocker, OAuth: oauth, Now: options.Now,
		}
	}
	reader := options.Reader
	if reader == nil {
		reader = oceanengine.QianchuanReadAdapter{Factory: factory, Retry: options.Retry}
	}
	batchReader := options.BatchReader
	if batchReader == nil && options.Reader != nil {
		batchReader = options.Reader
	}
	if batchReader == nil {
		readFactory := factory
		if options.BatchReadConcurrency > 0 {
			readFactory, err = newQianchuanBatchReadFactory(options.BatchReadConcurrency)
			if err != nil {
				return QianchuanRuntime{}, err
			}
		}
		batchReader = oceanengine.QianchuanReadAdapter{Factory: readFactory, Retry: options.Retry}
	}
	reportReader := oceanengine.QianchuanReportAdapter{Factory: factory, Retry: options.Retry}
	commandOptions := options
	commandOptions.ClientFactory = factory
	commandOptions.Authorizations = authorizations
	commandOptions.RefreshLocker = refreshLocker
	commandOptions.OAuth = oauth
	commandOptions.Tokens = tokens
	commandOptions.Reader = batchReader
	commandOptions.OnlineRead = true
	preflights, err := NewQianchuanCommandService(commandOptions)
	if err != nil {
		return QianchuanRuntime{}, err
	}
	templateQuery := applicationtemplates.Query{Store: options.Config}
	var reportsReader portreports.QianchuanReader = reportReader
	var unifiedReader portreports.QianchuanUnifiedReader = reportReader
	return QianchuanRuntime{
		Preflights: preflights,
		Reads: applicationqianchuanread.Service{
			Tokens: tokens, Reader: reader, Templates: templateQuery, Now: options.Now,
		},
		Reports: applicationreports.Service{
			Tokens: tokens, Reader: reportsReader, UnifiedReader: unifiedReader, Now: options.Now,
		},
		Auth: authapplication.QueryService{
			Credentials: options.CredentialStore, Authorizations: authorizations,
		},
	}, nil
}

func newQianchuanBatchReadFactory(concurrency int) (*oceanengine.ClientFactory, error) {
	return oceanengine.NewClientFactory(oceanengine.FactoryOptions{
		RequestLimits: requestcontrol.Limits{
			AuthorizationConcurrency:          concurrency,
			EndpointConcurrency:               concurrency,
			QianchuanAuthorizationConcurrency: concurrency,
		},
	})
}

func NewQianchuanCommandService(options QianchuanOptions) (applicationqianchuan.CommandService, error) {
	service := applicationqianchuan.CommandService{
		Config: options.Config, Links: options.Links,
		Journals: filesystem.OperationJournalStore{Root: options.StateRoot}, Now: options.Now,
	}
	if !options.OnlineRead && !options.Submit {
		return service, nil
	}
	factory := options.ClientFactory
	readFactory := factory
	var err error
	if factory == nil && (options.Reader == nil || options.Writer == nil) {
		factory, err = oceanengine.NewClientFactory(oceanengine.FactoryOptions{
			SharedQianchuanControl: filesystem.QianchuanRequestController{Root: options.StateRoot},
		})
		if err != nil {
			return applicationqianchuan.CommandService{}, err
		}
		readFactory = factory
		if options.BatchReadConcurrency > 0 {
			readFactory, err = newQianchuanBatchReadFactory(options.BatchReadConcurrency)
			if err != nil {
				return applicationqianchuan.CommandService{}, err
			}
		}
	}
	authorizations := options.Authorizations
	if authorizations == nil {
		authorizations = filesystem.AuthorizationStore{Root: options.StateRoot}
	}
	refreshLocker := options.RefreshLocker
	if refreshLocker == nil {
		refreshLocker = filesystem.RefreshLocker{StateRoot: options.StateRoot}
	}
	oauth := options.OAuth
	if oauth == nil {
		oauth = oceanengine.OAuthAdapter{Factory: factory}
	}
	tokens := options.Tokens
	if tokens == nil {
		tokens = &authapplication.TokenManager{
			Credentials: options.CredentialStore, Authorizations: authorizations,
			Locks: refreshLocker, OAuth: oauth, Now: options.Now,
		}
	}
	credentials := options.Credentials
	if credentials == nil {
		credentials = sharedplans.TokenCredentialProvider{Tokens: tokens}
	}
	locker := options.Locker
	if locker == nil {
		locker = filesystem.AdvertiserLockStore{Root: options.StateRoot}
	}
	reader := options.Reader
	if reader == nil {
		reader = oceanengine.QianchuanReadAdapter{Factory: readFactory, Retry: options.Retry}
	}
	writer := options.Writer
	if writer == nil {
		writer = oceanengine.QianchuanWriteAdapter{Factory: factory}
	}
	service.Tokens = tokens
	if service.Links == nil {
		service.Links = applicationworkmetadata.Resolver{
			Links: adapterworkmetadata.DouyinRedirectResolver{Client: options.HTTPClient},
			Metadata: adapterworkmetadata.F2Resolver{
				Python: options.PythonResolver, Directory: options.PluginRoot,
			},
		}
	}
	service.OwnerHints = options.OwnerHints
	if service.OwnerHints == nil {
		service.OwnerHints = filesystem.QianchuanOwnerHintCache{
			Path: filepath.Join(options.StateRoot, "cache", "qianchuan-work-owners.json"), Now: options.Now,
		}
	}
	service.Verifier = applicationqianchuan.WorkVerifier{Reader: reader}
	service.Locks = locker
	reconciler := applicationqianchuan.CurrentDayReconciler{Reader: reader, Now: options.Now}
	guard := sharedplans.GuardedExecutor{Credentials: credentials, Locks: locker, Now: options.Now}
	service.Create = applicationqianchuan.CreateExecutor{Guard: guard, Writer: writer, Reconciler: reconciler}
	service.Batch = applicationqianchuan.BatchService{
		Guard: guard, Reader: reader, Writer: writer, Reconciler: reconciler, Now: options.Now,
	}
	service.Remove = applicationqianchuan.RemoveExecutor{Guard: guard, Reader: reader, Writer: writer}
	return service, nil
}
