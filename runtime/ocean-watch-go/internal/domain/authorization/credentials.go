package authorization

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"time"
)

var ErrInvalidCredential = errors.New("credential metadata is invalid")

var identifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

type Key struct {
	Channel         string
	AuthorizationID string
}

func (key Key) Validate() error {
	switch key.Channel {
	case "marketing", "qianchuan", "star_map":
	default:
		return ErrInvalidCredential
	}
	if !identifierPattern.MatchString(key.AuthorizationID) {
		return ErrInvalidCredential
	}
	return nil
}

func Reference(key Key, revision int64) (string, error) {
	if key.Validate() != nil || revision < 1 || revision > math.MaxInt32 {
		return "", ErrInvalidCredential
	}
	payload, err := json.Marshal(struct {
		Schema   int
		Key      Key
		Revision int64
	}{1, key, revision})
	if err != nil {
		return "", ErrInvalidCredential
	}
	digest := sha256.Sum256(payload)
	return "owcred-v1-" + hex.EncodeToString(digest[:]), nil
}

type CredentialVersion struct {
	Key              Key
	Revision         int64
	Reference        string
	Digest           string
	IssuedAt         time.Time
	AccessExpiresAt  time.Time
	RefreshExpiresAt time.Time
}

func NewCredentialVersion(key Key, revision int64, issued, accessExpiry, refreshExpiry time.Time) (CredentialVersion, error) {
	ref, err := Reference(key, revision)
	if err != nil {
		return CredentialVersion{}, err
	}
	version := CredentialVersion{Key: key, Revision: revision, Reference: ref,
		IssuedAt: issued.UTC(), AccessExpiresAt: accessExpiry.UTC(), RefreshExpiresAt: refreshExpiry.UTC()}
	version.Digest = version.digest()
	if err := version.Validate(); err != nil {
		return CredentialVersion{}, err
	}
	return version, nil
}

func (version CredentialVersion) Validate() error {
	ref, err := Reference(version.Key, version.Revision)
	if err != nil || version.Reference != ref || !validTime(version.IssuedAt) ||
		!validTime(version.AccessExpiresAt) || !validTime(version.RefreshExpiresAt) ||
		!version.AccessExpiresAt.After(version.IssuedAt) || !version.RefreshExpiresAt.After(version.IssuedAt) ||
		version.Digest != version.digest() {
		return ErrInvalidCredential
	}
	return nil
}

func (version CredentialVersion) Equal(other CredentialVersion) bool {
	return version.Key == other.Key && version.Revision == other.Revision &&
		version.Reference == other.Reference && version.Digest == other.Digest &&
		version.IssuedAt.Equal(other.IssuedAt) && version.AccessExpiresAt.Equal(other.AccessExpiresAt) &&
		version.RefreshExpiresAt.Equal(other.RefreshExpiresAt)
}

func (version CredentialVersion) digest() string {
	payload, _ := json.Marshal(struct {
		Schema           int
		Key              Key
		Revision         int64
		Reference        string
		IssuedAt         int64
		AccessExpiresAt  int64
		RefreshExpiresAt int64
	}{1, version.Key, version.Revision, version.Reference, version.IssuedAt.UnixNano(),
		version.AccessExpiresAt.UnixNano(), version.RefreshExpiresAt.UnixNano()})
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func validTime(value time.Time) bool {
	return !value.IsZero() && value.UnixNano() > 0 && value.Year() < 2200 && value.Year() >= 1970
}

type Status string

const (
	Active                  Status = "active"
	ReauthorizationRequired Status = "reauthorization_required"
)

type RotationStatus string

const (
	RotationIntent                  RotationStatus = "intent"
	RotationCandidateStored         RotationStatus = "candidate_stored"
	RotationActivated               RotationStatus = "activated"
	RotationCompleted               RotationStatus = "completed"
	RotationReauthorizationRequired RotationStatus = "reauthorization_required"
)

type Rotation struct {
	Key                Key
	OldRevision        int64
	OldReference       string
	NewRevision        int64
	CandidateReference string
	Candidate          *CredentialVersion
	Status             RotationStatus
	StartedAt          time.Time
	ActivatedAt        time.Time
	CompletedAt        time.Time
	FailureCode        string
}

func NewRotation(current CredentialVersion, now time.Time) (Rotation, error) {
	if current.Validate() != nil || !validTime(now) || now.Before(current.IssuedAt) {
		return Rotation{}, ErrInvalidCredential
	}
	ref, err := Reference(current.Key, current.Revision+1)
	if err != nil {
		return Rotation{}, err
	}
	return Rotation{Key: current.Key, OldRevision: current.Revision, OldReference: current.Reference,
		NewRevision: current.Revision + 1, CandidateReference: ref, Status: RotationIntent, StartedAt: now.UTC()}, nil
}

func (rotation Rotation) Validate() error {
	oldRef, oldErr := Reference(rotation.Key, rotation.OldRevision)
	newRef, newErr := Reference(rotation.Key, rotation.NewRevision)
	if oldErr != nil || newErr != nil || rotation.NewRevision != rotation.OldRevision+1 ||
		rotation.OldReference != oldRef || rotation.CandidateReference != newRef || !validTime(rotation.StartedAt) {
		return ErrInvalidCredential
	}
	if rotation.Candidate != nil && (!rotation.Accepts(*rotation.Candidate) || rotation.Candidate.Validate() != nil) {
		return ErrInvalidCredential
	}
	switch rotation.Status {
	case RotationIntent:
		if rotation.Candidate != nil || !rotation.ActivatedAt.IsZero() || !rotation.CompletedAt.IsZero() || rotation.FailureCode != "" {
			return ErrInvalidCredential
		}
	case RotationCandidateStored:
		if rotation.Candidate == nil || !rotation.ActivatedAt.IsZero() || !rotation.CompletedAt.IsZero() || rotation.FailureCode != "" {
			return ErrInvalidCredential
		}
	case RotationActivated, RotationCompleted:
		if rotation.Candidate == nil || !validTime(rotation.ActivatedAt) || rotation.ActivatedAt.Before(rotation.Candidate.IssuedAt) || rotation.FailureCode != "" {
			return ErrInvalidCredential
		}
		if rotation.Status == RotationCompleted {
			if !validTime(rotation.CompletedAt) || rotation.CompletedAt.Before(rotation.ActivatedAt) {
				return ErrInvalidCredential
			}
		} else if !rotation.CompletedAt.IsZero() {
			return ErrInvalidCredential
		}
	case RotationReauthorizationRequired:
		if !ValidFailureCode(rotation.FailureCode) || !rotation.CompletedAt.IsZero() {
			return ErrInvalidCredential
		}
	default:
		return ErrInvalidCredential
	}
	return nil
}

func (rotation Rotation) Accepts(version CredentialVersion) bool {
	return version.Key == rotation.Key && version.Revision == rotation.NewRevision &&
		version.Reference == rotation.CandidateReference && !version.IssuedAt.Before(rotation.StartedAt)
}

func ValidFailureCode(code string) bool {
	switch code {
	case "official_result_unknown", "candidate_unavailable", "candidate_invalid", "credential_invalid":
		return true
	default:
		return false
	}
}

type CredentialState struct {
	Credential CredentialVersion
	Status     Status
	Rotation   *Rotation
}

func (state CredentialState) Validate() error {
	if state.Credential.Validate() != nil || (state.Status != Active && state.Status != ReauthorizationRequired) {
		return ErrInvalidCredential
	}
	if state.Rotation == nil {
		return nil
	}
	rotation := state.Rotation
	if rotation.Validate() != nil || rotation.Key != state.Credential.Key {
		return ErrInvalidCredential
	}
	if rotation.Status == RotationActivated || rotation.Status == RotationCompleted ||
		(rotation.Status == RotationReauthorizationRequired && !rotation.ActivatedAt.IsZero()) {
		if rotation.Candidate == nil || !state.Credential.Equal(*rotation.Candidate) {
			return ErrInvalidCredential
		}
	} else if state.Credential.Revision != rotation.OldRevision || state.Credential.Reference != rotation.OldReference {
		return ErrInvalidCredential
	}
	if (rotation.Status == RotationReauthorizationRequired) != (state.Status == ReauthorizationRequired) {
		return ErrInvalidCredential
	}
	return nil
}
