package cli

import (
	"context"
	"errors"
	"flag"
	"io"
	"path/filepath"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/filesystem"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/sqlite"
	auth "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans/qianchuan"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/bootstrap"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

func (runner Runner) runOperations(ctx context.Context, action string, args []string, root string, credentials auth.CredentialStore, stdout io.Writer) int {
	if action == "upgrade-state" {
		return runner.upgradeOperationState(ctx, args, root, stdout)
	}
	flags := flag.NewFlagSet("operations "+action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var ref qianchuan.OperationReference
	var preflight, approvalID string
	var submit bool
	if action == "prepare" {
		flags.StringVar(&preflight, "preflight-id", "", "")
	} else {
		flags.StringVar(&ref.OperationID, "operation-id", "", "")
		flags.IntVar(&ref.Version, "version", 0, "")
		flags.StringVar(&ref.SnapshotDigest, "snapshot-digest", "", "")
	}
	if action == "execute" {
		flags.StringVar(&approvalID, "approval-id", "", "")
		flags.BoolVar(&submit, "submit", false, "")
	}
	if flags.Parse(args) != nil || len(flags.Args()) > 0 || action == "execute" && (!submit || approvalID == "") {
		WriteDomainError(stdout, domain.NewError("invalid_arguments", "exact operation references and explicit execution parameters are required", 2, nil))
		return 2
	}
	config := filesystem.ManagedConfigStore{Root: filepath.Join(root, "ads-plan-monitor"), Path: filepath.Join(root, "ads-plan-monitor", "config.json")}
	service, close, err := bootstrap.NewQianchuanOperations(ctx, bootstrap.QianchuanOptions{Config: config, StateRoot: filepath.Join(root, "ads-plan-monitor", "state"), CredentialStore: credentials, Policy: runner.Policy, RuntimeVersion: runner.RuntimeVersion}, action)
	if err != nil {
		return operationCLIError(stdout, err)
	}
	defer close()
	var result any
	switch action {
	case "prepare":
		result, err = service.Prepare(ctx, preflight)
	case "get":
		result, err = service.Get(ctx, ref)
	case "confirm":
		result, err = service.Confirm(ctx, ref)
	case "execute":
		result, err = service.Execute(ctx, ref, approvalID)
	case "reconcile":
		result, err = service.Reconcile(ctx, ref)
	default:
		err = errors.New("unsupported operation action")
	}
	if err != nil {
		return operationCLIError(stdout, err)
	}
	if WriteJSONDestination(stdout, result, "") != nil {
		return 1
	}
	return 0
}

func (runner Runner) upgradeOperationState(ctx context.Context, args []string, root string, stdout io.Writer) int {
	flags := flag.NewFlagSet("operations upgrade-state", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var submit bool
	flags.BoolVar(&submit, "submit", false, "")
	if flags.Parse(args) != nil || len(flags.Args()) > 0 {
		return operationCLIError(stdout, errors.New("invalid migration arguments"))
	}
	path := filepath.Join(root, "ads-plan-monitor", "state", "ocean-watch.db")
	if !submit {
		inspection, err := (sqlite.Inspector{Path: path}).Inspect(ctx)
		if err != nil {
			return operationCLIError(stdout, err)
		}
		return writeOperationMigrationPreview(stdout, map[string]any{"mode": "dry_run", "state": inspection, "target_schema": sqlite.SchemaVersion})
	}
	store, err := sqlite.Upgrade(ctx, sqlite.Options{Path: path, RuntimeVersion: runner.RuntimeVersion})
	if err != nil {
		return operationCLIError(stdout, err)
	}
	defer store.Close()
	return writeOperationMigrationPreview(stdout, map[string]any{"mode": "submit", "status": "ready", "schema_version": sqlite.SchemaVersion})
}
func writeOperationMigrationPreview(stdout io.Writer, value any) int {
	if WriteJSONDestination(stdout, value, "") != nil {
		return 1
	}
	return 0
}

func operationCLIError(stdout io.Writer, err error) int {
	if errors.Is(err, sqlite.ErrIncompatible) {
		WriteDomainError(stdout, domain.NewError("state_incompatible", "state format is incompatible; inspect operations upgrade-state before any explicit migration", 1, nil))
		return 1
	}
	code := "operation_blocked"
	if errors.Is(err, qianchuan.ErrOperationChanged) {
		code = "operation_changed"
	}
	WriteDomainError(stdout, domain.NewError(code, "operation did not complete; inspect the saved operation before retrying", 1, nil))
	return 1
}
