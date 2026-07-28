package qianchuan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"

	sharedplans "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/plans"
	domainplans "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/plans"
	domainqianchuan "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/qianchuan"
	portqianchuan "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/ports/qianchuan"
)

const qianchuanMutationBatchLimit = 10

var qianchuanMutationEndpoints = map[portqianchuan.MutationKind]string{
	portqianchuan.MutationStatus: "/v1.0/qianchuan/uni_promotion/ad/status/update/",
	portqianchuan.MutationBudget: "/v1.0/qianchuan/uni_promotion/ad/budget/update/",
	portqianchuan.MutationROI:    "/v1.0/qianchuan/uni_promotion/ad/roi2_goal/update/",
}

type MutationCommand struct {
	AdvertiserID       string
	AuthAccountID      string
	Submit             bool
	ConfirmDelete      bool
	Kind               portqianchuan.MutationKind
	AdIDs              []string
	Status             string
	Value              string
	DeepExternalAction string
}

type MutationRow struct {
	AdID          string                    `json:"ad_id"`
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
	Operation     portqianchuan.MutationKind `json:"operation"`
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
	Writer portqianchuan.Writer
	Reader portqianchuan.Reader
}

type normalizedMutationCommand struct {
	MutationCommand
	endpoint string
	target   string
}

func (executor MutationExecutor) Execute(ctx context.Context, command MutationCommand) (MutationResult, error) {
	normalized, err := normalizeQianchuanMutation(command)
	if err != nil {
		return MutationResult{}, err
	}
	preview := normalized.preview()
	guarded, err := executor.Guard.Execute(ctx, sharedplans.GuardedMutation{
		Scope: domainplans.WriteScope{
			Channel: domainplans.ChannelQianchuan, AdvertiserID: normalized.AdvertiserID,
			LockFamily: domainplans.LockPlanSettings,
		},
		AuthAccountID: normalized.AuthAccountID, Submit: normalized.Submit,
		Validate: func() error {
			if normalized.Submit && (executor.Writer == nil || executor.Reader == nil) {
				return errors.New("Qianchuan mutation writer and reader are required")
			}
			return nil
		},
		Preview: preview,
	}, func(ctx context.Context, execution sharedplans.MutationExecution) (any, error) {
		return executor.execute(ctx, normalized, execution), nil
	})
	if guarded.Mode == "dry_run" {
		return preview, err
	}
	result, ok := guarded.Value.(MutationResult)
	if !ok {
		if err != nil {
			return MutationResult{}, err
		}
		return MutationResult{}, errors.New("Qianchuan mutation executor returned an invalid result")
	}
	return result, err
}

func (executor MutationExecutor) execute(
	ctx context.Context,
	command normalizedMutationCommand,
	execution sharedplans.MutationExecution,
) MutationResult {
	result := command.preview()
	result.Mode, result.Submitted = "submit", true
	request := portqianchuan.MutationRequest{
		AdvertiserID: command.AdvertiserID, AccessToken: execution.AccessToken,
		Kind: command.Kind, AdIDs: append([]string(nil), command.AdIDs...),
		Status: command.Status, Value: command.Value,
		DeepExternalAction: command.DeepExternalAction,
	}
	receipt := execution.Dispatcher.Dispatch(
		ctx, execution.Capability, qianchuanMutationOperationKey(command),
		func(ctx context.Context) (any, error) { return executor.Writer.UpdatePlan(ctx, request) },
	)
	result.DispatchState = receipt.State
	written, _ := receipt.Value.(portqianchuan.WriteResult)
	result.RequestID = written.RequestID
	rowErrors, rowErrorProblem := normalizeQianchuanRowErrors(command.AdIDs, written.RowErrors)
	if receipt.State == domainplans.DispatchNotSent {
		message := "Qianchuan mutation was not sent"
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
	for index := range result.Rows {
		row := &result.Rows[index]
		row.DispatchState = receipt.State
		if officialError := rowErrors[row.AdID]; officialError != "" {
			row.Status, row.OfficialError = "failed", officialError
		}
		if rowErrorProblem != "" {
			row.Status = "failed"
			row.Error = joinQianchuanMutationError(row.Error, rowErrorProblem)
		}
		if receipt.Error != nil && receipt.State == domainplans.DispatchAcknowledged && len(rowErrors) == 0 {
			row.Status = "failed"
			row.Error = joinQianchuanMutationError(row.Error, receipt.Error.Error())
		}
		detail, readErr := executor.Reader.FetchPlanDetail(ctx, portqianchuan.PlanDetailRequest{
			AdvertiserID: command.AdvertiserID, AccessToken: execution.AccessToken, AdID: row.AdID,
		})
		if readErr != nil {
			row.Status = "failed"
			row.Error = joinQianchuanMutationError(row.Error, "official readback failed: "+readErr.Error())
			continue
		}
		if detail.AdID != row.AdID {
			row.Status = "failed"
			row.Error = joinQianchuanMutationError(row.Error, "official readback returned a mismatched ad_id")
			continue
		}
		row.Observed = qianchuanMutationSnapshot(command.Kind, detail)
		if !qianchuanMutationTargetMatches(command.Kind, command.target, row.Observed) {
			row.Status = "failed"
			row.Error = joinQianchuanMutationError(row.Error, "official readback does not match the requested target")
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

func normalizeQianchuanMutation(command MutationCommand) (normalizedMutationCommand, error) {
	command.AdvertiserID = strings.TrimSpace(command.AdvertiserID)
	command.AuthAccountID = strings.TrimSpace(command.AuthAccountID)
	command.Status = strings.TrimSpace(command.Status)
	command.Value = strings.TrimSpace(command.Value)
	command.DeepExternalAction = strings.TrimSpace(command.DeepExternalAction)
	if !validPositiveID(command.AdvertiserID) {
		return normalizedMutationCommand{}, errors.New("advertiser_id must be a valid positive decimal ID")
	}
	endpoint, ok := qianchuanMutationEndpoints[command.Kind]
	if !ok {
		return normalizedMutationCommand{}, errors.New("Qianchuan mutation operation is unsupported")
	}
	ids, err := normalizeQianchuanMutationIDs(command.AdIDs)
	if err != nil {
		return normalizedMutationCommand{}, err
	}
	command.AdIDs = ids
	target := command.Status
	switch command.Kind {
	case portqianchuan.MutationStatus:
		if command.Status != "ENABLE" && command.Status != "DISABLE" && command.Status != "DELETE" {
			return normalizedMutationCommand{}, errors.New("Qianchuan status must be ENABLE, DISABLE, or DELETE")
		}
		if command.Value != "" || command.DeepExternalAction != "" {
			return normalizedMutationCommand{}, errors.New("Qianchuan status mutation does not accept a value or ROI action")
		}
		if command.Submit && command.Status == "DELETE" && !command.ConfirmDelete {
			return normalizedMutationCommand{}, errors.New("Qianchuan DELETE submission requires explicit confirm-delete")
		}
		if command.Status != "DELETE" && command.ConfirmDelete {
			return normalizedMutationCommand{}, errors.New("confirm-delete is valid only with Qianchuan DELETE")
		}
	case portqianchuan.MutationBudget:
		if command.Status != "" || command.DeepExternalAction != "" || command.ConfirmDelete {
			return normalizedMutationCommand{}, errors.New("Qianchuan budget mutation accepts only a numeric value")
		}
		target, err = normalizeQianchuanDecimal(command.Value, "budget")
		if err != nil {
			return normalizedMutationCommand{}, err
		}
		command.Value = target
	case portqianchuan.MutationROI:
		if command.Status != "" || command.ConfirmDelete {
			return normalizedMutationCommand{}, errors.New("Qianchuan ROI mutation does not accept status or delete confirmation")
		}
		if command.DeepExternalAction != "" &&
			command.DeepExternalAction != "AD_CONVERT_TYPE_LIVE_PAY_ROI" &&
			command.DeepExternalAction != "AD_CONVERT_TYPE_LIVE_PURE_PAY_ROI" {
			return normalizedMutationCommand{}, errors.New("Qianchuan ROI deep_external_action is unsupported")
		}
		target, err = normalizeQianchuanDecimal(command.Value, "roi2_goal")
		if err != nil {
			return normalizedMutationCommand{}, err
		}
		command.Value = target
	}
	return normalizedMutationCommand{MutationCommand: command, endpoint: endpoint, target: target}, nil
}

func normalizeQianchuanMutationIDs(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, errors.New("at least one Qianchuan ad_id is required")
	}
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if !validPositiveID(value) {
			return nil, errors.New("Qianchuan ad_ids must be valid positive decimal IDs")
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	if len(result) > qianchuanMutationBatchLimit {
		return nil, fmt.Errorf("Qianchuan mutation accepts at most %d unique ad_ids", qianchuanMutationBatchLimit)
	}
	return result, nil
}

func normalizeQianchuanDecimal(value, field string) (string, error) {
	value = strings.TrimSpace(value)
	parts := strings.Split(value, ".")
	if value == "" || strings.ContainsAny(value, "eE") || len(parts) > 2 ||
		parts[0] == "" || (len(parts) == 2 && (parts[1] == "" || len(parts[1]) > 2)) {
		return "", fmt.Errorf("%s must be a plain positive decimal with at most two decimal places", field)
	}
	for _, part := range parts {
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
	integer := new(big.Int).Quo(rational.Num(), rational.Denom())
	fraction := new(big.Rat).Sub(rational, new(big.Rat).SetInt(integer))
	if fraction.Sign() == 0 {
		return integer.String(), nil
	}
	return strings.TrimRight(strings.TrimRight(rational.FloatString(2), "0"), "."), nil
}

func (command normalizedMutationCommand) preview() MutationResult {
	rows := make([]MutationRow, 0, len(command.AdIDs))
	for _, adID := range command.AdIDs {
		rows = append(rows, MutationRow{AdID: adID, Status: "ready", Target: command.target})
	}
	return MutationResult{
		Mode: "dry_run", Channel: "qianchuan", Operation: command.Kind,
		Endpoint: command.endpoint, AdvertiserID: command.AdvertiserID, Rows: rows,
	}
}

func (result *MutationResult) finalize() {
	result.SuccessCount, result.FailureCount, result.ExitCode = 0, 0, 0
	for _, row := range result.Rows {
		if row.Status == "completed" || row.Status == "reconciled" {
			result.SuccessCount++
		} else {
			result.FailureCount++
		}
	}
	if result.FailureCount != 0 {
		result.ExitCode = 1
	}
}

func normalizeQianchuanRowErrors(requested []string, values []portqianchuan.RowError) (map[string]string, string) {
	allowed := stringSetFrom(requested)
	result := map[string]string{}
	for _, value := range values {
		value.ObjectID = strings.TrimSpace(value.ObjectID)
		if _, ok := allowed[value.ObjectID]; !ok {
			return result, "official mutation result returned an unexpected ad_id"
		}
		if _, duplicate := result[value.ObjectID]; duplicate {
			return result, "official mutation result repeated an ad_id"
		}
		message := strings.TrimSpace(value.Message)
		if message == "" {
			message = "official mutation row failed"
		}
		if value.Code != "" {
			message = strings.TrimSpace(value.Code) + ": " + message
		}
		result[value.ObjectID] = message
	}
	return result, ""
}

func qianchuanMutationSnapshot(kind portqianchuan.MutationKind, detail domainqianchuan.PlanDetail) string {
	switch kind {
	case portqianchuan.MutationStatus:
		return strings.TrimSpace(detail.OptStatus)
	case portqianchuan.MutationBudget:
		if detail.Budget != nil {
			return detail.Budget.String()
		}
	case portqianchuan.MutationROI:
		if detail.ROI2Goal != nil {
			return detail.ROI2Goal.String()
		}
	}
	return ""
}

func qianchuanMutationTargetMatches(kind portqianchuan.MutationKind, expected, observed string) bool {
	if kind == portqianchuan.MutationStatus {
		return expected == observed
	}
	expectedRat, expectedOK := new(big.Rat).SetString(expected)
	observedRat, observedOK := new(big.Rat).SetString(observed)
	return expectedOK && observedOK && expectedRat.Cmp(observedRat) == 0
}

func qianchuanMutationOperationKey(command normalizedMutationCommand) string {
	value := strings.Join([]string{
		string(command.Kind), strings.Join(command.AdIDs, ","), command.target, command.DeepExternalAction,
	}, "\x00")
	digest := sha256.Sum256([]byte(value))
	return "qianchuan-plan-mutation-" + hex.EncodeToString(digest[:16])
}

func joinQianchuanMutationError(current, addition string) string {
	current, addition = strings.TrimSpace(current), strings.TrimSpace(addition)
	if current == "" {
		return addition
	}
	if addition == "" || addition == current {
		return current
	}
	return current + "; " + addition
}
