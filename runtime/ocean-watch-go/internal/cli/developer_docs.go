package cli

import (
	"context"
	"flag"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/credentials"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/officialmcp"
	docs "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/developerdocs"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	"io"
)

func runDeveloperDocs(ctx context.Context, action string, args []string, stdout io.Writer, references docs.AppReferences) int {
	flags := flag.NewFlagSet("developer-docs "+action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	metadata := docs.Metadata{}
	if action == "configure" {
		flags.StringVar(&metadata.AppID, "app-id", "", "")
		flags.StringVar(&metadata.DeveloperID, "developer-id", "", "")
		flags.StringVar(&metadata.AuthorizationReference, "authorization-id", "", "")
	}
	if flags.Parse(args) != nil || len(flags.Args()) > 0 {
		WriteDomainError(stdout, domain.NewError("invalid_arguments", "invalid developer documentation arguments", 2, nil))
		return 2
	}
	client := officialmcp.NewDeveloperClient()
	settings := docs.Settings{Store: credentials.SystemStore{}, Backend: client, AppReferences: references}
	status := settings.Status(ctx)
	if action == "configure" {
		var err error
		status, err = settings.Configure(ctx, metadata)
		if err != nil {
			WriteDomainError(stdout, domain.NewError("developer_docs_configuration_failed", "developer metadata or official documentation connection could not be verified", 2, nil))
			return 2
		}
	}
	_ = WriteJSONDestination(stdout, status, "")
	return 0
}
