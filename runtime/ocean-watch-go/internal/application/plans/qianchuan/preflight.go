package qianchuan

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	domainplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/plans"
)

const (
	batchPreflightKind          = "qianchuan_batch_preflight"
	batchPreflightSchemaVersion = 2
	batchPreflightLifetime      = 30 * time.Minute
)

var (
	ErrBatchPreflightNotFound       = errors.New("Qianchuan batch preflight was not found")
	ErrBatchPreflightExpired        = errors.New("Qianchuan batch preflight has expired")
	ErrBatchPreflightInvalid        = errors.New("Qianchuan batch preflight is invalid")
	ErrBatchPreflightSchemaObsolete = errors.New("PREFLIGHT_SCHEMA_OBSOLETE")
)

type BatchPreflightDecision struct {
	GroupID        string `json:"group_id"`
	CreatorID      string `json:"creator_id"`
	PlanType       string `json:"plan_type"`
	Business       string `json:"business"`
	Action         string `json:"action"`
	ExistingPlanID string `json:"existing_plan_id,omitempty"`
}

type BatchPreflightSummary struct {
	SchemaVersion    int                      `json:"schema_version"`
	PreflightID      string                   `json:"preflight_id"`
	CreatedAt        string                   `json:"created_at"`
	ExpiresAt        string                   `json:"expires_at"`
	AdvertiserID     string                   `json:"advertiser_id"`
	TemplateID       string                   `json:"template_id"`
	TemplateName     string                   `json:"template_name"`
	ProductName      string                   `json:"product_name"`
	ProductShortName string                   `json:"product_short_name"`
	ProductIDs       []string                 `json:"product_ids"`
	BusinessDate     string                   `json:"business_date,omitempty"`
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
	decoded, err := decodeBatchPreflightForRead(journal, service.now())
	if err != nil {
		if errors.Is(err, ErrBatchPreflightExpired) {
			return BatchPreflightSummary{}, err
		}
		return BatchPreflightSummary{}, fmt.Errorf("%w: %v", ErrBatchPreflightInvalid, err)
	}
	return decoded.summary(preflightID), nil
}

type legacyPreparedBatchSnapshot struct {
	SchemaVersion    int                                    `json:"schema_version"`
	CreatedAt        string                                 `json:"created_at"`
	ExpiresAt        string                                 `json:"expires_at"`
	TemplateDigest   string                                 `json:"template_digest"`
	AdvertiserID     string                                 `json:"advertiser_id"`
	AuthAccountID    string                                 `json:"auth_account_id,omitempty"`
	TemplateID       string                                 `json:"template_id"`
	TemplateName     string                                 `json:"template_name"`
	ProductName      string                                 `json:"product_name"`
	ProductShortName string                                 `json:"product_short_name"`
	PlanNameTemplate string                                 `json:"plan_name_template"`
	PlanType         string                                 `json:"plan_type,omitempty"`
	Business         string                                 `json:"business,omitempty"`
	TemplatePayload  json.RawMessage                        `json:"template_payload"`
	IncludePayloads  bool                                   `json:"include_payloads,omitempty"`
	Works            []VerifiedWork                         `json:"works"`
	Skipped          []SkippedWork                          `json:"skipped"`
	QueryFailures    []WorkQueryFailure                     `json:"query_failures"`
	Expected         map[string]legacyBatchExpectedDecision `json:"expected"`
}

type legacyBatchExpectedDecision struct {
	CreatorID string `json:"creator_id"`
	PlanType  string `json:"plan_type"`
	Business  string `json:"business"`
	Action    string `json:"action"`
	AdID      string `json:"ad_id,omitempty"`
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

func legacyBatchSnapshotFingerprint(snapshot legacyPreparedBatchSnapshot) (string, error) {
	canonicalPayload, err := canonicalJSON(snapshot.TemplatePayload)
	if err != nil {
		return "", err
	}
	snapshot.TemplatePayload = canonicalPayload
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func canonicalJSON(payload json.RawMessage) (json.RawMessage, error) {
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return nil, errors.New("JSON contains multiple values")
	} else if !errors.Is(err, io.EOF) {
		return nil, err
	}
	return json.Marshal(value)
}

func decodeLegacyBatchPreflight(journal domainplans.Journal, now time.Time) (legacyPreparedBatchSnapshot, error) {
	if journal.SchemaVersion != 1 {
		return legacyPreparedBatchSnapshot{}, errors.New("Qianchuan batch preflight journal schema is unsupported")
	}
	var kind string
	if err := json.Unmarshal(journal.Extra["kind"], &kind); err != nil || kind != batchPreflightKind {
		return legacyPreparedBatchSnapshot{}, errors.New("operation journal is not a Qianchuan batch preflight")
	}
	decoder := json.NewDecoder(strings.NewReader(string(journal.Extra["snapshot"])))
	decoder.DisallowUnknownFields()
	var snapshot legacyPreparedBatchSnapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return legacyPreparedBatchSnapshot{}, errors.New("Qianchuan batch preflight snapshot is invalid")
	}
	if snapshot.SchemaVersion != 1 || len(snapshot.Expected) == 0 || len(snapshot.Works) == 0 {
		return legacyPreparedBatchSnapshot{}, errors.New("Qianchuan batch preflight snapshot schema is unsupported")
	}
	if len(journal.Jobs) != len(snapshot.Expected) {
		return legacyPreparedBatchSnapshot{}, errors.New("Qianchuan batch preflight jobs do not match the snapshot")
	}
	for groupID, expected := range snapshot.Expected {
		job, exists := journal.Jobs[groupID]
		if !exists || job.Status != "prepared" || job.AdvertiserID != snapshot.AdvertiserID || job.AdID != expected.AdID {
			return legacyPreparedBatchSnapshot{}, errors.New("Qianchuan batch preflight jobs do not match the snapshot")
		}
	}
	fingerprint, err := legacyBatchSnapshotFingerprint(snapshot)
	if err != nil || fingerprint != journal.Fingerprint {
		return legacyPreparedBatchSnapshot{}, errors.New("Qianchuan batch preflight fingerprint does not match")
	}
	createdAt, createdErr := time.Parse(time.RFC3339Nano, snapshot.CreatedAt)
	expiresAt, expiresErr := time.Parse(time.RFC3339Nano, snapshot.ExpiresAt)
	journalCreatedAt, journalErr := time.Parse(time.RFC3339Nano, journal.CreatedAt)
	if createdErr != nil || expiresErr != nil || journalErr != nil || !createdAt.Equal(journalCreatedAt) ||
		!expiresAt.After(createdAt) {
		return legacyPreparedBatchSnapshot{}, errors.New("Qianchuan batch preflight timestamps are invalid")
	}
	expectedExpiry, expiryErr := batchPreflightExpiry(createdAt)
	if expiryErr != nil || !expiresAt.Equal(expectedExpiry) {
		return legacyPreparedBatchSnapshot{}, errors.New("Qianchuan batch preflight timestamps are invalid")
	}
	if now.IsZero() {
		now = time.Now()
	}
	if !now.UTC().Before(expiresAt) {
		return legacyPreparedBatchSnapshot{}, fmt.Errorf("%w; run preflight again", ErrBatchPreflightExpired)
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
