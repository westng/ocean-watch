// Package policy enforces the immutable instance configuration in shared Core.
// It is independent of tool discovery, OAuth grants and transport visibility.
package policy

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/contracts"
)

var ErrConfiguration = errors.New("invalid business policy configuration")
var ErrDenied = errors.New("business policy denied this capability or subject")

type Instance struct {
	mode     contracts.QueryExecutionMode
	toolsets map[string]bool
	subjects map[string]map[string]map[string]bool
}

type Config struct {
	Mode            contracts.QueryExecutionMode   `json:"mode"`
	Toolsets        []string                       `json:"toolsets"`
	AllowedSubjects map[string]map[string][]string `json:"allowed_subjects,omitempty"`
}

func Default() Instance {
	instance, _ := New(Config{Mode: contracts.QueryModePlan, Toolsets: []string{"core", "marketing", "qianchuan", "reports"}})
	return instance
}

func New(config Config) (Instance, error) {
	instance := Instance{mode: config.Mode, toolsets: map[string]bool{}, subjects: map[string]map[string]map[string]bool{}}
	if modeRank(config.Mode) < 0 || len(config.Toolsets) == 0 {
		return Instance{}, ErrConfiguration
	}
	for _, name := range config.Toolsets {
		if instance.toolsets[name] || !knownToolset(name) {
			return Instance{}, ErrConfiguration
		}
		instance.toolsets[name] = true
	}
	for channel, scopes := range config.AllowedSubjects {
		if channel != "marketing" && channel != "qianchuan" && channel != "star_map" {
			return Instance{}, ErrConfiguration
		}
		instance.subjects[channel] = map[string]map[string]bool{}
		for kind, ids := range scopes {
			if kind != "advertiser" && kind != "agent" && kind != "shop" && kind != "customer_center" && kind != "organization" && kind != "authorization" {
				return Instance{}, ErrConfiguration
			}
			instance.subjects[channel][kind] = map[string]bool{}
			for _, id := range ids {
				if id == "" || strings.ContainsAny(id, "/\\?#\x00\r\n\t ") || instance.subjects[channel][kind][id] {
					return Instance{}, ErrConfiguration
				}
				if kind != "authorization" && !decimalID(id) {
					return Instance{}, ErrConfiguration
				}
				instance.subjects[channel][kind][id] = true
			}
		}
	}
	return instance, nil
}

// FromConfig snapshots only non-secret business_policy. Absent configuration
// preserves existing plan/query behavior; developer_docs always stays off.
func FromConfig(config map[string]any) (Instance, error) {
	raw, exists := config["business_policy"]
	if !exists {
		return Default(), nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return Instance{}, ErrConfiguration
	}
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.DisallowUnknownFields()
	var options Config
	if decoder.Decode(&options) != nil {
		return Instance{}, ErrConfiguration
	}
	return New(options)
}

func (instance Instance) Mode() contracts.QueryExecutionMode { return instance.mode }

// Snapshot contains configuration only, never credentials or OAuth grants.
func (instance Instance) Snapshot() Config {
	config := Config{Mode: instance.mode, Toolsets: instance.Toolsets(), AllowedSubjects: map[string]map[string][]string{}}
	for channel, scopes := range instance.subjects {
		config.AllowedSubjects[channel] = map[string][]string{}
		for kind, ids := range scopes {
			values := []string{}
			for id := range ids {
				values = append(values, id)
			}
			sort.Strings(values)
			config.AllowedSubjects[channel][kind] = values
		}
	}
	return config
}
func (instance Instance) Toolsets() []string {
	result := []string{}
	for _, name := range []string{"core", "marketing", "qianchuan", "reports", "developer_docs"} {
		if instance.toolsets[name] {
			result = append(result, name)
		}
	}
	return result
}

func (instance Instance) Check(required []string, minimum contracts.QueryExecutionMode) error {
	if modeRank(instance.mode) < 0 || modeRank(minimum) < 0 || modeRank(instance.mode) < modeRank(minimum) {
		return ErrDenied
	}
	for _, toolset := range required {
		if !knownToolset(toolset) || !instance.toolsets[toolset] {
			return ErrDenied
		}
	}
	return nil
}

func (instance Instance) CheckCapability(id string) error {
	spec, ok := contracts.DefaultCapabilityRegistry.ByID(id)
	if !ok {
		return ErrDenied
	}
	return instance.Check(spec.RequiredToolsets, spec.MinimumMode)
}

func (instance Instance) CheckSubject(channel, kind, id string) error {
	if channel != "marketing" && channel != "qianchuan" && channel != "star_map" {
		return ErrDenied
	}
	if kind == "public_catalog" {
		if id != "" {
			return ErrDenied
		}
		return nil
	}
	if id == "" {
		return ErrDenied
	}
	if scopes, configured := instance.subjects[channel]; configured {
		allowed, bounded := scopes[kind]
		if !bounded && len(scopes) > 0 {
			return ErrDenied
		}
		if bounded && !allowed[id] {
			return ErrDenied
		}
	}
	return nil
}

// Grant restrictions are an additional boundary for every business subject.
// An advertiser-only configuration does not need a second grant allowlist.
func (instance Instance) AuthorizationRestricted(channel string) bool {
	_, bounded := instance.subjects[channel]["authorization"]
	return bounded
}

func (instance Instance) CheckAuthorization(channel, id string) error {
	if channel != "marketing" && channel != "qianchuan" && channel != "star_map" || id == "" {
		return ErrDenied
	}
	if instance.AuthorizationRestricted(channel) && !instance.subjects[channel]["authorization"][id] {
		return ErrDenied
	}
	return nil
}

// A verified parent scope may cover its children when no narrower child
// allowlist was configured. Application must prove the relationship first.
func (instance Instance) CheckDescendant(channel, parentKind, parentID, childKind, childID string) error {
	if instance.CheckSubject(channel, parentKind, parentID) != nil || childID == "" {
		return ErrDenied
	}
	if scopes, configured := instance.subjects[channel]; configured {
		if allowed, bounded := scopes[childKind]; bounded {
			if !allowed[childID] {
				return ErrDenied
			}
			return nil
		}
	}
	return nil
}

func modeRank(mode contracts.QueryExecutionMode) int {
	switch mode {
	case contracts.QueryModeRead:
		return 0
	case contracts.QueryModePlan:
		return 1
	case contracts.QueryModeExecute:
		return 2
	default:
		return -1
	}
}
func knownToolset(name string) bool {
	switch name {
	case "core", "marketing", "qianchuan", "reports", "developer_docs":
		return true
	default:
		return false
	}
}
func decimalID(value string) bool {
	if value == "" || len(value) > 64 || value[0] == '0' {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
