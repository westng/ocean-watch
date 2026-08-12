package oceanengine

import (
	cryptorand "crypto/rand"
	"errors"
	"io"
	"math"
	"math/big"
	"net"
	"strings"
	"time"
)

var DefaultReadRetryDelays = []time.Duration{time.Second, 2 * time.Second}

const DefaultMaxRetryAfter = 30 * time.Second

func defaultReadJitter(delay time.Duration) time.Duration {
	if delay <= 0 {
		return delay
	}
	maximumDelta := delay / 5
	if maximumDelta == 0 {
		return delay
	}
	randomDelta, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(maximumDelta)+1))
	if err != nil {
		return delay
	}
	delta := time.Duration(randomDelta.Int64())
	if delta > time.Duration(math.MaxInt64)-delay {
		return time.Duration(math.MaxInt64)
	}
	return delay + delta
}

func ClassifyReadError(err error) (bool, time.Duration) {
	if err == nil {
		return false, 0
	}
	var envelope *EnvelopeError
	if errors.As(err, &envelope) {
		retryableCode := envelope.Code == 40100 || envelope.Code == 51010
		retryableStatus := envelope.HTTPStatus == 408 || envelope.HTTPStatus == 425 ||
			envelope.HTTPStatus == 429 || envelope.HTTPStatus == 500 ||
			envelope.HTTPStatus == 502 || envelope.HTTPStatus == 503 || envelope.HTTPStatus == 504
		message := strings.ToLower(envelope.Message)
		rpcTimeout := strings.Contains(message, "rpc") &&
			(strings.Contains(message, "timeout") || strings.Contains(message, "timed out") || strings.Contains(message, "time out") || strings.Contains(message, "超时"))
		return retryableCode || retryableStatus || rpcTimeout, envelope.RetryAfter
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		return networkError.Timeout(), 0
	}
	return errors.Is(err, io.ErrUnexpectedEOF), 0
}
