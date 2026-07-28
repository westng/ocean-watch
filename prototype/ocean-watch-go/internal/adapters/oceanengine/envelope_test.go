package oceanengine

import (
	"context"
	"errors"
	"net/http"
	"testing"

	domainplans "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/plans"
)

func TestEnvelopeGuardRejectsHTTP200BusinessError(t *testing.T) {
	code := int64(40100)
	message := "rate limited"
	err := GuardEnvelope(&http.Response{StatusCode: 200}, nil, &code, &message, nil, false, false)
	var envelope *EnvelopeError
	if !errors.As(err, &envelope) || envelope.Code != code {
		t.Fatalf("business error was not preserved: %v", err)
	}
}

func TestEnvelopeErrorExposesOnlyRedactedOfficialSummary(t *testing.T) {
	err := &domainplans.DispatchFailure{
		State: domainplans.DispatchAcknowledged,
		Cause: &EnvelopeError{
			Code: 40000, Message: "access_token=TEST_TOKEN_DO_NOT_LOG", RequestID: "request-1",
		},
	}
	summary := domainplans.OfficialResponseFromError(err)
	if summary == nil || summary.Code == nil || *summary.Code != 40000 ||
		summary.Message != "sensitive Ocean Engine error details redacted" || summary.RequestID != "request-1" {
		t.Fatalf("official error summary was not safely preserved: %#v", summary)
	}
}

func TestEnvelopeGuardRejectsMissingCodeAndData(t *testing.T) {
	if err := GuardEnvelope(&http.Response{StatusCode: 200}, nil, nil, nil, nil, false, false); err == nil {
		t.Fatal("missing business code was accepted")
	}
	code := int64(0)
	if err := GuardEnvelope(&http.Response{StatusCode: 200}, nil, &code, nil, nil, true, false); err == nil {
		t.Fatal("missing required data was accepted")
	}
}

func TestEnvelopeGuardRejectsTransportFailure(t *testing.T) {
	cause := errors.New("access_token=TEST_TOKEN_DO_NOT_LOG")
	err := GuardEnvelope(nil, cause, nil, nil, nil, false, false)
	if err == nil {
		t.Fatal("transport error was accepted")
	}
	if err.Error() != "Ocean Engine SDK request failed" || !errors.Is(err, cause) {
		t.Fatal("transport error was not safely normalized")
	}
	if err := GuardEnvelope(nil, context.Canceled, nil, nil, nil, false, false); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation identity was not preserved")
	}
}

func TestEnvelopeGuardAcceptsCompleteSuccess(t *testing.T) {
	code := int64(0)
	if err := GuardEnvelope(&http.Response{StatusCode: 200}, nil, &code, nil, nil, true, true); err != nil {
		t.Fatal(err)
	}
}
