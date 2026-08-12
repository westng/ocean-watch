package oceanengine

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	domainplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/plans"
)

type EnvelopeError struct {
	HTTPStatus int
	Code       int64
	Message    string
	RequestID  string
	RetryAfter time.Duration
}

func (err *EnvelopeError) Error() string {
	if err.Code != 0 {
		return fmt.Sprintf("Ocean Engine API business error %d: %s", err.Code, Redact(err.Message))
	}
	return fmt.Sprintf("Ocean Engine API HTTP error %d", err.HTTPStatus)
}

func (err *EnvelopeError) OfficialResponseSummary() domainplans.OfficialResponse {
	result := domainplans.OfficialResponse{
		Message: Redact(err.Message), RequestID: strings.TrimSpace(err.RequestID),
	}
	if err.Code != 0 {
		code := err.Code
		result.Code = &code
	}
	return result
}

func GuardEnvelope(
	response *http.Response,
	sdkErr error,
	code *int64,
	message *string,
	requestID *string,
	requireData bool,
	hasData bool,
) error {
	if sdkErr != nil {
		var normalized *SDKRequestError
		if errors.As(sdkErr, &normalized) {
			return sdkErr
		}
		return &SDKRequestError{cause: sdkErr, dispatched: response != nil}
	}
	if response == nil {
		return errors.New("Ocean Engine SDK returned no HTTP response")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &EnvelopeError{HTTPStatus: response.StatusCode, RetryAfter: responseRetryAfter(response)}
	}
	if code == nil {
		return errors.New("Ocean Engine SDK response is missing business code")
	}
	if *code != 0 {
		return &EnvelopeError{
			HTTPStatus: response.StatusCode, Code: *code,
			Message: stringPointerValue(message), RequestID: stringPointerValue(requestID),
			RetryAfter: responseRetryAfter(response),
		}
	}
	if requireData && !hasData {
		return errors.New("Ocean Engine SDK success response is missing required data")
	}
	return nil
}

func responseRetryAfter(response *http.Response) time.Duration {
	if response == nil {
		return 0
	}
	value := strings.TrimSpace(response.Header.Get("Retry-After"))
	if value == "" {
		return 0
	}
	seconds, err := strconv.Atoi(value)
	if err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	when, err := http.ParseTime(value)
	if err != nil {
		return 0
	}
	delay := time.Until(when)
	if delay < 0 {
		return 0
	}
	return delay
}

func stringPointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
