package plans

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type Channel string

const (
	ChannelMarketing Channel = "marketing"
	ChannelQianchuan Channel = "qianchuan"
)

type LockFamily string

const (
	LockMarketingPlans LockFamily = "marketing-plans"
	LockQianchuanWorks LockFamily = "qianchuan-work-plans"
	LockPlanSettings   LockFamily = "plan-settings"
)

type WriteScope struct {
	Channel      Channel
	AdvertiserID string
	LockFamily   LockFamily
}

var positiveIDPattern = regexp.MustCompile(`^[1-9][0-9]*$`)

func (scope WriteScope) Validate() error {
	if scope.Channel != ChannelMarketing && scope.Channel != ChannelQianchuan {
		return errors.New("write channel must be marketing or qianchuan")
	}
	if !positiveIDPattern.MatchString(strings.TrimSpace(scope.AdvertiserID)) {
		return errors.New("advertiser_id must be a positive decimal ID")
	}
	switch scope.LockFamily {
	case LockMarketingPlans:
		if scope.Channel != ChannelMarketing {
			return errors.New("marketing plan locks require the marketing channel")
		}
	case LockQianchuanWorks:
		if scope.Channel != ChannelQianchuan {
			return errors.New("Qianchuan work locks require the qianchuan channel")
		}
	case LockPlanSettings:
	default:
		return errors.New("write lock family is unsupported")
	}
	return nil
}

type WriteCapability struct {
	scope    WriteScope
	nonce    string
	issuedAt time.Time
}

func IssueWriteCapability(submit bool, scope WriteScope, now time.Time) (WriteCapability, error) {
	if err := scope.Validate(); err != nil {
		return WriteCapability{}, err
	}
	if !submit {
		return WriteCapability{}, errors.New("write capability requires explicit submit authorization")
	}
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return WriteCapability{}, fmt.Errorf("issue write capability: %w", err)
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return WriteCapability{
		scope: scope, nonce: hex.EncodeToString(bytes), issuedAt: now.UTC(),
	}, nil
}

func (capability WriteCapability) Authorizes(scope WriteScope) bool {
	return capability.nonce != "" && capability.scope == scope && !capability.issuedAt.IsZero()
}

type DispatchState string

const (
	DispatchAcknowledged DispatchState = "acknowledged"
	DispatchNotSent      DispatchState = "not_sent"
	DispatchUnknown      DispatchState = "unknown"
)

type DispatchFailure struct {
	State DispatchState
	Cause error
}

func (failure *DispatchFailure) Error() string {
	if failure == nil || failure.Cause == nil {
		return "write dispatch failed"
	}
	return failure.Cause.Error()
}

func (failure *DispatchFailure) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.Cause
}

func ClassifyDispatchError(err error) DispatchState {
	if err == nil {
		return DispatchAcknowledged
	}
	var failure *DispatchFailure
	if errors.As(err, &failure) {
		switch failure.State {
		case DispatchAcknowledged, DispatchNotSent, DispatchUnknown:
			return failure.State
		}
	}
	return DispatchUnknown
}

type ReconciliationState string

const (
	ReconciliationApplied    ReconciliationState = "applied"
	ReconciliationNotApplied ReconciliationState = "not_applied"
	ReconciliationAmbiguous  ReconciliationState = "ambiguous"
)

type Reconciliation struct {
	State      ReconciliationState `json:"state"`
	ObjectID   string              `json:"object_id,omitempty"`
	Candidates int                 `json:"candidates"`
	Reason     string              `json:"reason,omitempty"`
}

func ReconcileCandidates(objectIDs []string) (Reconciliation, error) {
	unique := make([]string, 0, len(objectIDs))
	seen := map[string]struct{}{}
	for _, objectID := range objectIDs {
		objectID = strings.TrimSpace(objectID)
		if objectID == "" || !positiveIDPattern.MatchString(objectID) {
			return Reconciliation{}, errors.New("reconciliation candidate contains an invalid object ID")
		}
		if _, exists := seen[objectID]; exists {
			continue
		}
		seen[objectID] = struct{}{}
		unique = append(unique, objectID)
	}
	switch len(unique) {
	case 0:
		return Reconciliation{State: ReconciliationNotApplied, Candidates: 0}, nil
	case 1:
		return Reconciliation{
			State: ReconciliationApplied, ObjectID: unique[0], Candidates: 1,
		}, nil
	default:
		return Reconciliation{
			State: ReconciliationAmbiguous, Candidates: len(unique),
			Reason: "multiple official objects match the stable business key",
		}, nil
	}
}
