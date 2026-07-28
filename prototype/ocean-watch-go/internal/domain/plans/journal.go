package plans

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
)

var journalIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,127}$`)

type Journal struct {
	SchemaVersion int                        `json:"schema_version"`
	Fingerprint   string                     `json:"fingerprint"`
	CreatedAt     string                     `json:"created_at"`
	Jobs          map[string]JournalJob      `json:"jobs"`
	Extra         map[string]json.RawMessage `json:"-"`
}

type JournalJob struct {
	Status         string                     `json:"status"`
	AdvertiserID   string                     `json:"advertiser_id,omitempty"`
	ProjectID      string                     `json:"project_id,omitempty"`
	PromotionID    string                     `json:"promotion_id,omitempty"`
	AdID           string                     `json:"ad_id,omitempty"`
	Reconciliation *Reconciliation            `json:"reconciliation,omitempty"`
	RequestID      string                     `json:"request_id,omitempty"`
	FailureStage   string                     `json:"failure_stage,omitempty"`
	DispatchState  DispatchState              `json:"dispatch_state,omitempty"`
	LastResponse   *OfficialResponse          `json:"last_response,omitempty"`
	Extra          map[string]json.RawMessage `json:"-"`
}

type JournalRecord struct {
	RunID   string
	Journal Journal
}

type OfficialResponse struct {
	Code        *int64 `json:"code,omitempty"`
	Message     string `json:"message,omitempty"`
	RequestID   string `json:"request_id,omitempty"`
	ProjectID   string `json:"project_id,omitempty"`
	PromotionID string `json:"promotion_id,omitempty"`
}

type OfficialResponseSource interface {
	OfficialResponseSummary() OfficialResponse
}

func OfficialResponseFromError(err error) *OfficialResponse {
	var source OfficialResponseSource
	if !errors.As(err, &source) {
		return nil
	}
	summary := source.OfficialResponseSummary()
	if summary.Code == nil && strings.TrimSpace(summary.Message) == "" && strings.TrimSpace(summary.RequestID) == "" {
		return nil
	}
	return &summary
}

func (journal *Journal) UnmarshalJSON(payload []byte) error {
	type known Journal
	var decoded known
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(payload, &raw); err != nil {
		return err
	}
	for _, key := range []string{"schema_version", "fingerprint", "created_at", "jobs"} {
		delete(raw, key)
	}
	*journal = Journal(decoded)
	journal.Extra = raw
	return nil
}

func (journal Journal) MarshalJSON() ([]byte, error) {
	fields := cloneRawFields(journal.Extra)
	for key, value := range map[string]any{
		"schema_version": journal.SchemaVersion,
		"fingerprint":    journal.Fingerprint,
		"created_at":     journal.CreatedAt,
		"jobs":           journal.Jobs,
	} {
		if err := setRawField(fields, key, value); err != nil {
			return nil, err
		}
	}
	return json.Marshal(fields)
}

func (job *JournalJob) UnmarshalJSON(payload []byte) error {
	type known JournalJob
	var decoded known
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(payload, &raw); err != nil {
		return err
	}
	for _, key := range []string{
		"status", "advertiser_id", "project_id", "promotion_id", "ad_id",
		"reconciliation", "request_id", "failure_stage", "dispatch_state", "last_response",
	} {
		delete(raw, key)
	}
	*job = JournalJob(decoded)
	job.Extra = raw
	return nil
}

func (job JournalJob) MarshalJSON() ([]byte, error) {
	fields := cloneRawFields(job.Extra)
	if err := setRawField(fields, "status", job.Status); err != nil {
		return nil, err
	}
	for key, value := range map[string]string{
		"advertiser_id": job.AdvertiserID,
		"project_id":    job.ProjectID,
		"promotion_id":  job.PromotionID,
		"ad_id":         job.AdID,
		"request_id":    job.RequestID,
		"failure_stage": job.FailureStage,
	} {
		if value == "" {
			delete(fields, key)
			continue
		}
		if err := setRawField(fields, key, value); err != nil {
			return nil, err
		}
	}
	if job.DispatchState == "" {
		delete(fields, "dispatch_state")
	} else if err := setRawField(fields, "dispatch_state", job.DispatchState); err != nil {
		return nil, err
	}
	if job.Reconciliation == nil {
		delete(fields, "reconciliation")
	} else if err := setRawField(fields, "reconciliation", job.Reconciliation); err != nil {
		return nil, err
	}
	if job.LastResponse == nil {
		delete(fields, "last_response")
	} else if err := setRawField(fields, "last_response", job.LastResponse); err != nil {
		return nil, err
	}
	return json.Marshal(fields)
}

func cloneRawFields(source map[string]json.RawMessage) map[string]json.RawMessage {
	result := make(map[string]json.RawMessage, len(source)+8)
	for key, value := range source {
		result[key] = append(json.RawMessage(nil), value...)
	}
	return result
}

func setRawField(target map[string]json.RawMessage, key string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	target[key] = payload
	return nil
}

func NewJournal(fingerprint string, jobs map[string]JournalJob, now time.Time) (Journal, error) {
	fingerprint = strings.TrimSpace(fingerprint)
	if fingerprint == "" || len(fingerprint) > 256 {
		return Journal{}, errors.New("journal fingerprint is required")
	}
	if len(jobs) == 0 {
		return Journal{}, errors.New("journal requires at least one job")
	}
	for key, job := range jobs {
		if !validJournalJobKey(key) {
			return Journal{}, errors.New("journal job key contains unsupported characters")
		}
		if strings.TrimSpace(job.Status) == "" {
			return Journal{}, errors.New("journal job status is required")
		}
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return Journal{
		SchemaVersion: 1, Fingerprint: fingerprint,
		CreatedAt: now.UTC().Format(time.RFC3339Nano), Jobs: jobs,
	}, nil
}

func validJournalJobKey(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 4096 {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func ValidateJournalID(runID string) error {
	if !journalIDPattern.MatchString(strings.TrimSpace(runID)) {
		return errors.New("run_id contains unsupported characters")
	}
	return nil
}
