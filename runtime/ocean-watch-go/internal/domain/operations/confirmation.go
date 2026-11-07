package operations

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var ErrConfirmationSummary = errors.New("operation has no complete confirmation summary")

// ConfirmationItem is saved in the immutable snapshot, alongside the exact
// dispatch digests. Changes are produced by the use case, never by a transport
// accepting an arbitrary approval message.
type ConfirmationItem struct {
	ItemID         string               `json:"item_id"`
	GroupID        string               `json:"group_id"`
	Action         string               `json:"action"`
	TargetID       string               `json:"target_id"`
	PayloadDigest  string               `json:"payload_digest"`
	ExpectedDigest string               `json:"expected_digest"`
	Changes        []ConfirmationChange `json:"changes"`
	MaterialCount  *int                 `json:"material_count,omitempty"`
}

type ConfirmationChange struct {
	Field  string `json:"field"`
	Before string `json:"before"`
	After  string `json:"after"`
}

type ConfirmationSummary struct {
	Contract          string             `json:"contract"`
	OperationID       string             `json:"operation_id"`
	Version           int                `json:"version"`
	Kind              string             `json:"kind"`
	Channel           string             `json:"channel"`
	SubjectID         string             `json:"subject_id"`
	SnapshotDigest    string             `json:"snapshot_digest"`
	ScopeDigest       string             `json:"scope_digest"`
	PreflightAt       time.Time          `json:"preflight_at"`
	PreflightExpires  time.Time          `json:"preflight_expires_at"`
	ApprovalExpiresAt time.Time          `json:"approval_expires_at"`
	Items             []ConfirmationItem `json:"items"`
}

// NewConfirmationSummary rejects legacy journals and snapshots without enough
// review information. A digest or an item count alone is not a useful approval.
func NewConfirmationSummary(snapshot Snapshot, expiresAt time.Time) (ConfirmationSummary, error) {
	if err := snapshot.Validate(); err != nil {
		return ConfirmationSummary{}, err
	}
	if !expiresAt.After(snapshot.CreatedAt) || expiresAt.After(snapshot.ExpiresAt) {
		return ConfirmationSummary{}, ErrConfirmationSummary
	}
	var payload struct {
		Items []ConfirmationItem `json:"items"`
	}
	if err := json.Unmarshal(snapshot.Payload, &payload); err != nil {
		return ConfirmationSummary{}, ErrConfirmationSummary
	}
	scope, err := ApprovalScopeDigest(snapshot)
	if err != nil {
		return ConfirmationSummary{}, err
	}
	summary := ConfirmationSummary{
		Contract: "operation-confirmation-v1", OperationID: snapshot.OperationID,
		Version: snapshot.Version, Kind: snapshot.Kind, Channel: snapshot.Channel,
		SubjectID: snapshot.SubjectID, SnapshotDigest: snapshot.SnapshotDigest, ScopeDigest: scope,
		PreflightAt: snapshot.CreatedAt.UTC(), PreflightExpires: snapshot.ExpiresAt.UTC(),
		ApprovalExpiresAt: expiresAt.UTC(), Items: payload.Items,
	}
	if _, err := summary.Canonical(); err != nil {
		return ConfirmationSummary{}, err
	}
	return summary, nil
}

// Canonical is both the bounded UI document and the document the broker binds
// to its private challenge. Oversized or ambiguous documents fail; none are
// truncated before the user decides.
func (s ConfirmationSummary) Canonical() ([]byte, error) {
	if s.Contract != "operation-confirmation-v1" || !operationIDPattern.MatchString(s.OperationID) || s.Version < 1 ||
		!digestPattern.MatchString(s.SnapshotDigest) || !digestPattern.MatchString(s.ScopeDigest) ||
		!confirmationText(s.Kind, 128) || !confirmationText(s.SubjectID, 128) ||
		s.PreflightAt.IsZero() || !s.ApprovalExpiresAt.After(s.PreflightAt) || s.ApprovalExpiresAt.After(s.PreflightExpires) ||
		len(s.Items) == 0 || len(s.Items) > 100 {
		return nil, ErrConfirmationSummary
	}
	switch s.Channel {
	case "marketing", "qianchuan", "star_map":
	default:
		return nil, ErrConfirmationSummary
	}
	seen := make(map[string]bool, len(s.Items))
	for _, item := range s.Items {
		if !safeIDPattern.MatchString(item.ItemID) || !safeIDPattern.MatchString(item.GroupID) || !safeIDPattern.MatchString(item.TargetID) ||
			seen[item.ItemID] || !digestPattern.MatchString(item.PayloadDigest) || !digestPattern.MatchString(item.ExpectedDigest) ||
			len(item.Changes) == 0 || len(item.Changes) > 20 || (item.MaterialCount != nil && *item.MaterialCount < 0) {
			return nil, ErrConfirmationSummary
		}
		seen[item.ItemID] = true
		switch item.Action {
		case "create", "append", "update", "delete":
		default:
			return nil, ErrConfirmationSummary
		}
		fields := map[string]bool{}
		for _, change := range item.Changes {
			if !confirmationText(change.Field, 128) || sensitiveKey(change.Field) || fields[change.Field] ||
				(change.Before != "" && !confirmationText(change.Before, 512)) ||
				(change.After != "" && !confirmationText(change.After, 512)) || change.Before == change.After {
				return nil, ErrConfirmationSummary
			}
			fields[change.Field] = true
		}
	}
	encoded, err := json.MarshalIndent(s, "", "  ")
	if err != nil || len(encoded) > 12<<10 {
		return nil, ErrConfirmationSummary
	}
	return encoded, nil
}

// ReviewText is rendered from the same validated document as the challenge.
// The UI never receives a caller-authored explanation or a truncated preview.
func (s ConfirmationSummary) ReviewText() (string, error) {
	if _, err := s.Canonical(); err != nil {
		return "", err
	}
	var out strings.Builder
	zone := time.FixedZone("Asia/Shanghai", 8*60*60)
	fmt.Fprintf(&out, "渠道：%s\n广告账户：%s\n操作：%s（版本 %d）\n类型：%s\n预检时间：%s\n预检有效至：%s\n此次确认有效至：%s\n",
		s.Channel, s.SubjectID, s.OperationID, s.Version, s.Kind,
		s.PreflightAt.In(zone).Format("2006-01-02 15:04:05 +08:00"),
		s.PreflightExpires.In(zone).Format("2006-01-02 15:04:05 +08:00"),
		s.ApprovalExpiresAt.In(zone).Format("2006-01-02 15:04:05 +08:00"))
	actions := map[string]string{"create": "新建", "append": "追加", "update": "修改", "delete": "删除"}
	for index, item := range s.Items {
		fmt.Fprintf(&out, "\n%d. %s，目标：%s\n项目：%s；分组：%s\n", index+1, actions[item.Action], item.TargetID, item.ItemID, item.GroupID)
		if item.MaterialCount != nil {
			fmt.Fprintf(&out, "素材数量：%d\n", *item.MaterialCount)
		}
		if item.Action == "delete" {
			out.WriteString("风险：删除操作；取消本地调用不能撤销已派发的变更。\n")
		}
		for _, change := range item.Changes {
			before, after := change.Before, change.After
			if before == "" {
				before = "（无）"
			}
			if after == "" {
				after = "（无）"
			}
			fmt.Fprintf(&out, "%s：%s → %s\n", change.Field, before, after)
		}
	}
	return out.String(), nil
}

func confirmationText(value string, limit int) bool {
	if !utf8.ValidString(value) || strings.TrimSpace(value) == "" || len(value) > limit || containsRawURL(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			return false
		}
	}
	return true
}
