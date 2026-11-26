package cli

import (
	"context"
	"io"
	"os"
	"strings"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/policy"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/templates"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/contracts"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

func runStructuredTemplateCreate(ctx context.Context, options templateOptions, store templates.ConfigStore, authorizations authorizationReader, instance *policy.Instance, stdin io.Reader, stdout io.Writer) int {
	var reader io.Reader = strings.NewReader(options.inputJSON)
	if options.inputFile == "-" {
		reader = stdin
	} else if options.inputFile != "" {
		file, err := os.Open(options.inputFile)
		if err != nil {
			return writeTemplateCreateError(stdout, err)
		}
		defer file.Close()
		reader = file
	}
	payload, err := io.ReadAll(io.LimitReader(reader, contracts.MaxOfficialQueryRequestBytes+1))
	var input templates.CreateRequest
	if err != nil || len(strings.TrimSpace(string(payload))) == 0 || strings.TrimSpace(string(payload))[0] != '{' || contracts.DecodeStrictJSON(payload, &input) != nil {
		WriteDomainError(stdout, domain.NewError("invalid_arguments", "template input must be one bounded JSON object with supported fields", 2, nil))
		return 2
	}
	result, err := (templates.StructuredCreator{Store: store, Authorizations: authorizations, Policy: instance}).Create(ctx, input, templates.CreateOptions{Submit: options.submit, ExpectedRevision: options.expectedRevision, PreviewDigest: options.previewDigest})
	if err != nil {
		return writeTemplateCreateError(stdout, err)
	}
	if err = WriteJSON(stdout, result); err != nil {
		return 1
	}
	return 0
}
