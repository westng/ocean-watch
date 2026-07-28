package qianchuan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	sharedplans "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/plans"
	domainplans "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/plans"
	domainqianchuan "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/qianchuan"
	portqianchuan "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/ports/qianchuan"
)

const (
	DeleteMaterialsEndpoint = "/v1.0/qianchuan/uni_promotion/ad/material/delete/"
	DeleteMaterialLimit     = 100
	DeleteRiskNotice        = "官方接口在多号或多商品场景下可能同时删除同一素材的关联投放"
)

type RemoveWork struct {
	InputIndex  int    `json:"input_index"`
	InputURL    string `json:"input_url,omitempty"`
	AwemeItemID string `json:"aweme_item_id"`
}

type RemoveCommand struct {
	AdvertiserID    string
	AuthAccountID   string
	ReadAccessToken string
	AdID            string
	Submit          bool
	ConfirmDelete   bool
	Works           []RemoveWork
	SkippedLinks    []SkippedWork
}

type RemoveRow struct {
	InputIndex               int                       `json:"input_index"`
	InputURL                 string                    `json:"input_url,omitempty"`
	AwemeItemID              string                    `json:"aweme_item_id"`
	MaterialID               string                    `json:"material_id,omitempty"`
	MaterialSelectTypes      []string                  `json:"material_select_types,omitempty"`
	MaterialStatuses         []string                  `json:"material_statuses,omitempty"`
	VerifiedMaterialStatuses []string                  `json:"verified_material_statuses,omitempty"`
	CandidateMaterialIDs     []string                  `json:"candidate_material_ids,omitempty"`
	Status                   string                    `json:"status"`
	Reason                   string                    `json:"reason,omitempty"`
	Message                  string                    `json:"message,omitempty"`
	DispatchState            domainplans.DispatchState `json:"dispatch_state,omitempty"`
}

type RemoveBatch struct {
	MaterialIDs   []string                  `json:"material_ids"`
	Status        string                    `json:"status"`
	RequestID     string                    `json:"request_id,omitempty"`
	DispatchState domainplans.DispatchState `json:"dispatch_state,omitempty"`
	Error         string                    `json:"error,omitempty"`
}

type RemoveResult struct {
	Mode         string         `json:"mode"`
	Channel      string         `json:"channel"`
	AdvertiserID string         `json:"advertiser_id"`
	AdID         string         `json:"ad_id"`
	Endpoint     string         `json:"endpoint"`
	RiskNotice   string         `json:"risk_notice"`
	Counts       map[string]int `json:"counts"`
	Results      []RemoveRow    `json:"results"`
	SkippedLinks []SkippedWork  `json:"skipped_links"`
	Batches      []RemoveBatch  `json:"batches"`
	ExitCode     int            `json:"exit_code"`
}

type RemoveExecutor struct {
	Guard  sharedplans.GuardedExecutor
	Reader portqianchuan.Reader
	Writer portqianchuan.Writer
}

type normalizedRemoveCommand struct {
	RemoveCommand
	works []RemoveWork
}

func (executor RemoveExecutor) Execute(ctx context.Context, command RemoveCommand) (RemoveResult, error) {
	normalized, err := normalizeRemoveCommand(command)
	if err != nil {
		return RemoveResult{}, err
	}
	if !normalized.Submit {
		if executor.Reader == nil {
			return RemoveResult{}, errors.New("Qianchuan material removal reader is required")
		}
		if normalized.ReadAccessToken == "" {
			return RemoveResult{}, errors.New("Qianchuan material removal preflight access token is required")
		}
		return executor.execute(ctx, normalized, normalized.ReadAccessToken, nil, domainplans.WriteCapability{})
	}
	guarded, err := executor.Guard.Execute(ctx, sharedplans.GuardedMutation{
		Scope: domainplans.WriteScope{
			Channel: domainplans.ChannelQianchuan, AdvertiserID: normalized.AdvertiserID,
			LockFamily: domainplans.LockQianchuanWorks,
		},
		AuthAccountID: normalized.AuthAccountID, Submit: true,
		Validate: func() error {
			if executor.Reader == nil || executor.Writer == nil {
				return errors.New("Qianchuan material removal reader and writer are required")
			}
			return nil
		},
		Preview: normalized.preview(),
	}, func(ctx context.Context, execution sharedplans.MutationExecution) (any, error) {
		return executor.execute(ctx, normalized, execution.AccessToken, execution.Dispatcher, execution.Capability)
	})
	result, ok := guarded.Value.(RemoveResult)
	if !ok {
		if err != nil {
			return RemoveResult{}, err
		}
		return RemoveResult{}, errors.New("Qianchuan material removal executor returned an invalid result")
	}
	return result, err
}

func (executor RemoveExecutor) execute(
	ctx context.Context,
	command normalizedRemoveCommand,
	accessToken string,
	dispatcher *sharedplans.OnceDispatcher,
	capability domainplans.WriteCapability,
) (RemoveResult, error) {
	rows, err := fetchAllPlanMaterials(ctx, executor.Reader, command.AdvertiserID, accessToken, command.AdID)
	if err != nil {
		return RemoveResult{}, err
	}
	result, candidates := command.reconcile(rows)
	if !command.Submit || len(candidates) == 0 {
		result.finalize()
		return result, nil
	}
	rowsByMaterial := map[string][]int{}
	materialIDs := []string{}
	for _, index := range candidates {
		materialID := result.Results[index].MaterialID
		if len(rowsByMaterial[materialID]) == 0 {
			materialIDs = append(materialIDs, materialID)
		}
		rowsByMaterial[materialID] = append(rowsByMaterial[materialID], index)
	}
	for start := 0; start < len(materialIDs); start += DeleteMaterialLimit {
		end := min(start+DeleteMaterialLimit, len(materialIDs))
		batchIDs := append([]string(nil), materialIDs[start:end]...)
		receipt := dispatcher.Dispatch(ctx, capability, deleteMaterialOperationKey(command.AdID, batchIDs), func(ctx context.Context) (any, error) {
			return executor.Writer.DeleteMaterials(ctx, portqianchuan.DeleteMaterialsRequest{
				AdvertiserID: command.AdvertiserID, AccessToken: accessToken,
				AdID: command.AdID, MaterialIDs: batchIDs,
			})
		})
		batch := RemoveBatch{MaterialIDs: batchIDs, DispatchState: receipt.State}
		written, _ := receipt.Value.(portqianchuan.WriteResult)
		batch.RequestID = written.RequestID
		writeErr := receipt.Error
		if writeErr == nil && len(written.RowErrors) != 0 {
			writeErr = errors.New("Qianchuan material deletion returned unexpected row errors")
		}
		if writeErr != nil && receipt.State != domainplans.DispatchUnknown {
			batch.Status, batch.Error = "failed", writeErr.Error()
			for _, materialID := range batchIDs {
				for _, index := range rowsByMaterial[materialID] {
					result.Results[index].Status = "failed"
					result.Results[index].Reason = "official_delete_failed"
					result.Results[index].Message = writeErr.Error()
					result.Results[index].DispatchState = receipt.State
				}
			}
		} else {
			batch.Status = "submitted"
			for _, materialID := range batchIDs {
				for _, index := range rowsByMaterial[materialID] {
					result.Results[index].Status = "delete_submitted"
					result.Results[index].DispatchState = receipt.State
				}
			}
		}
		result.Batches = append(result.Batches, batch)
	}
	verified, readErr := fetchAllPlanMaterials(ctx, executor.Reader, command.AdvertiserID, accessToken, command.AdID)
	if readErr != nil {
		for _, index := range candidates {
			if result.Results[index].Status != "delete_submitted" {
				continue
			}
			result.Results[index].Status = "failed"
			result.Results[index].Reason = "delete_verification_failed"
			result.Results[index].Message = readErr.Error()
		}
		result.finalize()
		return result, nil
	}
	statuses := materialStatusesByID(verified)
	for _, index := range candidates {
		row := &result.Results[index]
		if row.Status != "delete_submitted" {
			continue
		}
		row.VerifiedMaterialStatuses = sortedSet(statuses[row.MaterialID])
		if reflectStringSlice(row.VerifiedMaterialStatuses, []string{"DELETED"}) {
			if row.DispatchState == domainplans.DispatchUnknown {
				row.Status = "reconciled"
			} else {
				row.Status = "deleted"
			}
			continue
		}
		row.Status = "failed"
		row.Reason = "delete_verification_failed"
		row.Message = "删除接口完成后未确认到 DELETED 状态"
	}
	result.finalize()
	return result, nil
}

func normalizeRemoveCommand(command RemoveCommand) (normalizedRemoveCommand, error) {
	command.AdvertiserID = strings.TrimSpace(command.AdvertiserID)
	command.AuthAccountID = strings.TrimSpace(command.AuthAccountID)
	command.ReadAccessToken = strings.TrimSpace(command.ReadAccessToken)
	command.AdID = strings.TrimSpace(command.AdID)
	if !validPositiveID(command.AdvertiserID) || !validPositiveID(command.AdID) {
		return normalizedRemoveCommand{}, errors.New("Qianchuan advertiser_id and ad_id must be valid positive decimal IDs")
	}
	if len(command.Works) == 0 {
		return normalizedRemoveCommand{}, errors.New("at least one resolved Qianchuan work is required")
	}
	if command.Submit && !command.ConfirmDelete {
		return normalizedRemoveCommand{}, errors.New("Qianchuan material deletion requires explicit confirm-delete")
	}
	if !command.Submit && command.ConfirmDelete {
		return normalizedRemoveCommand{}, errors.New("confirm-delete is valid only with submit")
	}
	works := make([]RemoveWork, 0, len(command.Works))
	seen := map[string]struct{}{}
	for index, work := range command.Works {
		work.InputURL = strings.TrimSpace(work.InputURL)
		work.AwemeItemID = strings.TrimSpace(work.AwemeItemID)
		if work.InputIndex < 0 {
			work.InputIndex = index
		}
		if !validPositiveID(work.AwemeItemID) {
			return normalizedRemoveCommand{}, errors.New("resolved Qianchuan work contains an invalid aweme_item_id")
		}
		if _, duplicate := seen[work.AwemeItemID]; duplicate {
			continue
		}
		seen[work.AwemeItemID] = struct{}{}
		works = append(works, work)
	}
	command.Works = works
	return normalizedRemoveCommand{RemoveCommand: command, works: works}, nil
}

func (command normalizedRemoveCommand) preview() RemoveResult {
	return RemoveResult{
		Mode: "dry_run", Channel: "qianchuan", AdvertiserID: command.AdvertiserID,
		AdID: command.AdID, Endpoint: DeleteMaterialsEndpoint, RiskNotice: DeleteRiskNotice,
		Counts: map[string]int{}, Results: []RemoveRow{},
		SkippedLinks: append([]SkippedWork(nil), command.SkippedLinks...), Batches: []RemoveBatch{},
	}
}

func (command normalizedRemoveCommand) reconcile(materials []domainqianchuan.PlanMaterial) (RemoveResult, []int) {
	result := command.preview()
	if command.Submit {
		result.Mode = "submit"
	}
	byWork := map[string][]domainqianchuan.PlanMaterial{}
	for _, material := range materials {
		if material.AwemeItemID != "" {
			byWork[material.AwemeItemID] = append(byWork[material.AwemeItemID], material)
		}
	}
	candidates := []int{}
	for _, work := range command.works {
		row := RemoveRow{
			InputIndex: work.InputIndex, InputURL: work.InputURL,
			AwemeItemID: work.AwemeItemID, Status: "ready",
		}
		matches := byWork[work.AwemeItemID]
		if len(matches) == 0 {
			row.Status, row.Reason, row.Message = "skipped", "work_not_in_plan", "作品不在目标计划的视频素材中"
			result.Results = append(result.Results, row)
			continue
		}
		materialSet, selectSet, statusSet := map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}
		missingMaterialID := false
		for _, match := range matches {
			if !validPositiveID(match.MaterialID) {
				missingMaterialID = true
			} else {
				materialSet[match.MaterialID] = struct{}{}
			}
			selectSet[defaultUnknown(match.MaterialSelectType)] = struct{}{}
			statusSet[defaultUnknown(match.MaterialStatus)] = struct{}{}
		}
		row.CandidateMaterialIDs = sortedSet(materialSet)
		row.MaterialSelectTypes = sortedSet(selectSet)
		row.MaterialStatuses = sortedSet(statusSet)
		if missingMaterialID {
			row.Status, row.Reason, row.Message = "failed", "missing_material_id", "计划素材没有返回可删除的 material_id"
			result.Results = append(result.Results, row)
			continue
		}
		if len(row.CandidateMaterialIDs) != 1 {
			row.Status, row.Reason, row.Message = "failed", "ambiguous_material_match", "同一作品匹配到多个不同的计划素材 ID"
			result.Results = append(result.Results, row)
			continue
		}
		row.MaterialID = row.CandidateMaterialIDs[0]
		if !reflectStringSlice(row.MaterialSelectTypes, []string{"CUSTOM"}) {
			row.Status, row.Reason, row.Message = "skipped", "unsupported_material_select_type", "官方删除接口仅支持 CUSTOM 自提素材"
			result.Results = append(result.Results, row)
			continue
		}
		if reflectStringSlice(row.MaterialStatuses, []string{"DELETED"}) {
			row.Status = "already_deleted"
			result.Results = append(result.Results, row)
			continue
		}
		row.Status = "would_delete"
		result.Results = append(result.Results, row)
		candidates = append(candidates, len(result.Results)-1)
	}
	return result, candidates
}

func (result *RemoveResult) finalize() {
	result.Counts = map[string]int{
		"input_works": len(result.Results), "skipped_links": len(result.SkippedLinks),
	}
	result.ExitCode = 0
	for _, row := range result.Results {
		result.Counts[row.Status]++
		if row.Status == "failed" {
			result.ExitCode = 1
		}
	}
}

func materialStatusesByID(rows []domainqianchuan.PlanMaterial) map[string]map[string]struct{} {
	result := map[string]map[string]struct{}{}
	for _, row := range rows {
		if row.MaterialID == "" {
			continue
		}
		set := result[row.MaterialID]
		if set == nil {
			set = map[string]struct{}{}
			result[row.MaterialID] = set
		}
		set[defaultUnknown(row.MaterialStatus)] = struct{}{}
	}
	return result
}

func sortedSet(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	for index := 1; index < len(result); index++ {
		for cursor := index; cursor > 0 && result[cursor] < result[cursor-1]; cursor-- {
			result[cursor], result[cursor-1] = result[cursor-1], result[cursor]
		}
	}
	return result
}

func defaultUnknown(value string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return "UNKNOWN"
}

func reflectStringSlice(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func deleteMaterialOperationKey(adID string, materialIDs []string) string {
	digest := sha256.Sum256([]byte(adID + "\x00" + strings.Join(materialIDs, "\x00")))
	return "qianchuan-material-delete-" + hex.EncodeToString(digest[:16])
}
