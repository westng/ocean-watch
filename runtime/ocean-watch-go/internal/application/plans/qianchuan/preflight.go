package qianchuan

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	domainplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/plans"
)

const (
	batchPreflightKind          = "qianchuan_batch_preflight"
	batchPreflightSchemaVersion = 1
	batchPreflightLifetime      = 30 * time.Minute
)

var (
	ErrBatchPreflightNotFound = errors.New("Qianchuan batch preflight was not found")
	ErrBatchPreflightExpired  = errors.New("Qianchuan batch preflight has expired")
	ErrBatchPreflightInvalid  = errors.New("Qianchuan batch preflight is invalid")
)

type BatchPreflightDecision struct {
	CreatorID      string `json:"creator_id"`
	Action         string `json:"action"`
	ExistingPlanID string `json:"existing_plan_id,omitempty"`
}

type BatchPreflightSummary struct {
	PreflightID      string                   `json:"preflight_id"`
	CreatedAt        string                   `json:"created_at"`
	ExpiresAt        string                   `json:"expires_at"`
	AdvertiserID     string                   `json:"advertiser_id"`
	TemplateID       string                   `json:"template_id"`
	TemplateName     string                   `json:"template_name"`
	ProductName      string                   `json:"product_name"`
	ProductShortName string                   `json:"product_short_name"`
	ProductIDs       []string                 `json:"product_ids"`
	EligibleWorks    int                      `json:"eligible_works"`
	SkippedWorks     int                      `json:"skipped_works"`
	Decisions        []BatchPreflightDecision `json:"decisions"`
	ReadyForSubmit   bool                     `json:"ready_for_submit"`
}

func (service CommandService) GetBatchPreflight(
	ctx context.Context,
	preflightID string,
) (BatchPreflightSummary, error) {
	preflightID = strings.TrimSpace(preflightID)
	if !batchPreflightIDPattern.MatchString(preflightID) {
		return BatchPreflightSummary{}, fmt.Errorf("%w: id format is invalid", ErrBatchPreflightInvalid)
	}
	if service.Journals == nil {
		return BatchPreflightSummary{}, fmt.Errorf("%w: journal store is unavailable", ErrBatchPreflightInvalid)
	}
	journal, err := service.Journals.Load(ctx, preflightID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return BatchPreflightSummary{}, fmt.Errorf("%w: %v", ErrBatchPreflightNotFound, err)
		}
		return BatchPreflightSummary{}, fmt.Errorf("load Qianchuan batch preflight: %w", err)
	}
	snapshot, err := decodeBatchPreflight(journal, service.now())
	if err != nil {
		if errors.Is(err, ErrBatchPreflightExpired) {
			return BatchPreflightSummary{}, err
		}
		return BatchPreflightSummary{}, fmt.Errorf("%w: %v", ErrBatchPreflightInvalid, err)
	}
	productSet := map[string]struct{}{}
	for _, work := range snapshot.Works {
		for _, productID := range work.MatchedProductIDs {
			if productID = strings.TrimSpace(productID); productID != "" {
				productSet[productID] = struct{}{}
			}
		}
	}
	productIDs := make([]string, 0, len(productSet))
	for productID := range productSet {
		productIDs = append(productIDs, productID)
	}
	sort.Strings(productIDs)
	creatorIDs := make([]string, 0, len(snapshot.Expected))
	for creatorID := range snapshot.Expected {
		creatorIDs = append(creatorIDs, creatorID)
	}
	sort.Strings(creatorIDs)
	decisions := make([]BatchPreflightDecision, 0, len(creatorIDs))
	for _, creatorID := range creatorIDs {
		expected := snapshot.Expected[creatorID]
		decisions = append(decisions, BatchPreflightDecision{
			CreatorID: creatorID, Action: expected.Action, ExistingPlanID: expected.AdID,
		})
	}
	return BatchPreflightSummary{
		PreflightID: preflightID, CreatedAt: snapshot.CreatedAt, ExpiresAt: snapshot.ExpiresAt,
		AdvertiserID: snapshot.AdvertiserID, TemplateID: snapshot.TemplateID, TemplateName: snapshot.TemplateName,
		ProductName: snapshot.ProductName, ProductShortName: snapshot.ProductShortName, ProductIDs: productIDs,
		EligibleWorks: len(snapshot.Works), SkippedWorks: len(snapshot.Skipped), Decisions: decisions,
		ReadyForSubmit: true,
	}, nil
}

type preparedBatchSnapshot struct {
	SchemaVersion    int                              `json:"schema_version"`
	CreatedAt        string                           `json:"created_at"`
	ExpiresAt        string                           `json:"expires_at"`
	TemplateDigest   string                           `json:"template_digest"`
	AdvertiserID     string                           `json:"advertiser_id"`
	AuthAccountID    string                           `json:"auth_account_id,omitempty"`
	TemplateID       string                           `json:"template_id"`
	TemplateName     string                           `json:"template_name"`
	ProductName      string                           `json:"product_name"`
	ProductShortName string                           `json:"product_short_name"`
	PlanNameTemplate string                           `json:"plan_name_template"`
	PlanType         string                           `json:"plan_type,omitempty"`
	Business         string                           `json:"business,omitempty"`
	TemplatePayload  json.RawMessage                  `json:"template_payload"`
	IncludePayloads  bool                             `json:"include_payloads,omitempty"`
	Works            []VerifiedWork                   `json:"works"`
	Skipped          []SkippedWork                    `json:"skipped"`
	QueryFailures    []WorkQueryFailure               `json:"query_failures"`
	Expected         map[string]batchExpectedDecision `json:"expected"`
}

type batchExpectedDecision struct {
	Action string `json:"action"`
	AdID   string `json:"ad_id,omitempty"`
}

func prepareBatchSnapshot(request BatchRequest, result BatchResult, now time.Time) (preparedBatchSnapshot, bool, error) {
	expected := map[string]batchExpectedDecision{}
	eligibleCreators := map[string]struct{}{}
	for _, group := range result.Results {
		decision := batchExpectedDecision{}
		switch group.Status {
		case "would_create":
			decision.Action = "create"
		case "would_append":
			decision.Action, decision.AdID = "append", strings.TrimSpace(group.AdID)
		default:
			continue
		}
		if !validPositiveID(group.AwemeID) || decision.Action == "append" && !validPositiveID(decision.AdID) {
			return preparedBatchSnapshot{}, false, errors.New("Qianchuan preflight contains an invalid submit decision")
		}
		expected[group.AwemeID] = decision
		eligibleCreators[group.AwemeID] = struct{}{}
	}
	if len(expected) == 0 {
		return preparedBatchSnapshot{}, false, nil
	}
	works := make([]VerifiedWork, 0, len(request.Works))
	for _, work := range request.Works {
		if _, eligible := eligibleCreators[work.Creator.AwemeID]; eligible {
			works = append(works, preparedVerifiedWork(work))
		}
	}
	if len(works) == 0 {
		return preparedBatchSnapshot{}, false, errors.New("Qianchuan preflight has no eligible verified works")
	}
	skipped := make([]SkippedWork, len(request.Skipped))
	copy(skipped, request.Skipped)
	for index := range skipped {
		skipped[index].InputURL = ""
	}
	if now.IsZero() {
		now = time.Now()
	}
	now = now.UTC()
	expiresAt, err := batchPreflightExpiry(now)
	if err != nil {
		return preparedBatchSnapshot{}, false, err
	}
	snapshot := preparedBatchSnapshot{
		SchemaVersion: batchPreflightSchemaVersion,
		CreatedAt:     now.Format(time.RFC3339Nano), ExpiresAt: expiresAt.Format(time.RFC3339Nano),
		TemplateDigest: batchTemplateDigest(request), AdvertiserID: request.AdvertiserID,
		AuthAccountID: request.AuthAccountID, TemplateID: request.TemplateID,
		TemplateName: request.TemplateName, ProductName: request.ProductName,
		ProductShortName: request.ProductShortName, PlanNameTemplate: request.PlanNameTemplate,
		PlanType: request.PlanType, Business: request.Business,
		TemplatePayload: append(json.RawMessage(nil), request.TemplatePayload...),
		IncludePayloads: request.IncludePayloads, Works: works,
		Skipped: skipped, QueryFailures: []WorkQueryFailure{}, Expected: expected,
	}
	return snapshot, true, nil
}

func preparedVerifiedWork(work VerifiedWork) VerifiedWork {
	work.InputURL = ""
	work.Creator.Avatar = ""
	work.Creator.AuthTypes = nil
	work.Creator.HasAuthorized = nil
	work.Creator.ProductPromotionDisabled = nil
	work.Creator.ProductDisableReasons = nil
	work.Creator.ProductPromotionApply = ""
	work.Creator.CanControlPromotion = nil
	work.Creator.CanApplyPromotion = nil
	work.Creator.HasShopPermission = nil
	work.Creator.HasLivePermission = nil
	work.Material.VideoID = ""
	work.Material.VideoCoverURL = ""
	work.Material.URL = ""
	work.Material.Width = nil
	work.Material.Height = nil
	work.Material.Duration = nil
	work.Material.IsRecommend = nil
	work.Material.ViewCount = nil
	work.Material.LikeCount = nil
	work.Material.ShareCount = nil
	work.Material.CommentCount = nil
	work.Material.IsAICreated = nil
	work.Material.MatchedProductIDs = nil
	return work
}

func (snapshot preparedBatchSnapshot) batchRequest(authAccountID string, pool *ReadPool) BatchRequest {
	if strings.TrimSpace(authAccountID) == "" {
		authAccountID = snapshot.AuthAccountID
	}
	return BatchRequest{
		AdvertiserID: snapshot.AdvertiserID, AuthAccountID: strings.TrimSpace(authAccountID), Submit: true,
		TemplateID: snapshot.TemplateID, TemplateName: snapshot.TemplateName,
		ProductName: snapshot.ProductName, ProductShortName: snapshot.ProductShortName,
		PlanNameTemplate: snapshot.PlanNameTemplate, PlanType: snapshot.PlanType, Business: snapshot.Business,
		TemplatePayload: append(json.RawMessage(nil), snapshot.TemplatePayload...),
		IncludePayloads: snapshot.IncludePayloads, Works: append([]VerifiedWork(nil), snapshot.Works...),
		Skipped:       append([]SkippedWork(nil), snapshot.Skipped...),
		QueryFailures: append([]WorkQueryFailure(nil), snapshot.QueryFailures...),
		ReadPool:      pool, Expected: cloneBatchExpected(snapshot.Expected),
	}
}

func batchTemplateDigest(request BatchRequest) string {
	canonicalPayload := append(json.RawMessage(nil), request.TemplatePayload...)
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(request.TemplatePayload)))
	decoder.UseNumber()
	if decoder.Decode(&value) == nil {
		if normalized, err := json.Marshal(value); err == nil {
			canonicalPayload = normalized
		}
	}
	payload, _ := json.Marshal(struct {
		TemplateID       string          `json:"template_id"`
		TemplateName     string          `json:"template_name"`
		AdvertiserID     string          `json:"advertiser_id"`
		ProductName      string          `json:"product_name"`
		ProductShortName string          `json:"product_short_name"`
		PlanNameTemplate string          `json:"plan_name_template"`
		TemplatePayload  json.RawMessage `json:"template_payload"`
	}{
		TemplateID: request.TemplateID, TemplateName: request.TemplateName,
		AdvertiserID: request.AdvertiserID, ProductName: request.ProductName,
		ProductShortName: request.ProductShortName, PlanNameTemplate: request.PlanNameTemplate,
		TemplatePayload: canonicalPayload,
	})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func batchSnapshotFingerprint(snapshot preparedBatchSnapshot) (string, error) {
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func batchPreflightJournal(snapshot preparedBatchSnapshot, now time.Time) (domainplans.Journal, error) {
	fingerprint, err := batchSnapshotFingerprint(snapshot)
	if err != nil {
		return domainplans.Journal{}, err
	}
	jobs := make(map[string]domainplans.JournalJob, len(snapshot.Expected))
	for awemeID, expected := range snapshot.Expected {
		extra, marshalErr := json.Marshal(expected)
		if marshalErr != nil {
			return domainplans.Journal{}, marshalErr
		}
		jobs[awemeID] = domainplans.JournalJob{
			Status: "prepared", AdvertiserID: snapshot.AdvertiserID,
			AdID: expected.AdID, Extra: map[string]json.RawMessage{"expected": extra},
		}
	}
	journal, err := domainplans.NewJournal(fingerprint, jobs, now)
	if err != nil {
		return domainplans.Journal{}, err
	}
	kind, _ := json.Marshal(batchPreflightKind)
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return domainplans.Journal{}, err
	}
	journal.Extra = map[string]json.RawMessage{"kind": kind, "snapshot": payload}
	return journal, nil
}

func decodeBatchPreflight(journal domainplans.Journal, now time.Time) (preparedBatchSnapshot, error) {
	if journal.SchemaVersion != 1 {
		return preparedBatchSnapshot{}, errors.New("Qianchuan batch preflight journal schema is unsupported")
	}
	var kind string
	if err := json.Unmarshal(journal.Extra["kind"], &kind); err != nil || kind != batchPreflightKind {
		return preparedBatchSnapshot{}, errors.New("operation journal is not a Qianchuan batch preflight")
	}
	decoder := json.NewDecoder(strings.NewReader(string(journal.Extra["snapshot"])))
	decoder.DisallowUnknownFields()
	var snapshot preparedBatchSnapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return preparedBatchSnapshot{}, errors.New("Qianchuan batch preflight snapshot is invalid")
	}
	if snapshot.SchemaVersion != batchPreflightSchemaVersion || len(snapshot.Expected) == 0 || len(snapshot.Works) == 0 {
		return preparedBatchSnapshot{}, errors.New("Qianchuan batch preflight snapshot schema is unsupported")
	}
	if len(journal.Jobs) != len(snapshot.Expected) {
		return preparedBatchSnapshot{}, errors.New("Qianchuan batch preflight jobs do not match the snapshot")
	}
	for awemeID, expected := range snapshot.Expected {
		job, exists := journal.Jobs[awemeID]
		if !exists || job.Status != "prepared" || job.AdvertiserID != snapshot.AdvertiserID || job.AdID != expected.AdID {
			return preparedBatchSnapshot{}, errors.New("Qianchuan batch preflight jobs do not match the snapshot")
		}
	}
	fingerprint, err := batchSnapshotFingerprint(snapshot)
	if err != nil || fingerprint != journal.Fingerprint {
		return preparedBatchSnapshot{}, errors.New("Qianchuan batch preflight fingerprint does not match")
	}
	createdAt, createdErr := time.Parse(time.RFC3339Nano, snapshot.CreatedAt)
	expiresAt, expiresErr := time.Parse(time.RFC3339Nano, snapshot.ExpiresAt)
	journalCreatedAt, journalErr := time.Parse(time.RFC3339Nano, journal.CreatedAt)
	if createdErr != nil || expiresErr != nil || journalErr != nil || !createdAt.Equal(journalCreatedAt) ||
		!expiresAt.After(createdAt) {
		return preparedBatchSnapshot{}, errors.New("Qianchuan batch preflight timestamps are invalid")
	}
	expectedExpiry, expiryErr := batchPreflightExpiry(createdAt)
	if expiryErr != nil || !expiresAt.Equal(expectedExpiry) {
		return preparedBatchSnapshot{}, errors.New("Qianchuan batch preflight timestamps are invalid")
	}
	if now.IsZero() {
		now = time.Now()
	}
	if !now.UTC().Before(expiresAt) {
		return preparedBatchSnapshot{}, fmt.Errorf("%w; run preflight again", ErrBatchPreflightExpired)
	}
	return snapshot, nil
}

func batchPreflightExpiry(createdAt time.Time) (time.Time, error) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.Time{}, fmt.Errorf("load Asia/Shanghai timezone: %w", err)
	}
	local := createdAt.In(location)
	nextDay := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, location)
	expiresAt := createdAt.Add(batchPreflightLifetime)
	if nextDay.Before(expiresAt) {
		expiresAt = nextDay
	}
	return expiresAt.UTC(), nil
}

func newBatchPreflightID(_ string, now time.Time) (string, error) {
	random := make([]byte, 6)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return "qianchuan-preflight-" + now.UTC().Format("20060102t150405") + "-" + hex.EncodeToString(random), nil
}

func cloneBatchExpected(source map[string]batchExpectedDecision) map[string]batchExpectedDecision {
	result := make(map[string]batchExpectedDecision, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
