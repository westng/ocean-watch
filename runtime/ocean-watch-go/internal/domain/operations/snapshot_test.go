package operations

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDigestCanonicalizesObjectOrderWhitespaceAndEscapes(t *testing.T) {
	payloads := []string{
		` { "b" : [ true, null, {"z":2,"a":"x"} ], "a" : 1 } `,
		`{"a":1,"b":[true,null,{"a":"x","z":2}]}`,
		`{"b":[true,null,{"\u0061":"\u0078","z":2}],"a":1}`,
	}
	want := expectedDigest([]byte(`{"a":1,"b":[true,null,{"a":"x","z":2}]}`))
	for _, payload := range payloads {
		before := []byte(payload)
		raw := bytes.Clone(before)
		got, err := Digest(raw)
		if err != nil {
			t.Fatalf("Digest: %v", err)
		}
		if got != want {
			t.Fatalf("Digest = %s, want %s", got, want)
		}
		if !bytes.Equal(raw, before) {
			t.Fatal("Digest mutated its payload")
		}
	}
}

func TestDigestPreservesNumberLexemes(t *testing.T) {
	for _, number := range []string{
		"9007199254740993", "9007199254740992",
		"900719925474099312345678901234567890",
		"-900719925474099312345678901234567890",
		"1.0000000000000000000000000000000001", "1.00", "1e400", "-0",
	} {
		t.Run(number, func(t *testing.T) {
			payload := []byte(`{"value":` + number + `}`)
			got, err := Digest(payload)
			if err != nil {
				t.Fatalf("Digest: %v", err)
			}
			if want := expectedDigest(payload); got != want {
				t.Fatalf("Digest changed number lexeme: got %s, want %s", got, want)
			}
		})
	}
}

func TestDigestRejectsDuplicateKeysAtEveryDepth(t *testing.T) {
	for _, payload := range []string{
		`{"a":1,"a":2}`,
		`{"a":null,"a":2}`,
		`{"a":1,"\u0061":2}`,
		`{"nested":{"key":true,"key":false}}`,
		`{"items":[{"key":1,"key":2}]}`,
	} {
		t.Run(payload, func(t *testing.T) {
			digest, err := Digest([]byte(payload))
			if !errors.Is(err, ErrInvalidSnapshot) || digest != "" {
				t.Fatalf("Digest = (%q, %v), want empty digest and ErrInvalidSnapshot", digest, err)
			}
		})
	}
}

func TestDigestRejectsInvalidPayloads(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
	}{
		{name: "empty"},
		{name: "whitespace", payload: []byte(" \t\n")},
		{name: "trailing document", payload: []byte(`{"a":1}{"b":2}`)},
		{name: "trailing token", payload: []byte(`{"a":1} trailing`)},
		{name: "trailing boolean", payload: []byte(`{"a":1} true`)},
		{name: "malformed", payload: []byte(`{"a":}`)},
		{name: "unclosed", payload: []byte(`{"a":1`)},
		{name: "trailing comma", payload: []byte(`{"a":1,}`)},
		{name: "bad number", payload: []byte(`{"a":01}`)},
		{name: "NaN", payload: []byte(`{"a":NaN}`)},
		{name: "array", payload: []byte(`[1]`)},
		{name: "null", payload: []byte(`null`)},
		{name: "string", payload: []byte(`"value"`)},
		{name: "number", payload: []byte(`123`)},
		{name: "empty object", payload: []byte(`{}`)},
		{name: "raw control", payload: []byte("{\"a\":\"line\nend\"}")},
		{name: "invalid UTF-8", payload: []byte{'{', '"', 'a', '"', ':', '"', 0xff, '"', '}'}},
		{name: "over 1 MiB", payload: []byte(`{"value":"` + strings.Repeat("x", maxPayloadSize) + `"}`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			digest, err := Digest(test.payload)
			if !errors.Is(err, ErrInvalidSnapshot) || digest != "" {
				t.Fatalf("Digest = (%q, %v), want empty digest and ErrInvalidSnapshot", digest, err)
			}
		})
	}
}

func TestDigestAcceptsPayloadAtSizeLimit(t *testing.T) {
	payload := []byte(`{"id":"` + strings.Repeat("x", maxPayloadSize-len(`{"id":""}`)) + `"}`)
	if len(payload) != maxPayloadSize {
		t.Fatalf("test payload has %d bytes, want %d", len(payload), maxPayloadSize)
	}
	if _, err := Digest(payload); err != nil {
		t.Fatalf("Digest at size limit: %v", err)
	}
	if _, err := Digest(append(payload, ' ')); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("Digest over size limit: %v, want ErrInvalidSnapshot", err)
	}
}

func TestDigestPreservesArrayOrder(t *testing.T) {
	first, err := Digest([]byte(`{"items":[1,2]}`))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Digest([]byte(`{"items":[2,1]}`))
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("different array order shared a digest")
	}
}

func TestSnapshotValidateAcceptsChannelsAndDoesNotMutate(t *testing.T) {
	for _, channel := range []string{"marketing", "qianchuan", "star_map"} {
		t.Run(channel, func(t *testing.T) {
			snapshot := validSnapshot(t, ` { "items": [ {"work_id":"123", "group_id":"existing-group", "failure_code":null} ] } `)
			snapshot.Channel = channel
			snapshot.Kind = "preflight_snapshot"
			before := snapshot
			before.Payload = bytes.Clone(snapshot.Payload)
			if err := snapshot.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}
			if !reflect.DeepEqual(snapshot, before) {
				t.Fatal("Validate mutated the snapshot")
			}
		})
	}
}

func TestSnapshotValidateRejectsSensitiveKeys(t *testing.T) {
	keys := []string{
		"access_token", "refresh_token", "token", "secret", "client_secret", "app_secret",
		"cookie", "authorization", "code", "approval_token", "signature", "password",
		"work_url", "raw_url", "raw_response", "ACCESS_TOKEN", "refreshToken", "ClientSecret",
		"__app__secret__", "Work_URL", "rawResponse", "Approval_Token", "access-token", "access token",
	}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			encodedKey, err := json.Marshal(key)
			if err != nil {
				t.Fatal(err)
			}
			for _, payload := range []string{
				`{` + string(encodedKey) + `:null}`,
				`{"items":[{"nested":{` + string(encodedKey) + `:null}}]}`,
			} {
				snapshot := validSnapshot(t, payload)
				if err := snapshot.Validate(); !errors.Is(err, ErrInvalidSnapshot) {
					t.Fatalf("Validate = %v, want ErrInvalidSnapshot", err)
				}
			}
		})
	}
	snapshot := validSnapshot(t, `{"\u0061ccess_token":null}`)
	if err := snapshot.Validate(); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("escaped sensitive key: %v, want ErrInvalidSnapshot", err)
	}
}

func TestSnapshotValidateRejectsRawLinks(t *testing.T) {
	for _, payload := range []string{
		`{"work_id":"https://example.invalid/work/123"}`,
		`{"note":"see HTTP://example.invalid/work/123"}`,
		`{"items":[{"nested":["HTTPS://example.invalid/123"]}]}`,
		`{"note":"https:\/\/example.invalid/123"}`,
		`{"note":"https\u003a//example.invalid/123"}`,
		`{"https://example.invalid/123":null}`,
	} {
		snapshot := validSnapshot(t, payload)
		if err := snapshot.Validate(); !errors.Is(err, ErrInvalidSnapshot) {
			t.Fatalf("Validate = %v, want ErrInvalidSnapshot", err)
		}
	}
}

func TestSnapshotValidateRejectsInvalidFields(t *testing.T) {
	base := validSnapshot(t, `{"subject_id":"subject-1"}`)
	tests := []struct {
		name string
		edit func(*Snapshot)
	}{
		{name: "empty operation id", edit: func(s *Snapshot) { s.OperationID = "" }},
		{name: "operation prefix", edit: func(s *Snapshot) { s.OperationID = "bad" }},
		{name: "operation suffix empty", edit: func(s *Snapshot) { s.OperationID = "op_" }},
		{name: "operation id too long", edit: func(s *Snapshot) { s.OperationID = "op_" + strings.Repeat("x", 129) }},
		{name: "operation path", edit: func(s *Snapshot) { s.OperationID = "op_../x" }},
		{name: "operation control", edit: func(s *Snapshot) { s.OperationID = "op_value\n" }},
		{name: "empty kind", edit: func(s *Snapshot) { s.Kind = "" }},
		{name: "blank kind", edit: func(s *Snapshot) { s.Kind = " " }},
		{name: "kind too long", edit: func(s *Snapshot) { s.Kind = strings.Repeat("x", 129) }},
		{name: "kind control", edit: func(s *Snapshot) { s.Kind = "create\nplan" }},
		{name: "empty subject", edit: func(s *Snapshot) { s.SubjectID = "" }},
		{name: "subject too long", edit: func(s *Snapshot) { s.SubjectID = strings.Repeat("x", 129) }},
		{name: "subject control", edit: func(s *Snapshot) { s.SubjectID = "subject\x00" }},
		{name: "subject unicode control", edit: func(s *Snapshot) { s.SubjectID = "subject\u0085" }},
		{name: "empty runtime", edit: func(s *Snapshot) { s.RuntimeVersion = "" }},
		{name: "runtime too long", edit: func(s *Snapshot) { s.RuntimeVersion = strings.Repeat("x", 129) }},
		{name: "runtime control", edit: func(s *Snapshot) { s.RuntimeVersion = "runtime\r1" }},
		{name: "runtime invalid UTF-8", edit: func(s *Snapshot) { s.RuntimeVersion = string([]byte{0xff}) }},
		{name: "unknown channel", edit: func(s *Snapshot) { s.Channel = "unknown" }},
		{name: "uppercase channel", edit: func(s *Snapshot) { s.Channel = "Marketing" }},
		{name: "zero version", edit: func(s *Snapshot) { s.Version = 0 }},
		{name: "negative version", edit: func(s *Snapshot) { s.Version = -1 }},
		{name: "schema version", edit: func(s *Snapshot) { s.SchemaVersion = 2 }},
		{name: "empty input digest", edit: func(s *Snapshot) { s.InputDigest = "" }},
		{name: "input digest format", edit: func(s *Snapshot) { s.InputDigest = "sha256:ABC" }},
		{name: "uppercase input digest", edit: func(s *Snapshot) { s.InputDigest = "sha256:" + strings.Repeat("A", 64) }},
		{name: "empty snapshot digest", edit: func(s *Snapshot) { s.SnapshotDigest = "" }},
		{name: "snapshot digest format", edit: func(s *Snapshot) { s.SnapshotDigest = "sha256:" + strings.Repeat("0", 63) + "g" }},
		{name: "tampered digest", edit: func(s *Snapshot) { s.SnapshotDigest = "sha256:" + strings.Repeat("0", 64) }},
		{name: "tampered payload", edit: func(s *Snapshot) { s.Payload = []byte(`{"subject_id":"changed"}`) }},
		{name: "zero created", edit: func(s *Snapshot) { s.CreatedAt = time.Time{} }},
		{name: "zero expires", edit: func(s *Snapshot) { s.ExpiresAt = time.Time{} }},
		{name: "expires before created", edit: func(s *Snapshot) { s.ExpiresAt = s.CreatedAt.Add(-time.Nanosecond) }},
		{name: "same time", edit: func(s *Snapshot) { s.ExpiresAt = s.CreatedAt }},
		{name: "nil payload", edit: func(s *Snapshot) { s.Payload = nil }},
		{name: "duplicate payload", edit: func(s *Snapshot) { s.Payload = []byte(`{"id":1,"id":2}`) }},
		{name: "trailing payload", edit: func(s *Snapshot) { s.Payload = []byte(`{"id":1} {"id":2}`) }},
		{name: "oversized payload", edit: func(s *Snapshot) { s.Payload = []byte(`{"value":"` + strings.Repeat("x", maxPayloadSize) + `"}`) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := base
			test.edit(&snapshot)
			if err := snapshot.Validate(); !errors.Is(err, ErrInvalidSnapshot) {
				t.Fatalf("Validate = %v, want ErrInvalidSnapshot", err)
			}
		})
	}
}

func TestSnapshotValidateAcceptsFieldSizeBoundaries(t *testing.T) {
	snapshot := validSnapshot(t, `{"id":"subject-1"}`)
	snapshot.OperationID = "op_" + strings.Repeat("x", 128)
	snapshot.Kind = strings.Repeat("x", 128)
	snapshot.SubjectID = strings.Repeat("x", 128)
	snapshot.RuntimeVersion = strings.Repeat("x", 128)
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("Validate at size limits: %v", err)
	}
}

func TestSnapshotValidateUsesUTCInstants(t *testing.T) {
	created := time.Date(2026, time.October, 21, 10, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	snapshot := validSnapshot(t, `{"id":"subject-1"}`)
	snapshot.CreatedAt = created
	snapshot.ExpiresAt = created.UTC().Add(time.Nanosecond)
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("Validate with positive UTC duration: %v", err)
	}
	snapshot.ExpiresAt = created.UTC()
	if err := snapshot.Validate(); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("Validate with same instant: %v, want ErrInvalidSnapshot", err)
	}
	snapshot.ExpiresAt = created.UTC().Add(-time.Nanosecond)
	if err := snapshot.Validate(); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("Validate with negative UTC duration: %v, want ErrInvalidSnapshot", err)
	}
}

func TestSnapshotValidateDoesNotFillInputDigest(t *testing.T) {
	snapshot := validSnapshot(t, `{"id":"subject-1"}`)
	snapshot.InputDigest = ""
	if err := snapshot.Validate(); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("Validate with empty input digest: %v, want ErrInvalidSnapshot", err)
	}
	if snapshot.InputDigest != "" {
		t.Fatalf("Validate filled InputDigest: %q", snapshot.InputDigest)
	}
}

func TestSnapshotValidateErrorsDoNotEchoPayload(t *testing.T) {
	snapshot := validSnapshot(t, `{"id":"subject-1"}`)
	snapshot.Payload = []byte(`{"value":"diagnostic-marker","value":null}`)
	err := snapshot.Validate()
	if !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("Validate: %v, want ErrInvalidSnapshot", err)
	}
	if strings.Contains(err.Error(), "diagnostic-marker") {
		t.Fatal("validation error echoed payload data")
	}
}

func validSnapshot(t *testing.T, payload string) Snapshot {
	t.Helper()
	digest, err := Digest([]byte(payload))
	if err != nil {
		t.Fatalf("Digest(valid payload): %v", err)
	}
	return Snapshot{
		OperationID:    "op_01JABCDEF0123456789ABCDEFG",
		Version:        1,
		Kind:           "qianchuan_preflight",
		Channel:        "qianchuan",
		SubjectID:      "subject-1",
		RuntimeVersion: "runtime-1",
		SchemaVersion:  SchemaVersion,
		InputDigest:    "sha256:" + strings.Repeat("1", 64),
		SnapshotDigest: digest,
		CreatedAt:      time.Date(2026, time.October, 21, 10, 0, 0, 0, time.UTC),
		ExpiresAt:      time.Date(2026, time.October, 21, 10, 30, 0, 0, time.UTC),
		Payload:        []byte(payload),
	}
}

func expectedDigest(payload []byte) string {
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}
