package operations

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const SchemaVersion = 1

const (
	maxPayloadSize = 1 << 20
	maxFieldSize   = 128
)

var (
	ErrInvalidSnapshot = errors.New("invalid snapshot")
	digestPattern      = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	operationIDPattern = regexp.MustCompile(`^op_[A-Za-z0-9_-]{1,128}$`)
)

type Snapshot struct {
	OperationID    string
	Version        int
	Kind           string
	Channel        string
	SubjectID      string
	RuntimeVersion string
	SchemaVersion  int
	InputDigest    string
	SnapshotDigest string
	CreatedAt      time.Time
	ExpiresAt      time.Time
	Payload        json.RawMessage
}

func (s Snapshot) Validate() error {
	if !operationIDPattern.MatchString(s.OperationID) {
		return invalidSnapshot("operation_id must match op_ followed by 1-128 safe characters")
	}
	if s.Version < 1 {
		return invalidSnapshot("version must be positive")
	}
	if err := validateField(s.Kind, "kind"); err != nil {
		return err
	}
	switch s.Channel {
	case "marketing", "qianchuan", "star_map":
	default:
		return invalidSnapshot("channel must be marketing, qianchuan, or star_map")
	}
	if err := validateField(s.SubjectID, "subject_id"); err != nil {
		return err
	}
	if err := validateField(s.RuntimeVersion, "runtime_version"); err != nil {
		return err
	}
	if s.SchemaVersion != SchemaVersion {
		return invalidSnapshot("schema_version must be 1")
	}
	if !digestPattern.MatchString(s.InputDigest) {
		return invalidSnapshot("input_digest must match sha256:<64 lowercase hex characters>")
	}
	if !digestPattern.MatchString(s.SnapshotDigest) {
		return invalidSnapshot("snapshot_digest must match sha256:<64 lowercase hex characters>")
	}
	createdAt := s.CreatedAt.UTC()
	expiresAt := s.ExpiresAt.UTC()
	if createdAt.IsZero() || expiresAt.IsZero() {
		return invalidSnapshot("created_at and expires_at are required")
	}
	if !expiresAt.After(createdAt) {
		return invalidSnapshot("expires_at must be after created_at")
	}
	object, err := parsePayload(s.Payload)
	if err != nil {
		return err
	}
	if err := validatePayloadSafety(object); err != nil {
		return err
	}
	digest, err := digestObject(object)
	if err != nil {
		return err
	}
	if s.SnapshotDigest != digest {
		return invalidSnapshot("snapshot_digest does not match payload")
	}
	return nil
}

func Digest(payload json.RawMessage) (string, error) {
	object, err := parsePayload(payload)
	if err != nil {
		return "", err
	}
	return digestObject(object)
}

func parsePayload(payload json.RawMessage) (map[string]any, error) {
	if len(payload) == 0 || len(payload) > maxPayloadSize {
		return nil, invalidSnapshot("payload must contain 1-%d bytes", maxPayloadSize)
	}
	if !utf8.Valid(payload) || !json.Valid(payload) {
		return nil, invalidSnapshot("payload must contain one valid UTF-8 JSON document")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	value, err := decodeJSONValue(decoder)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, invalidSnapshot("payload must contain one JSON document")
	}
	object, ok := value.(map[string]any)
	if !ok || len(object) == 0 {
		return nil, invalidSnapshot("payload must be a non-empty JSON object")
	}
	return object, nil
}

func decodeJSONValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, invalidSnapshot("payload contains invalid JSON")
	}
	switch token {
	case json.Delim('{'):
		object := make(map[string]any)
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return nil, invalidSnapshot("payload contains an invalid object key")
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, invalidSnapshot("payload contains a non-string object key")
			}
			if _, exists := object[key]; exists {
				return nil, invalidSnapshot("payload contains a duplicate object key")
			}
			child, err := decodeJSONValue(decoder)
			if err != nil {
				return nil, err
			}
			object[key] = child
		}
		if closing, err := decoder.Token(); err != nil || closing != json.Delim('}') {
			return nil, invalidSnapshot("payload contains an invalid object")
		}
		return object, nil
	case json.Delim('['):
		array := make([]any, 0)
		for decoder.More() {
			child, err := decodeJSONValue(decoder)
			if err != nil {
				return nil, err
			}
			array = append(array, child)
		}
		if closing, err := decoder.Token(); err != nil || closing != json.Delim(']') {
			return nil, invalidSnapshot("payload contains an invalid array")
		}
		return array, nil
	default:
		return token, nil
	}
}

func digestObject(object map[string]any) (string, error) {
	canonical, err := json.Marshal(object)
	if err != nil {
		return "", invalidSnapshot("payload cannot be encoded canonically")
	}
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func validatePayloadSafety(value any) error {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if sensitiveKey(key) {
				return invalidSnapshot("payload contains a sensitive key")
			}
			if containsRawURL(key) {
				return invalidSnapshot("payload contains an HTTP(S) URL")
			}
			if err := validatePayloadSafety(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range typed {
			if err := validatePayloadSafety(child); err != nil {
				return err
			}
		}
	case string:
		if containsRawURL(typed) {
			return invalidSnapshot("payload contains an HTTP(S) URL")
		}
	}
	return nil
}

func sensitiveKey(key string) bool {
	var normalized strings.Builder
	for _, value := range strings.ToLower(key) {
		if value == '_' || value == '-' || unicode.IsSpace(value) {
			continue
		}
		normalized.WriteRune(value)
	}
	switch normalized.String() {
	case "accesstoken", "refreshtoken", "token", "secret", "clientsecret", "appsecret",
		"cookie", "cookies", "setcookie", "authorization", "authorizationcode", "code",
		"approvaltoken", "signature", "password", "passwd", "privatekey", "apikey",
		"workurl", "rawurl", "rawresponse", "rawrequest", "officialresponse", "shareurl":
		return true
	default:
		return false
	}
}

func containsRawURL(value string) bool {
	value = strings.ToLower(value)
	return strings.Contains(value, "http://") || strings.Contains(value, "https://")
}

func validateField(value, field string) error {
	if len(value) == 0 || len(value) > maxFieldSize || strings.TrimSpace(value) == "" || !utf8.ValidString(value) {
		return invalidSnapshot("%s must contain 1-%d UTF-8 bytes", field, maxFieldSize)
	}
	for _, value := range value {
		if unicode.IsControl(value) {
			return invalidSnapshot("%s must not contain control characters", field)
		}
	}
	return nil
}

func invalidSnapshot(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidSnapshot, fmt.Sprintf(format, args...))
}
