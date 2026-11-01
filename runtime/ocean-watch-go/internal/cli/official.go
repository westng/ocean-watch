package cli

import (
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"

	auth "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	official "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/official"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/bootstrap"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/contracts"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	port "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/official"
)

type OfficialRuntime struct{ Service *official.Service }

func (runner Runner) runOfficial(ctx context.Context, action string, args []string, root string, credentials auth.CredentialStore, stdin io.Reader, stdout io.Writer) int {
	flags := flag.NewFlagSet("qc-official "+action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var query official.CatalogQuery
	var inputFile, out string
	flags.StringVar(&out, "out", "", "")
	if action == "catalog" {
		flags.StringVar(&query.QueryID, "query-id", "", "")
		flags.StringVar(&query.Group, "group", "", "")
		flags.StringVar(&query.Keyword, "keyword", "", "")
		flags.IntVar(&query.Limit, "limit", 20, "")
		flags.StringVar(&query.Cursor, "cursor", "", "")
	} else {
		flags.StringVar(&inputFile, "input-json", "-", "")
	}
	if flags.Parse(args) != nil || len(flags.Args()) > 0 {
		WriteDomainError(stdout, domain.NewError("invalid_arguments", "invalid official query arguments", 2, nil))
		return 2
	}
	service := runner.Official.Service
	if service == nil {
		if action == "catalog" {
			service = &official.Service{Policy: *runner.Policy}
		} else {
			runtime, err := bootstrap.NewQianchuanRuntime(bootstrap.QianchuanOptions{Policy: runner.Policy, StateRoot: filepath.Join(root, "ads-plan-monitor", "state"), CredentialStore: credentials})
			if err != nil {
				return officialCLIError(stdout, err)
			}
			defer runtime.Close()
			service = &runtime.Official
		}
	}
	var result any
	var err error
	if action == "catalog" {
		result, err = service.Catalog(ctx, query)
	} else {
		input := stdin
		if inputFile != "-" {
			file, openErr := os.Open(inputFile)
			if openErr != nil {
				WriteDomainError(stdout, domain.NewError("invalid_arguments", "input JSON could not be opened", 2, nil))
				return 2
			}
			defer file.Close()
			input = file
		}
		request, decodeErr := contracts.DecodeOfficialQueryRequest(input)
		if decodeErr != nil {
			WriteDomainError(stdout, domain.NewError("invalid_arguments", "invalid official query envelope", 2, nil))
			return 2
		}
		result, err = service.Query(ctx, request)
	}
	if err != nil {
		return officialCLIError(stdout, err)
	}
	if WriteJSONDestination(stdout, result, out) != nil {
		WriteDomainError(stdout, domain.NewError("configuration_error", "failed to write official query result", 2, nil))
		return 2
	}
	return 0
}

func officialCLIError(stdout io.Writer, err error) int {
	code := "official_query_failed"
	details := map[string]any{}
	var controlled *port.Failure
	if errors.As(err, &controlled) {
		code = controlled.Code
		details["stage"] = controlled.Stage
		details["dispatched"] = controlled.Dispatched
		if controlled.APICode != "" {
			details["api_code"] = controlled.APICode
		}
		if len(controlled.RequiredFields) > 0 {
			details["required_fields"] = controlled.RequiredFields
		}
		if controlled.AuthorizationID != "" {
			details["authorization_id"] = controlled.AuthorizationID
			details["channel"] = "qianchuan"
		}
		if controlled.AdvertiserID != "" {
			details["advertiser_id"] = controlled.AdvertiserID
		}
	}
	if errors.Is(err, context.Canceled) {
		code = "CANCELLED"
	}
	WriteDomainError(stdout, domain.NewError(code, code, 2, details))
	return 2
}
