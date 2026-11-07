package qianchuan

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	applicationops "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/operations"
	sharedplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/policy"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/contracts"
	ops "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/operations"
	domainplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/plans"
	domaintemplates "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/templates"
	port "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/qianchuan"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/state"
)

var ErrOperationChanged = errors.New("Qianchuan operation changed; run preflight and confirmation again")
var ErrOperationUnsupported = errors.New("Qianchuan operation is outside the supported preview scope")

const operationKind = "qianchuan_batch_execution_v1"
const executionTTL = 5 * time.Minute

type OperationReference struct {
	OperationID    string `json:"operation_id"`
	Version        int    `json:"version"`
	SnapshotDigest string `json:"snapshot_digest"`
}

type OperationView struct {
	OperationReference
	Status       ops.Status          `json:"status"`
	AdvertiserID string              `json:"advertiser_id"`
	ExpiresAt    time.Time           `json:"expires_at"`
	Summary      string              `json:"summary"`
	Items        []OperationItemView `json:"items"`
}

type OperationItemView struct {
	ItemID        string            `json:"item_id"`
	GroupID       string            `json:"group_id"`
	Action        string            `json:"action"`
	TargetID      string            `json:"target_id"`
	DispatchState ops.DispatchState `json:"dispatch_state"`
	Applied       bool              `json:"applied"`
}

type operationWrite struct {
	ItemID   string          `json:"item_id"`
	GroupID  string          `json:"group_id"`
	Action   string          `json:"action"`
	TargetID string          `json:"target_id"`
	PlanName string          `json:"plan_name"`
	ItemIDs  []string        `json:"item_ids"`
	Payload  json.RawMessage `json:"payload"`
}

type operationPayload struct {
	Contract        string                 `json:"contract"`
	PreflightID     string                 `json:"preflight_id"`
	Preflight       preparedBatchSnapshot  `json:"preflight"`
	AuthorizationID string                 `json:"authorization_id"`
	Items           []ops.ConfirmationItem `json:"items"`
	Writes          []operationWrite       `json:"writes"`
}

type OperationService struct {
	Commands       CommandService
	Core           applicationops.Service
	Recovery       state.OperationRecovery
	Policy         policy.Instance
	RuntimeVersion string
}

func (s OperationService) check(subject string, mode contracts.QueryExecutionMode) error {
	if err := s.Policy.Check([]string{"qianchuan"}, mode); err != nil {
		return err
	}
	if subject != "" {
		return s.Policy.CheckSubject("qianchuan", "advertiser", subject)
	}
	return nil
}

func (s OperationService) Prepare(ctx context.Context, preflightID string) (OperationView, error) {
	if err := s.check("", contracts.QueryModePlan); err != nil {
		return OperationView{}, err
	}
	if !batchPreflightIDPattern.MatchString(preflightID) || s.Commands.Journals == nil || s.Core.Snapshots == nil {
		return OperationView{}, ErrOperationUnsupported
	}
	sum := sha256.Sum256([]byte(preflightID))
	id := "op_qc_" + hex.EncodeToString(sum[:])
	if existing, err := s.Core.Snapshots.GetSnapshot(ctx, id, 1); err == nil {
		return s.Get(ctx, OperationReference{id, 1, existing.SnapshotDigest})
	}
	journal, err := s.Commands.Journals.Load(ctx, preflightID)
	if err != nil {
		return OperationView{}, err
	}
	now := s.Commands.now().UTC()
	prepared, err := decodeBatchPreflight(journal, now)
	if err != nil {
		return OperationView{}, err
	}
	if err := s.check(prepared.AdvertiserID, contracts.QueryModePlan); err != nil {
		return OperationView{}, err
	}
	for _, g := range prepared.Groups {
		if !g.SubmitEligible {
			return OperationView{}, ErrOperationUnsupported
		}
	}
	lease, scoped, err := s.Commands.readLease(ctx, prepared.AdvertiserID, prepared.AuthAccountID)
	if err != nil {
		return OperationView{}, err
	}
	writes, err := s.preview(scoped, prepared, lease.AccessToken, now, id)
	if err != nil {
		return OperationView{}, err
	}
	if len(writes) == 0 || len(writes) > 100 {
		return OperationView{}, ErrOperationUnsupported
	}
	payload := operationPayload{Contract: operationKind, PreflightID: preflightID, Preflight: prepared, AuthorizationID: lease.AuthorizationID, Writes: writes}
	for _, write := range writes {
		item, err := confirmationItem(write, prepared.Groups[write.GroupID])
		if err != nil {
			return OperationView{}, err
		}
		payload.Items = append(payload.Items, item)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return OperationView{}, err
	}
	digest, err := ops.Digest(encoded)
	if err != nil {
		return OperationView{}, err
	}
	expires, err := time.Parse(time.RFC3339Nano, prepared.ExpiresAt)
	if err != nil {
		return OperationView{}, err
	}
	snapshot := ops.Snapshot{OperationID: id, Version: 1, Kind: operationKind, Channel: "qianchuan", SubjectID: prepared.AdvertiserID, RuntimeVersion: s.RuntimeVersion, SchemaVersion: ops.SchemaVersion, InputDigest: "sha256:" + prepared.InputDigest, SnapshotDigest: digest, CreatedAt: now, ExpiresAt: expires, Payload: encoded}
	if _, err = ops.NewConfirmationSummary(snapshot, expires); err != nil {
		return OperationView{}, err
	}
	if err = s.Core.Snapshots.SavePreflight(ctx, snapshot); err != nil {
		return OperationView{}, err
	}
	return s.Get(ctx, OperationReference{id, 1, digest})
}

func (s OperationService) load(ctx context.Context, ref OperationReference, mode contracts.QueryExecutionMode) (ops.Snapshot, operationPayload, error) {
	if err := s.check("", mode); err != nil {
		return ops.Snapshot{}, operationPayload{}, err
	}
	if s.Core.Snapshots == nil || s.Core.Repository == nil || s.Recovery == nil {
		return ops.Snapshot{}, operationPayload{}, ErrOperationUnsupported
	}
	snapshot, err := s.Core.Snapshots.GetSnapshot(ctx, ref.OperationID, ref.Version)
	if err != nil {
		return snapshot, operationPayload{}, err
	}
	if snapshot.Kind != operationKind || snapshot.Channel != "qianchuan" || snapshot.SnapshotDigest != ref.SnapshotDigest {
		return snapshot, operationPayload{}, ErrOperationChanged
	}
	if err := s.check(snapshot.SubjectID, mode); err != nil {
		return snapshot, operationPayload{}, err
	}
	var p operationPayload
	decoder := json.NewDecoder(bytes.NewReader(snapshot.Payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&p) != nil || p.Contract != operationKind || len(p.Items) != len(p.Writes) || len(p.Writes) == 0 || p.Preflight.AdvertiserID != snapshot.SubjectID {
		return snapshot, p, ErrOperationUnsupported
	}
	for i, w := range p.Writes {
		digest, err := ops.Digest(w.Payload)
		if err != nil || digest != p.Items[i].PayloadDigest || w.ItemID != p.Items[i].ItemID || w.GroupID != p.Items[i].GroupID || w.Action != p.Items[i].Action || w.TargetID != p.Items[i].TargetID {
			return snapshot, p, ErrOperationChanged
		}
	}
	return snapshot, p, nil
}

func (s OperationService) Get(ctx context.Context, ref OperationReference) (OperationView, error) {
	snapshot, p, err := s.load(ctx, ref, contracts.QueryModeRead)
	if err != nil {
		return OperationView{}, err
	}
	state, err := s.Core.Repository.GetOperationState(ctx, ref.OperationID, ref.Version)
	if err != nil {
		return OperationView{}, err
	}
	summary, err := ops.NewConfirmationSummary(snapshot, snapshot.ExpiresAt)
	if err != nil {
		return OperationView{}, err
	}
	text, err := summary.ReviewText()
	if err != nil {
		return OperationView{}, err
	}
	records, err := s.Recovery.ReadDispatches(ctx, ref.OperationID, ref.Version)
	if err != nil {
		return OperationView{}, err
	}
	byID := map[string]ops.DispatchRecord{}
	for _, r := range records {
		byID[r.ItemID] = r
	}
	result := OperationView{OperationReference: ref, Status: state.Status, AdvertiserID: snapshot.SubjectID, ExpiresAt: snapshot.ExpiresAt, Summary: text, Items: []OperationItemView{}}
	for _, w := range p.Writes {
		r := byID[w.ItemID]
		target := w.TargetID
		if r.ObjectID != "" {
			target = r.ObjectID
		}
		result.Items = append(result.Items, OperationItemView{w.ItemID, w.GroupID, w.Action, target, r.State, r.Applied})
	}
	return result, nil
}

func (s OperationService) Confirm(ctx context.Context, ref OperationReference) (applicationops.ConfirmationResult, error) {
	snapshot, _, err := s.load(ctx, ref, contracts.QueryModeExecute)
	if err != nil {
		return applicationops.ConfirmationResult{}, err
	}
	if snapshot.RuntimeVersion != s.RuntimeVersion {
		return applicationops.ConfirmationResult{}, ErrOperationChanged
	}
	return s.Core.Confirm(ctx, ref.OperationID, ref.Version, ref.SnapshotDigest)
}

func (s OperationService) preview(ctx context.Context, p preparedBatchSnapshot, token string, at time.Time, id string) ([]operationWrite, error) {
	date, err := ShanghaiBusinessDate(s.Commands.now())
	if err != nil || date != p.BusinessDate {
		return nil, ErrBatchPreflightExpired
	}
	if err := s.verifyOperationWorks(ctx, p, token); err != nil {
		return nil, err
	}
	config, err := s.Commands.Config.Read(ctx)
	if err != nil {
		return nil, err
	}
	template, err := domaintemplates.ExportQianchuanPlanPayload(config, domaintemplates.QianchuanTemplateProduct, p.TemplateID, "")
	if err != nil || !template.Active {
		return nil, ErrOperationChanged
	}
	current := BatchRequest{AdvertiserID: template.AdvertiserID, TemplateID: template.TemplateID, TemplateName: template.DisplayName, ProductName: template.ProductName, ProductShortName: template.ProductShortName, PlanNameTemplate: template.PlanNameTemplate, TemplatePayload: template.Payload}
	if batchTemplateDigest(current) != p.TemplateDigest {
		return nil, ErrOperationChanged
	}
	request, err := p.batchRequest(current, p.AuthAccountID, nil)
	if err != nil {
		return nil, err
	}
	request.Submit = false
	request.PreparedGroups = nil
	request.IncludePayloads = true
	request.ReadAccessToken = token
	batch := s.Commands.Batch
	batch.Now = func() time.Time { return at.In(time.FixedZone("Asia/Shanghai", 8*60*60)) }
	result, err := batch.Execute(ctx, request)
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 || len(result.Results) != len(p.Groups) {
		return nil, ErrOperationChanged
	}
	writes := []operationWrite{}
	for _, group := range result.Results {
		expected := p.Groups[group.GroupID]
		action := group.Status
		if action == "already_present" {
			action = "noop"
		}
		if action != expected.ExpectedAction || group.AdID != expected.ExpectedAdID || group.BindingDigest != expected.BindingDigest || !sameStringSet(group.AlreadyPresent, expected.ExpectedPresentItemIDs) {
			return nil, ErrOperationChanged
		}
		if action == "noop" {
			continue
		}
		if action == "would_create" && len(group.InputItemIDs) > BatchMaterialLimit {
			return nil, ErrOperationUnsupported
		}
		actualIDs := []string{}
		for index, w := range group.Writes {
			action := "append"
			target := group.AdID
			name := group.PlanName
			if w.Endpoint == CreateEndpoint {
				action = "create"
				target = "new"
				identity := sha256.Sum256([]byte(id + "\x00" + group.GroupID))
				name = weightedTruncate(name, 80) + "-ow-" + hex.EncodeToString(identity[:6])
				obj, err := decodeCreatePayload(w.Payload)
				if err != nil {
					return nil, err
				}
				obj["name"] = name
				w.Payload, err = json.Marshal(obj)
				if err != nil {
					return nil, err
				}
			} else if w.Endpoint != AddMaterialsEndpoint {
				return nil, ErrOperationUnsupported
			}
			payload, err := canonicalJSON(w.Payload)
			if err != nil {
				return nil, err
			}
			actualIDs = append(actualIDs, w.ItemIDs...)
			writes = append(writes, operationWrite{ItemID: fmt.Sprintf("%s_%d", group.GroupID, index), GroupID: group.GroupID, Action: action, TargetID: target, PlanName: name, ItemIDs: sortedUniqueStrings(w.ItemIDs), Payload: payload})
		}
		if !sameStringSet(actualIDs, expected.ExpectedWriteItemIDs) {
			return nil, ErrOperationChanged
		}
	}
	sort.Slice(writes, func(i, j int) bool { return writes[i].ItemID < writes[j].ItemID })
	return writes, nil
}

func confirmationItem(w operationWrite, g PreparedGroup) (ops.ConfirmationItem, error) {
	digest, err := ops.Digest(w.Payload)
	if err != nil {
		return ops.ConfirmationItem{}, err
	}
	expectedBytes, err := json.Marshal(g)
	if err != nil {
		return ops.ConfirmationItem{}, err
	}
	expected, err := ops.Digest(expectedBytes)
	if err != nil {
		return ops.ConfirmationItem{}, err
	}
	count := len(w.ItemIDs)
	changes := []ops.ConfirmationChange{{Field: "计划", After: w.PlanName}, {Field: "达人 UID", After: g.Identity.CreatorID}, {Field: "商品 ID", After: strings.Join(g.Identity.ProductIDs, ", ")}}
	for i := 0; i < len(w.ItemIDs); i += 20 {
		end := i + 20
		if end > len(w.ItemIDs) {
			end = len(w.ItemIDs)
		}
		changes = append(changes, ops.ConfirmationChange{Field: fmt.Sprintf("新增作品 ID %d", i/20+1), After: strings.Join(w.ItemIDs[i:end], ", ")})
	}
	var object map[string]any
	decoder := json.NewDecoder(bytes.NewReader(w.Payload))
	decoder.UseNumber()
	if err := decoder.Decode(&object); err != nil {
		return ops.ConfirmationItem{}, err
	}
	// Every non-material payload field is visible. Unknown or oversized
	// settings fail summary validation instead of being silently omitted.
	var appendFields func(string, any)
	appendFields = func(prefix string, value any) {
		if fields, ok := value.(map[string]any); ok {
			keys := make([]string, 0, len(fields))
			for key := range fields {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				path := key
				if prefix != "" {
					path = prefix + "." + key
				}
				appendFields(path, fields[key])
			}
			return
		}
		encoded, _ := json.Marshal(value)
		label := prefix
		if prefix == "delivery_setting.budget" {
			label = "预算（budget 接口原值）"
		}
		if prefix == "delivery_setting.roi2_goal" {
			label = "目标 ROI（倍）"
		}
		changes = append(changes, ops.ConfirmationChange{Field: label, After: string(encoded)})
	}
	for _, key := range []string{"name", "aweme_id", "product_ids", "advertiser_id", "multi_product_creative_list", "ad_id"} {
		delete(object, key)
	}
	appendFields("", object)
	return ops.ConfirmationItem{ItemID: w.ItemID, GroupID: w.GroupID, Action: w.Action, TargetID: w.TargetID, PayloadDigest: digest, ExpectedDigest: expected, Changes: changes, MaterialCount: &count}, nil
}

func (s OperationService) verifyOperationWorks(ctx context.Context, p preparedBatchSnapshot, token string) error {
	for _, groupID := range sortedPreparedGroupIDs(p.Groups) {
		g := p.Groups[groupID]
		inputs := make([]WorkInput, 0, len(g.Works))
		expected := map[string]VerifiedWork{}
		for _, w := range g.Works {
			inputs = append(inputs, WorkInput{InputIndex: w.InputIndex, AwemeItemID: w.AwemeItemID, CreatorName: w.CreatorName, PlanType: w.PlanType, Business: w.Business, OwnerHint: &OwnerHint{AwemeID: w.Creator.AwemeID, AwemeShowID: w.Creator.VisibleID}})
			expected[w.AwemeItemID] = w
		}
		verified, err := (WorkVerifier{Reader: s.Commands.Batch.Reader}).Verify(ctx, WorkVerificationRequest{AdvertiserID: p.AdvertiserID, AccessToken: token, ProductIDs: g.Identity.ProductIDs, Works: inputs})
		if err != nil {
			return err
		}
		if len(verified.QueryFailures) > 0 || len(verified.Skipped) > 0 || len(verified.Matched) != len(expected) {
			return ErrOperationChanged
		}
		for _, w := range verified.Matched {
			old, ok := expected[w.AwemeItemID]
			if !ok || w.Creator.AwemeID != old.Creator.AwemeID || w.Material.ImageMode != old.Material.ImageMode || !sameStringSet(w.MatchedProductIDs, old.MatchedProductIDs) {
				return ErrOperationChanged
			}
		}
	}
	return nil
}

func operationOwner() (string, error) {
	var nonce [16]byte
	_, err := rand.Read(nonce[:])
	return "exec_" + hex.EncodeToString(nonce[:]), err
}

func (s OperationService) Execute(ctx context.Context, ref OperationReference, approvalID string) (OperationView, error) {
	snapshot, p, err := s.load(ctx, ref, contracts.QueryModeExecute)
	if err != nil {
		return OperationView{}, err
	}
	if snapshot.RuntimeVersion != s.RuntimeVersion || !s.Commands.now().Before(snapshot.ExpiresAt) {
		return OperationView{}, ErrOperationChanged
	}
	token, scoped, err := s.Commands.readLease(ctx, snapshot.SubjectID, p.Preflight.AuthAccountID)
	if err != nil {
		return OperationView{}, err
	}
	if token.AuthorizationID != p.AuthorizationID {
		return OperationView{}, ErrOperationChanged
	}
	scope := domainplans.WriteScope{Channel: domainplans.ChannelQianchuan, AdvertiserID: snapshot.SubjectID, LockFamily: domainplans.LockQianchuanWorks}
	err = sharedplans.WithAdvertiserLock(scoped, s.Commands.Locks, scope, func(locked context.Context) error {
		writes, err := s.preview(locked, p.Preflight, token.AccessToken, snapshot.CreatedAt, snapshot.OperationID)
		if err != nil {
			return err
		}
		before, _ := json.Marshal(p.Writes)
		after, _ := json.Marshal(writes)
		if !bytes.Equal(before, after) {
			return ErrOperationChanged
		}
		owner, err := operationOwner()
		if err != nil {
			return err
		}
		claimed, err := s.Core.Execute(locked, applicationops.ExecuteRequest{OperationID: ref.OperationID, Version: ref.Version, SnapshotDigest: ref.SnapshotDigest, ApprovalID: approvalID, OwnerID: owner, TTL: executionTTL})
		if err != nil {
			return err
		}
		lease := claimed.Lease
		locked, err = sharedplans.WithVerifiedOperation(locked, lease)
		if err != nil {
			return err
		}
		guard := s.Commands.Batch.Guard
		guard.Credentials = commandLeaseCredentials{lease: token, advertiserID: snapshot.SubjectID}
		_, err = guard.Execute(locked, sharedplans.GuardedMutation{Scope: scope, AuthAccountID: p.Preflight.AuthAccountID, Submit: true, Validate: func() error {
			if s.Commands.Batch.Writer == nil || s.Commands.Batch.Bindings == nil {
				return ErrOperationUnsupported
			}
			return nil
		}}, func(writeContext context.Context, e sharedplans.MutationExecution) (any, error) {
			for i, w := range p.Writes {
				if err := writeContext.Err(); err != nil {
					return nil, err
				}
				lease, err = s.Core.Repository.RenewExecutionLease(writeContext, lease, s.Commands.now(), executionTTL)
				if err != nil {
					return nil, err
				}
				item := p.Items[i]
				attempt, err := s.Core.BeginDispatch(writeContext, ops.DispatchIntentRequest{OperationID: ref.OperationID, Version: ref.Version, ItemID: w.ItemID, GroupID: w.GroupID, Action: w.Action, TargetID: w.TargetID, PayloadDigest: item.PayloadDigest, ExpectedDigest: item.ExpectedDigest, OwnerID: lease.OwnerID, FencingEpoch: lease.FencingEpoch})
				if err != nil {
					return nil, err
				}
				receipt := e.Dispatcher.Dispatch(writeContext, e.Capability, attempt.DispatchKey, func(call context.Context) (any, error) {
					if w.Action == "create" {
						return s.Commands.Batch.Writer.CreatePlan(call, port.CreatePlanRequest{AdvertiserID: snapshot.SubjectID, AccessToken: e.AccessToken, Payload: w.Payload})
					}
					return s.Commands.Batch.Writer.AddMaterials(call, port.MaterialWriteRequest{AdvertiserID: snapshot.SubjectID, AccessToken: e.AccessToken, AdID: w.TargetID, Payload: w.Payload})
				})
				written, _ := receipt.Value.(port.WriteResult)
				state := ops.DispatchState(receipt.State)
				if state == ops.DispatchAcknowledged && (receipt.Error != nil || len(written.RowErrors) > 0 || written.RequestID == "" || w.Action == "create" && !validPositiveID(written.ObjectID)) {
					state = ops.DispatchUnknown
				}
				cleanup, cancel := context.WithTimeout(context.WithoutCancel(writeContext), 5*time.Second)
				err = s.Core.CompleteDispatch(cleanup, ops.DispatchOutcome{OperationID: ref.OperationID, Version: ref.Version, ItemID: w.ItemID, DispatchKey: attempt.DispatchKey, OwnerID: lease.OwnerID, FencingEpoch: lease.FencingEpoch, State: state, OfficialRequestID: written.RequestID, ObjectID: written.ObjectID})
				cancel()
				if err != nil {
					return nil, err
				}
				if state == ops.DispatchNotSent {
					return nil, receipt.Error
				}
				if err = s.readback(writeContext, snapshot, p, w, written.ObjectID, e.AccessToken, lease); err != nil {
					return nil, err
				}
				if state == ops.DispatchUnknown {
					return nil, nil
				}
			}
			return nil, nil
		})
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(locked), 5*time.Second)
		defer cancel()
		_, finishErr := s.Recovery.FinishExecution(cleanup, lease, s.Commands.now())
		return errors.Join(err, finishErr)
	})
	view, readErr := s.Get(ctx, ref)
	return view, errors.Join(err, readErr)
}
