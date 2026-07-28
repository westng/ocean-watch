package marketing

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"

	sharedplans "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/plans"
	domainplans "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/plans"
	portmarketing "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/ports/marketing"
)

const marketingMutationBatchLimit = 10

var marketingMutationEndpoints = map[portmarketing.MutationKind]string{
	portmarketing.MutationProjectStatus:   "/v3.0/project/status/update/",
	portmarketing.MutationPromotionStatus: "/v3.0/promotion/status/update/",
	portmarketing.MutationPromotionBudget: "/v3.0/promotion/budget/update/",
	portmarketing.MutationPromotionBid:    "/v3.0/promotion/bid/update/",
	portmarketing.MutationProjectROI:      "/v3.0/project/roigoal/update/",
}

type MutationCommand struct {
	AdvertiserID  string
	AuthAccountID string
	Submit        bool
	Kind          portmarketing.MutationKind
	ObjectIDs     []string
	Status        string
	Value         string
}

type MutationRow struct {
	ObjectID      string                    `json:"object_id"`
	Status        string                    `json:"status"`
	Target        string                    `json:"target"`
	Observed      string                    `json:"observed,omitempty"`
	OfficialError string                    `json:"official_error,omitempty"`
	Error         string                    `json:"error,omitempty"`
	DispatchState domainplans.DispatchState `json:"dispatch_state,omitempty"`
}

type MutationResult struct {
	Mode          string                     `json:"mode"`
	Channel       string                     `json:"channel"`
	Operation     portmarketing.MutationKind `json:"operation"`
	Endpoint      string                     `json:"endpoint"`
	AdvertiserID  string                     `json:"advertiser_id"`
	Submitted     bool                       `json:"submitted"`
	Rows          []MutationRow              `json:"results"`
	SuccessCount  int                        `json:"success_count"`
	FailureCount  int                        `json:"failure_count"`
	ExitCode      int                        `json:"exit_code"`
	RequestID     string                     `json:"request_id,omitempty"`
	DispatchState domainplans.DispatchState  `json:"dispatch_state,omitempty"`
}

type MutationExecutor struct {
	Guard  sharedplans.GuardedExecutor
	Writer portmarketing.PlanMutationWriter
	Reader portmarketing.PlanMutationReader
}

type normalizedMutation struct {
	MutationCommand
	endpoint string
	target   string
}

func (executor MutationExecutor) Execute(
	ctx context.Context,
	command MutationCommand,
) (MutationResult, error) {
	normalized, err := normalizeMutation(command)
	if err != nil {
		return MutationResult{}, err
	}
	preview := normalized.preview()
	guarded, err := executor.Guard.Execute(ctx, sharedplans.GuardedMutation{
		Scope: domainplans.WriteScope{
			Channel: domainplans.ChannelMarketing, AdvertiserID: normalized.AdvertiserID,
			LockFamily: domainplans.LockPlanSettings,
		},
		AuthAccountID: normalized.AuthAccountID,
		Submit:        normalized.Submit,
		Validate: func() error {
			if normalized.Submit && (executor.Writer == nil || executor.Reader == nil) {
				return errors.New("Marketing mutation writer and reader are required")
			}
			return nil
		},
		Preview: preview,
	}, func(ctx context.Context, execution sharedplans.MutationExecution) (any, error) {
		return executor.executeMutation(ctx, normalized, execution), nil
	})
	if guarded.Mode == "dry_run" {
		return preview, err
	}
	result, ok := guarded.Value.(MutationResult)
	if !ok {
		if err != nil {
			return MutationResult{}, err
		}
		return MutationResult{}, errors.New("Marketing mutation executor returned an invalid result")
	}
	return result, err
}

func (executor MutationExecutor) executeMutation(
	ctx context.Context,
	command normalizedMutation,
	execution sharedplans.MutationExecution,
) MutationResult {
	result := command.preview()
	result.Mode = "submit"
	result.Submitted = true
	request := portmarketing.MutationRequest{
		AdvertiserID: command.AdvertiserID, AccessToken: execution.AccessToken,
		Kind: command.Kind, ObjectIDs: append([]string(nil), command.ObjectIDs...),
		Status: command.Status, Value: command.Value,
	}
	receipt := execution.Dispatcher.Dispatch(
		ctx, execution.Capability, "marketing-mutation/"+string(command.Kind),
		func(ctx context.Context) (any, error) {
			return executor.Writer.ApplyMutation(ctx, request)
		},
	)
	result.DispatchState = receipt.State
	writeResult, _ := receipt.Value.(portmarketing.MutationWriteResult)
	result.RequestID = writeResult.RequestID
	if receipt.State == domainplans.DispatchNotSent {
		message := "Marketing mutation was not sent"
		if receipt.Error != nil {
			message = receipt.Error.Error()
		}
		for index := range result.Rows {
			result.Rows[index].Status = "failed"
			result.Rows[index].Error = message
			result.Rows[index].DispatchState = receipt.State
		}
		result.finalize()
		return result
	}

	snapshots, readErr := executor.Reader.ReadMutation(ctx, request)
	observed := make(map[string]portmarketing.MutationSnapshot, len(snapshots))
	for _, snapshot := range snapshots {
		if _, exists := observed[snapshot.ObjectID]; exists {
			readErr = errors.Join(readErr, fmt.Errorf(
				"official readback returned duplicate object %s", snapshot.ObjectID,
			))
			continue
		}
		observed[snapshot.ObjectID] = snapshot
	}
	for index := range result.Rows {
		row := &result.Rows[index]
		row.DispatchState = receipt.State
		if message := strings.TrimSpace(writeResult.RowErrors[row.ObjectID]); message != "" {
			row.Status = "failed"
			row.OfficialError = message
		}
		if receipt.Error != nil && receipt.State == domainplans.DispatchAcknowledged {
			row.Status = "failed"
			row.Error = receipt.Error.Error()
		}
		if readErr != nil {
			row.Status = "failed"
			row.Error = joinMutationError(row.Error, "official readback failed: "+readErr.Error())
			continue
		}
		snapshot, exists := observed[row.ObjectID]
		if !exists {
			row.Status = "failed"
			row.Error = joinMutationError(row.Error, "official readback did not return this object")
			continue
		}
		row.Observed = snapshotTarget(command.Kind, snapshot)
		if !mutationTargetMatches(command.Kind, command.target, row.Observed) {
			row.Status = "failed"
			row.Error = joinMutationError(row.Error, "official readback does not match the requested target")
			continue
		}
		if row.Status != "failed" {
			if receipt.State == domainplans.DispatchUnknown {
				row.Status = "reconciled"
			} else {
				row.Status = "completed"
			}
		}
	}
	result.finalize()
	return result
}

func normalizeMutation(command MutationCommand) (normalizedMutation, error) {
	command.AdvertiserID = strings.TrimSpace(command.AdvertiserID)
	command.AuthAccountID = strings.TrimSpace(command.AuthAccountID)
	command.Status = strings.TrimSpace(command.Status)
	command.Value = strings.TrimSpace(command.Value)
	if !validPositiveID(command.AdvertiserID) {
		return normalizedMutation{}, errors.New("advertiser_id must be a positive decimal ID")
	}
	endpoint, ok := marketingMutationEndpoints[command.Kind]
	if !ok {
		return normalizedMutation{}, errors.New("Marketing mutation operation is unsupported")
	}
	ids, err := normalizeMutationIDs(command.ObjectIDs)
	if err != nil {
		return normalizedMutation{}, err
	}
	command.ObjectIDs = ids
	target := command.Status
	switch command.Kind {
	case portmarketing.MutationProjectStatus, portmarketing.MutationPromotionStatus:
		if command.Status != "ENABLE" && command.Status != "DISABLE" {
			return normalizedMutation{}, errors.New("Marketing status must be ENABLE or DISABLE")
		}
		if command.Value != "" {
			return normalizedMutation{}, errors.New("Marketing status mutation does not accept a value")
		}
	case portmarketing.MutationPromotionBudget, portmarketing.MutationProjectROI:
		if command.Status != "" {
			return normalizedMutation{}, errors.New("Marketing numeric mutation does not accept a status")
		}
		target, err = normalizePositiveDecimal(command.Value, "value", nil, nil)
		if err != nil {
			return normalizedMutation{}, err
		}
		command.Value = target
	case portmarketing.MutationPromotionBid:
		if command.Status != "" {
			return normalizedMutation{}, errors.New("Marketing bid mutation does not accept a status")
		}
		minimum, maximum := big.NewRat(1, 100), big.NewRat(10000, 1)
		target, err = normalizePositiveDecimal(command.Value, "bid", minimum, maximum)
		if err != nil {
			return normalizedMutation{}, err
		}
		command.Value = target
	}
	return normalizedMutation{MutationCommand: command, endpoint: endpoint, target: target}, nil
}

func normalizeMutationIDs(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, errors.New("at least one Marketing object ID is required")
	}
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if !validPositiveID(value) {
			return nil, errors.New("Marketing object IDs must be positive decimal IDs")
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	if len(result) > marketingMutationBatchLimit {
		return nil, fmt.Errorf("Marketing mutation accepts at most %d unique IDs", marketingMutationBatchLimit)
	}
	return result, nil
}

func normalizePositiveDecimal(
	value string,
	field string,
	minimum *big.Rat,
	maximum *big.Rat,
) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "eE") {
		return "", fmt.Errorf("%s must be a plain positive decimal", field)
	}
	parts := strings.Split(value, ".")
	if len(parts) > 2 || len(parts[0]) == 0 || (len(parts) == 2 && len(parts[1]) > 2) {
		return "", fmt.Errorf("%s supports at most two decimal places", field)
	}
	for _, part := range parts {
		if part == "" {
			continue
		}
		for _, character := range part {
			if character < '0' || character > '9' {
				return "", fmt.Errorf("%s must be a plain positive decimal", field)
			}
		}
	}
	rational, ok := new(big.Rat).SetString(value)
	if !ok || rational.Sign() <= 0 {
		return "", fmt.Errorf("%s must be greater than zero", field)
	}
	if minimum != nil && rational.Cmp(minimum) < 0 {
		return "", fmt.Errorf("%s is below the supported minimum", field)
	}
	if maximum != nil && rational.Cmp(maximum) > 0 {
		return "", fmt.Errorf("%s exceeds the supported maximum", field)
	}
	integer := new(big.Int).Quo(rational.Num(), rational.Denom())
	fraction := new(big.Rat).Sub(rational, new(big.Rat).SetInt(integer))
	if fraction.Sign() == 0 {
		return integer.String(), nil
	}
	return strings.TrimRight(strings.TrimRight(rational.FloatString(2), "0"), "."), nil
}

func (command normalizedMutation) preview() MutationResult {
	rows := make([]MutationRow, 0, len(command.ObjectIDs))
	for _, objectID := range command.ObjectIDs {
		rows = append(rows, MutationRow{
			ObjectID: objectID, Status: "ready", Target: command.target,
		})
	}
	return MutationResult{
		Mode: "dry_run", Channel: "marketing", Operation: command.Kind,
		Endpoint: command.endpoint, AdvertiserID: command.AdvertiserID, Rows: rows,
	}
}

func (result *MutationResult) finalize() {
	result.SuccessCount = 0
	result.FailureCount = 0
	result.ExitCode = 0
	for _, row := range result.Rows {
		switch row.Status {
		case "completed", "reconciled":
			result.SuccessCount++
		default:
			result.FailureCount++
		}
	}
	if result.FailureCount != 0 {
		result.ExitCode = 1
	}
}

func snapshotTarget(kind portmarketing.MutationKind, snapshot portmarketing.MutationSnapshot) string {
	switch kind {
	case portmarketing.MutationProjectStatus, portmarketing.MutationPromotionStatus:
		return strings.TrimSpace(snapshot.Status)
	default:
		return strings.TrimSpace(snapshot.Value)
	}
}

func mutationTargetMatches(kind portmarketing.MutationKind, expected, observed string) bool {
	if kind == portmarketing.MutationProjectStatus || kind == portmarketing.MutationPromotionStatus {
		return expected == observed
	}
	expectedRat, expectedOK := new(big.Rat).SetString(expected)
	observedRat, observedOK := new(big.Rat).SetString(observed)
	return expectedOK && observedOK && expectedRat.Cmp(observedRat) == 0
}

func joinMutationError(current, addition string) string {
	current = strings.TrimSpace(current)
	addition = strings.TrimSpace(addition)
	if current == "" {
		return addition
	}
	if addition == "" {
		return current
	}
	items := []string{current, addition}
	sort.Strings(items)
	return strings.Join(items, "; ")
}
