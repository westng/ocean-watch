package qianchuan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	domainplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/plans"
	domainqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/qianchuan"
)

type PreparedGroup struct {
	GroupID                string                            `json:"group_id"`
	Identity               domainqianchuan.PlanGroupIdentity `json:"identity"`
	Works                  []VerifiedWork                    `json:"works"`
	ExpectedAction         string                            `json:"expected_action"`
	ExpectedAdID           string                            `json:"expected_ad_id,omitempty"`
	ExpectedPresentItemIDs []string                          `json:"expected_present_item_ids"`
	ExpectedWriteItemIDs   []string                          `json:"expected_write_item_ids"`
	BindingDigest          string                            `json:"binding_digest"`
	SubmitEligible         bool                              `json:"submit_eligible"`
	ErrorCode              string                            `json:"error_code,omitempty"`
}

type preparedBatchSnapshot struct {
	SchemaVersion    int                      `json:"schema_version"`
	CreatedAt        string                   `json:"created_at"`
	ExpiresAt        string                   `json:"expires_at"`
	TemplateDigest   string                   `json:"template_digest"`
	InputDigest      string                   `json:"input_digest"`
	AdvertiserID     string                   `json:"advertiser_id"`
	AuthAccountID    string                   `json:"auth_account_id,omitempty"`
	TemplateID       string                   `json:"template_id"`
	TemplateName     string                   `json:"template_name"`
	ProductName      string                   `json:"product_name"`
	ProductShortName string                   `json:"product_short_name"`
	PlanNameTemplate string                   `json:"plan_name_template"`
	IncludePayloads  bool                     `json:"include_payloads,omitempty"`
	BusinessDate     string                   `json:"business_date"`
	Groups           map[string]PreparedGroup `json:"groups"`
	Skipped          []SkippedWork            `json:"skipped"`
}

type decodedBatchPreflight struct {
	v2     *preparedBatchSnapshot
	legacy *legacyPreparedBatchSnapshot
}

func prepareBatchSnapshot(request BatchRequest, result BatchResult, now time.Time) (preparedBatchSnapshot, bool, error) {
	if now.IsZero() {
		now = time.Now()
	}
	now = now.UTC()
	businessDate := strings.TrimSpace(result.BusinessDate)
	if businessDate == "" {
		var err error
		businessDate, err = ShanghaiBusinessDate(now)
		if err != nil {
			return preparedBatchSnapshot{}, false, err
		}
	}
	if err := ValidateBusinessDate(businessDate); err != nil {
		return preparedBatchSnapshot{}, false, err
	}

	worksByID := make(map[string]VerifiedWork, len(request.Works))
	for _, work := range request.Works {
		if _, duplicate := worksByID[work.AwemeItemID]; duplicate {
			return preparedBatchSnapshot{}, false, errors.New("Qianchuan preflight contains duplicate verified works")
		}
		worksByID[work.AwemeItemID] = work
	}
	absentBindingDigest, err := PlanBindingDigest(nil)
	if err != nil {
		return preparedBatchSnapshot{}, false, err
	}
	groups := make(map[string]PreparedGroup, len(result.Results))
	for _, resultGroup := range result.Results {
		identity, identityErr := domainqianchuan.NewPlanGroupIdentity(
			request.AdvertiserID, request.TemplateID, resultGroup.AwemeID,
			resultGroup.ProductIDs, resultGroup.PlanType, resultGroup.Business,
		)
		if identityErr != nil {
			return preparedBatchSnapshot{}, false, identityErr
		}
		groupID, groupErr := domainqianchuan.GroupID(identity)
		if groupErr != nil || groupID != resultGroup.GroupID {
			return preparedBatchSnapshot{}, false, errors.New("Qianchuan preflight group identity is invalid")
		}
		if _, duplicate := groups[groupID]; duplicate {
			return preparedBatchSnapshot{}, false, errors.New("Qianchuan preflight contains duplicate plan groups")
		}
		groupWorks := make([]VerifiedWork, 0, len(resultGroup.InputItemIDs))
		for _, itemID := range resultGroup.InputItemIDs {
			work, exists := worksByID[itemID]
			if !exists {
				return preparedBatchSnapshot{}, false, errors.New("Qianchuan preflight group references an unknown verified work")
			}
			groupWorks = append(groupWorks, preparedVerifiedWork(work))
		}
		if len(groupWorks) == 0 {
			return preparedBatchSnapshot{}, false, errors.New("Qianchuan preflight group contains no verified works")
		}

		prepared := PreparedGroup{
			GroupID: groupID, Identity: identity, Works: groupWorks,
			ExpectedAction: strings.TrimSpace(resultGroup.Status),
			ExpectedAdID:   strings.TrimSpace(resultGroup.AdID),
			BindingDigest:  strings.TrimSpace(resultGroup.BindingDigest),
			ErrorCode:      strings.TrimSpace(resultGroup.ErrorCode),
		}
		inputIDs := verifiedWorkIDs(groupWorks)
		switch resultGroup.Status {
		case "would_create":
			prepared.SubmitEligible = true
			prepared.ExpectedAdID = ""
			prepared.ExpectedWriteItemIDs = sortedUniqueStrings(inputIDs)
		case "would_append":
			prepared.SubmitEligible = true
			prepared.ExpectedPresentItemIDs = sortedUniqueStrings(resultGroup.AlreadyPresent)
			prepared.ExpectedWriteItemIDs = sortedStringDifference(inputIDs, prepared.ExpectedPresentItemIDs)
		case "already_present":
			prepared.ExpectedAction = "noop"
			prepared.SubmitEligible = true
			prepared.ExpectedPresentItemIDs = sortedUniqueStrings(inputIDs)
		case "legacy_binding_required":
			prepared.ErrorCode = "LEGACY_BINDING_REQUIRED"
		case "binding_drift":
			prepared.ErrorCode = "BINDING_DRIFT"
		default:
			if prepared.ErrorCode == "" {
				prepared.ErrorCode = "GROUP_PREFLIGHT_FAILED"
			}
		}
		if prepared.BindingDigest == "" {
			switch prepared.ExpectedAction {
			case "would_create", "legacy_binding_required":
				prepared.BindingDigest = absentBindingDigest
			default:
				return preparedBatchSnapshot{}, false, errors.New("Qianchuan preflight group has no binding digest")
			}
		}
		groups[groupID] = prepared
	}
	if len(groups) == 0 {
		return preparedBatchSnapshot{}, false, nil
	}
	expiresAt, err := batchPreflightExpiry(now)
	if err != nil {
		return preparedBatchSnapshot{}, false, err
	}
	skipped := append([]SkippedWork(nil), request.Skipped...)
	for index := range skipped {
		skipped[index].InputURL = ""
	}
	snapshot := preparedBatchSnapshot{
		SchemaVersion: batchPreflightSchemaVersion,
		CreatedAt:     now.Format(time.RFC3339Nano), ExpiresAt: expiresAt.Format(time.RFC3339Nano),
		TemplateDigest: batchTemplateDigest(request), AdvertiserID: request.AdvertiserID,
		AuthAccountID: request.AuthAccountID, TemplateID: request.TemplateID,
		TemplateName: request.TemplateName, ProductName: request.ProductName,
		ProductShortName: request.ProductShortName, PlanNameTemplate: request.PlanNameTemplate,
		IncludePayloads: request.IncludePayloads, BusinessDate: businessDate,
		Groups: groups, Skipped: skipped,
	}
	snapshot.InputDigest, err = batchInputDigest(snapshot.Groups)
	if err != nil {
		return preparedBatchSnapshot{}, false, err
	}
	return snapshot, true, nil
}

func (snapshot preparedBatchSnapshot) batchRequest(
	template BatchRequest,
	authAccountID string,
	pool *ReadPool,
) (BatchRequest, error) {
	if strings.TrimSpace(authAccountID) == "" {
		authAccountID = snapshot.AuthAccountID
	}
	works := []VerifiedWork{}
	preparedGroups := map[string]PreparedGroup{}
	groupIDs := sortedPreparedGroupIDs(snapshot.Groups)
	for _, groupID := range groupIDs {
		group := snapshot.Groups[groupID]
		if !group.SubmitEligible {
			continue
		}
		preparedGroups[groupID] = clonePreparedGroup(group)
		works = append(works, group.Works...)
	}
	if len(preparedGroups) == 0 {
		return BatchRequest{}, errors.New("Qianchuan batch preflight has no submit-eligible groups")
	}
	return BatchRequest{
		AdvertiserID: snapshot.AdvertiserID, AuthAccountID: strings.TrimSpace(authAccountID),
		Submit: true, BusinessDate: snapshot.BusinessDate,
		TemplateID: template.TemplateID, TemplateName: template.TemplateName,
		ProductName: template.ProductName, ProductShortName: template.ProductShortName,
		PlanNameTemplate: template.PlanNameTemplate,
		TemplatePayload:  append(json.RawMessage(nil), template.TemplatePayload...),
		IncludePayloads:  snapshot.IncludePayloads, Works: works,
		Skipped: append([]SkippedWork(nil), snapshot.Skipped...), ReadPool: pool,
		PreparedGroups: preparedGroups,
	}, nil
}

func batchInputDigest(groups map[string]PreparedGroup) (string, error) {
	type digestItem struct {
		InputIndex int    `json:"input_index"`
		ItemID     string `json:"item_id"`
		PlanType   string `json:"plan_type"`
		Business   string `json:"business"`
	}
	items := []digestItem{}
	seenIndexes := map[int]struct{}{}
	seenItems := map[string]struct{}{}
	for _, group := range groups {
		for _, work := range group.Works {
			if _, duplicate := seenIndexes[work.InputIndex]; duplicate {
				return "", errors.New("Qianchuan preflight input_index is duplicated")
			}
			if _, duplicate := seenItems[work.AwemeItemID]; duplicate {
				return "", errors.New("Qianchuan preflight item_id is duplicated")
			}
			seenIndexes[work.InputIndex] = struct{}{}
			seenItems[work.AwemeItemID] = struct{}{}
			items = append(items, digestItem{
				InputIndex: work.InputIndex, ItemID: work.AwemeItemID,
				PlanType: group.Identity.PlanType, Business: group.Identity.Business,
			})
		}
	}
	sort.Slice(items, func(left, right int) bool {
		if items[left].InputIndex != items[right].InputIndex {
			return items[left].InputIndex < items[right].InputIndex
		}
		return items[left].ItemID < items[right].ItemID
	})
	payload, err := json.Marshal(items)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
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
	jobs := make(map[string]domainplans.JournalJob, len(snapshot.Groups))
	for groupID, group := range snapshot.Groups {
		extra, marshalErr := json.Marshal(group)
		if marshalErr != nil {
			return domainplans.Journal{}, marshalErr
		}
		jobs[groupID] = domainplans.JournalJob{
			Status: "prepared", AdvertiserID: snapshot.AdvertiserID,
			AdID: group.ExpectedAdID, Extra: map[string]json.RawMessage{"prepared_group": extra},
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
	schemaVersion, err := batchPreflightSnapshotSchema(journal)
	if err != nil {
		return preparedBatchSnapshot{}, err
	}
	if schemaVersion == 1 {
		return preparedBatchSnapshot{}, fmt.Errorf("%w; run preflight again", ErrBatchPreflightSchemaObsolete)
	}
	if schemaVersion != batchPreflightSchemaVersion {
		return preparedBatchSnapshot{}, errors.New("Qianchuan batch preflight snapshot schema is unsupported")
	}
	return decodeBatchPreflightV2(journal, now)
}

func decodeBatchPreflightForRead(journal domainplans.Journal, now time.Time) (decodedBatchPreflight, error) {
	schemaVersion, err := batchPreflightSnapshotSchema(journal)
	if err != nil {
		return decodedBatchPreflight{}, err
	}
	switch schemaVersion {
	case 1:
		snapshot, decodeErr := decodeLegacyBatchPreflight(journal, now)
		return decodedBatchPreflight{legacy: &snapshot}, decodeErr
	case batchPreflightSchemaVersion:
		snapshot, decodeErr := decodeBatchPreflightV2(journal, now)
		return decodedBatchPreflight{v2: &snapshot}, decodeErr
	default:
		return decodedBatchPreflight{}, errors.New("Qianchuan batch preflight snapshot schema is unsupported")
	}
}

func batchPreflightSnapshotSchema(journal domainplans.Journal) (int, error) {
	if journal.SchemaVersion != 1 {
		return 0, errors.New("Qianchuan batch preflight journal schema is unsupported")
	}
	var kind string
	if err := json.Unmarshal(journal.Extra["kind"], &kind); err != nil || kind != batchPreflightKind {
		return 0, errors.New("operation journal is not a Qianchuan batch preflight")
	}
	var header struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal(journal.Extra["snapshot"], &header); err != nil || header.SchemaVersion == 0 {
		return 0, errors.New("Qianchuan batch preflight snapshot is invalid")
	}
	return header.SchemaVersion, nil
}

func decodeBatchPreflightV2(journal domainplans.Journal, now time.Time) (preparedBatchSnapshot, error) {
	decoder := json.NewDecoder(strings.NewReader(string(journal.Extra["snapshot"])))
	decoder.DisallowUnknownFields()
	var snapshot preparedBatchSnapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return preparedBatchSnapshot{}, errors.New("Qianchuan batch preflight snapshot is invalid")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return preparedBatchSnapshot{}, errors.New("Qianchuan batch preflight snapshot is invalid")
	}
	if err := validatePreparedBatchSnapshot(snapshot, journal); err != nil {
		return preparedBatchSnapshot{}, err
	}
	if err := validateBatchPreflightTimes(snapshot.CreatedAt, snapshot.ExpiresAt, snapshot.BusinessDate, journal.CreatedAt, now); err != nil {
		return preparedBatchSnapshot{}, err
	}
	return snapshot, nil
}

func validatePreparedBatchSnapshot(snapshot preparedBatchSnapshot, journal domainplans.Journal) error {
	if snapshot.SchemaVersion != batchPreflightSchemaVersion || len(snapshot.Groups) == 0 ||
		!sha256Hex(snapshot.TemplateDigest) || !sha256Hex(snapshot.InputDigest) ||
		!validPositiveID(snapshot.AdvertiserID) || strings.TrimSpace(snapshot.TemplateID) == "" {
		return errors.New("Qianchuan batch preflight snapshot schema is unsupported")
	}
	if len(journal.Jobs) != len(snapshot.Groups) {
		return errors.New("Qianchuan batch preflight jobs do not match the snapshot")
	}
	for groupID, group := range snapshot.Groups {
		canonicalID, err := domainqianchuan.GroupID(group.Identity)
		if err != nil || canonicalID != groupID || group.GroupID != groupID || len(group.Works) == 0 ||
			!sha256Hex(group.BindingDigest) {
			return errors.New("Qianchuan batch preflight group is invalid")
		}
		job, exists := journal.Jobs[groupID]
		if !exists || job.Status != "prepared" || job.AdvertiserID != snapshot.AdvertiserID || job.AdID != group.ExpectedAdID {
			return errors.New("Qianchuan batch preflight jobs do not match the snapshot")
		}
		if group.SubmitEligible {
			switch group.ExpectedAction {
			case "would_create":
				if group.ExpectedAdID != "" || len(group.ExpectedPresentItemIDs) != 0 {
					return errors.New("Qianchuan batch preflight create group is invalid")
				}
			case "would_append", "noop":
				if !validPositiveID(group.ExpectedAdID) {
					return errors.New("Qianchuan batch preflight existing-plan group is invalid")
				}
			default:
				return errors.New("Qianchuan batch preflight action is unsupported")
			}
			if !preparedItemPartitionMatches(group) {
				return errors.New("Qianchuan batch preflight material difference is invalid")
			}
		}
	}
	inputDigest, err := batchInputDigest(snapshot.Groups)
	if err != nil || inputDigest != snapshot.InputDigest {
		return errors.New("Qianchuan batch preflight input digest does not match")
	}
	fingerprint, err := batchSnapshotFingerprint(snapshot)
	if err != nil || fingerprint != journal.Fingerprint {
		return errors.New("Qianchuan batch preflight fingerprint does not match")
	}
	return nil
}

func preparedItemPartitionMatches(group PreparedGroup) bool {
	workIDs := sortedUniqueStrings(verifiedWorkIDs(group.Works))
	present := sortedUniqueStrings(group.ExpectedPresentItemIDs)
	writes := sortedUniqueStrings(group.ExpectedWriteItemIDs)
	if len(present) != len(group.ExpectedPresentItemIDs) || len(writes) != len(group.ExpectedWriteItemIDs) {
		return false
	}
	for _, itemID := range present {
		if containsString(writes, itemID) {
			return false
		}
	}
	return sameStringSet(workIDs, append(append([]string(nil), present...), writes...))
}

func validateBatchPreflightTimes(createdValue, expiresValue, businessDate, journalCreatedValue string, now time.Time) error {
	createdAt, createdErr := time.Parse(time.RFC3339Nano, createdValue)
	expiresAt, expiresErr := time.Parse(time.RFC3339Nano, expiresValue)
	journalCreatedAt, journalErr := time.Parse(time.RFC3339Nano, journalCreatedValue)
	createdBusinessDate, businessErr := ShanghaiBusinessDate(createdAt)
	if createdErr != nil || expiresErr != nil || journalErr != nil || businessErr != nil ||
		!createdAt.Equal(journalCreatedAt) || !expiresAt.After(createdAt) || createdBusinessDate != businessDate {
		return errors.New("Qianchuan batch preflight timestamps are invalid")
	}
	expectedExpiry, expiryErr := batchPreflightExpiry(createdAt)
	if expiryErr != nil || !expiresAt.Equal(expectedExpiry) {
		return errors.New("Qianchuan batch preflight timestamps are invalid")
	}
	if now.IsZero() {
		now = time.Now()
	}
	if !now.UTC().Before(expiresAt) {
		return fmt.Errorf("%w; run preflight again", ErrBatchPreflightExpired)
	}
	return nil
}

func (decoded decodedBatchPreflight) summary(preflightID string) BatchPreflightSummary {
	if decoded.v2 != nil {
		snapshot := decoded.v2
		productSet := map[string]struct{}{}
		eligibleWorks := map[string]struct{}{}
		ready := false
		decisions := make([]BatchPreflightDecision, 0, len(snapshot.Groups))
		for _, groupID := range sortedPreparedGroupIDs(snapshot.Groups) {
			group := snapshot.Groups[groupID]
			for _, productID := range group.Identity.ProductIDs {
				productSet[productID] = struct{}{}
			}
			if group.SubmitEligible {
				ready = true
				for _, work := range group.Works {
					eligibleWorks[work.AwemeItemID] = struct{}{}
				}
			}
			decisions = append(decisions, BatchPreflightDecision{
				GroupID: groupID, CreatorID: group.Identity.CreatorID,
				PlanType: group.Identity.PlanType, Business: group.Identity.Business,
				Action: group.ExpectedAction, ExistingPlanID: group.ExpectedAdID,
			})
		}
		return BatchPreflightSummary{
			SchemaVersion: snapshot.SchemaVersion, PreflightID: preflightID,
			CreatedAt: snapshot.CreatedAt, ExpiresAt: snapshot.ExpiresAt,
			AdvertiserID: snapshot.AdvertiserID, TemplateID: snapshot.TemplateID,
			TemplateName: snapshot.TemplateName, ProductName: snapshot.ProductName,
			ProductShortName: snapshot.ProductShortName, ProductIDs: sortedStringSet(productSet),
			BusinessDate: snapshot.BusinessDate, EligibleWorks: len(eligibleWorks),
			SkippedWorks: len(snapshot.Skipped), Decisions: decisions, ReadyForSubmit: ready,
		}
	}
	snapshot := decoded.legacy
	productSet := map[string]struct{}{}
	for _, work := range snapshot.Works {
		for _, productID := range work.MatchedProductIDs {
			productSet[productID] = struct{}{}
		}
	}
	groupIDs := make([]string, 0, len(snapshot.Expected))
	for groupID := range snapshot.Expected {
		groupIDs = append(groupIDs, groupID)
	}
	sort.Strings(groupIDs)
	decisions := make([]BatchPreflightDecision, 0, len(groupIDs))
	for _, groupID := range groupIDs {
		expected := snapshot.Expected[groupID]
		decisions = append(decisions, BatchPreflightDecision{
			GroupID: groupID, CreatorID: expected.CreatorID, PlanType: expected.PlanType,
			Business: expected.Business, Action: expected.Action, ExistingPlanID: expected.AdID,
		})
	}
	return BatchPreflightSummary{
		SchemaVersion: 1, PreflightID: preflightID, CreatedAt: snapshot.CreatedAt, ExpiresAt: snapshot.ExpiresAt,
		AdvertiserID: snapshot.AdvertiserID, TemplateID: snapshot.TemplateID, TemplateName: snapshot.TemplateName,
		ProductName: snapshot.ProductName, ProductShortName: snapshot.ProductShortName,
		ProductIDs: sortedStringSet(productSet), EligibleWorks: len(snapshot.Works),
		SkippedWorks: len(snapshot.Skipped), Decisions: decisions, ReadyForSubmit: false,
	}
}

func clonePreparedGroups(source map[string]PreparedGroup) map[string]PreparedGroup {
	result := make(map[string]PreparedGroup, len(source))
	for groupID, group := range source {
		result[groupID] = clonePreparedGroup(group)
	}
	return result
}

func clonePreparedGroup(group PreparedGroup) PreparedGroup {
	group.Identity.ProductIDs = append([]string(nil), group.Identity.ProductIDs...)
	group.Works = append([]VerifiedWork(nil), group.Works...)
	group.ExpectedPresentItemIDs = append([]string(nil), group.ExpectedPresentItemIDs...)
	group.ExpectedWriteItemIDs = append([]string(nil), group.ExpectedWriteItemIDs...)
	return group
}

func sortedPreparedGroupIDs(groups map[string]PreparedGroup) []string {
	result := make([]string, 0, len(groups))
	for groupID := range groups {
		result = append(result, groupID)
	}
	sort.Strings(result)
	return result
}

func sortedUniqueStrings(values []string) []string {
	set := map[string]struct{}{}
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			set[value] = struct{}{}
		}
	}
	return sortedStringSet(set)
}

func sortedStringDifference(all, excluded []string) []string {
	excludedSet := stringSetFrom(excluded)
	result := []string{}
	for _, value := range sortedUniqueStrings(all) {
		if _, exists := excludedSet[value]; !exists {
			result = append(result, value)
		}
	}
	return result
}

func sortedStringSet(set map[string]struct{}) []string {
	result := make([]string, 0, len(set))
	for value := range set {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func sha256Hex(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && value == strings.ToLower(value)
}
