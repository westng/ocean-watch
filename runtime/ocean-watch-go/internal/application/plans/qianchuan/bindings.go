package qianchuan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"

	domainqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/qianchuan"
)

const PlanBindingSchemaVersion = 1

var businessDatePattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
var groupIDPattern = regexp.MustCompile(`^qcg_[0-9a-f]{64}$`)

type PlanBinding struct {
	BindingKey     string   `json:"binding_key"`
	GroupID        string   `json:"group_id"`
	BusinessDate   string   `json:"business_date"`
	AdvertiserID   string   `json:"advertiser_id"`
	TemplateID     string   `json:"template_id"`
	CreatorID      string   `json:"creator_id"`
	ProductIDs     []string `json:"product_ids"`
	PlanType       string   `json:"plan_type"`
	Business       string   `json:"business"`
	AdID           string   `json:"ad_id"`
	CreatedAt      string   `json:"created_at"`
	UpdatedAt      string   `json:"updated_at"`
	LastVerifiedAt string   `json:"last_verified_at"`
}

type PlanBindingDocument struct {
	SchemaVersion int                    `json:"schema_version"`
	Bindings      map[string]PlanBinding `json:"bindings"`
}

type PlanBindingReader interface {
	Get(context.Context, string, string) (PlanBinding, bool, error)
	List(context.Context) ([]PlanBinding, error)
}

type PlanBindingWriter interface {
	Put(context.Context, PlanBinding) error
}

type PlanBindingStore interface {
	PlanBindingReader
	PlanBindingWriter
}

func BindingKey(groupID, businessDate string) (string, error) {
	groupID, businessDate = strings.TrimSpace(groupID), strings.TrimSpace(businessDate)
	if !groupIDPattern.MatchString(groupID) {
		return "", errors.New("plan binding group_id is invalid")
	}
	if err := ValidateBusinessDate(businessDate); err != nil {
		return "", err
	}
	return groupID + "/" + businessDate, nil
}

func ValidateBusinessDate(businessDate string) error {
	businessDate = strings.TrimSpace(businessDate)
	if !businessDatePattern.MatchString(businessDate) {
		return errors.New("plan binding business_date is invalid")
	}
	if _, err := time.Parse("2006-01-02", businessDate); err != nil {
		return errors.New("plan binding business_date is invalid")
	}
	return nil
}

func NewPlanBinding(identity domainqianchuan.PlanGroupIdentity, groupID, businessDate, adID string, now time.Time) (PlanBinding, error) {
	canonicalID, err := domainqianchuan.GroupID(identity)
	if err != nil {
		return PlanBinding{}, err
	}
	if strings.TrimSpace(groupID) != canonicalID {
		return PlanBinding{}, errors.New("plan binding group_id does not match its identity")
	}
	key, err := BindingKey(canonicalID, businessDate)
	if err != nil {
		return PlanBinding{}, err
	}
	if !validPositiveID(strings.TrimSpace(adID)) {
		return PlanBinding{}, errors.New("plan binding ad_id is invalid")
	}
	if now.IsZero() {
		return PlanBinding{}, errors.New("plan binding timestamp is required")
	}
	timestamp := now.UTC().Format(time.RFC3339Nano)
	return PlanBinding{
		BindingKey: key, GroupID: canonicalID, BusinessDate: businessDate,
		AdvertiserID: identity.AdvertiserID, TemplateID: identity.TemplateID, CreatorID: identity.CreatorID,
		ProductIDs: append([]string(nil), identity.ProductIDs...), PlanType: identity.PlanType, Business: identity.Business,
		AdID: strings.TrimSpace(adID), CreatedAt: timestamp, UpdatedAt: timestamp, LastVerifiedAt: timestamp,
	}, nil
}

func ValidatePlanBinding(binding PlanBinding) error {
	identity, err := domainqianchuan.NewPlanGroupIdentity(
		binding.AdvertiserID, binding.TemplateID, binding.CreatorID,
		binding.ProductIDs, binding.PlanType, binding.Business,
	)
	if err != nil {
		return err
	}
	groupID, err := domainqianchuan.GroupID(identity)
	if err != nil || groupID != strings.TrimSpace(binding.GroupID) {
		return errors.New("plan binding identity digest is invalid")
	}
	key, err := BindingKey(groupID, binding.BusinessDate)
	if err != nil || key != strings.TrimSpace(binding.BindingKey) {
		return errors.New("plan binding key is invalid")
	}
	if !validPositiveID(strings.TrimSpace(binding.AdID)) {
		return errors.New("plan binding ad_id is invalid")
	}
	for _, value := range []string{binding.CreatedAt, binding.UpdatedAt, binding.LastVerifiedAt} {
		if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
			return errors.New("plan binding timestamp is invalid")
		}
	}
	return nil
}

func BindingMatchesIdentity(binding PlanBinding, identity domainqianchuan.PlanGroupIdentity, businessDate string) bool {
	groupID, err := domainqianchuan.GroupID(identity)
	if err != nil {
		return false
	}
	key, err := BindingKey(groupID, businessDate)
	return err == nil && ValidatePlanBinding(binding) == nil && binding.BindingKey == key
}

func PlanBindingDigest(binding *PlanBinding) (string, error) {
	var payload []byte
	var err error
	if binding == nil {
		payload, err = json.Marshal(struct {
			State string `json:"state"`
		}{State: "absent"})
	} else {
		if err := ValidatePlanBinding(*binding); err != nil {
			return "", err
		}
		payload, err = json.Marshal(binding)
	}
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func SortedPlanBindings(document PlanBindingDocument) []PlanBinding {
	result := make([]PlanBinding, 0, len(document.Bindings))
	for _, binding := range document.Bindings {
		result = append(result, binding)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].BindingKey < result[right].BindingKey })
	return result
}

func ShanghaiBusinessDate(now time.Time) (string, error) {
	if now.IsZero() {
		return "", errors.New("business date timestamp is required")
	}
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return "", err
	}
	return now.In(location).Format("2006-01-02"), nil
}
