package requestcontrol

import (
	"context"
	"errors"
	"net/http"
)

var ErrAdvertiserScopeMissing = errors.New("request advertiser scope is missing")

type SharedController interface {
	Acquire(context.Context, string) (func(error, *http.Response) error, error)
}

// SharedReadController is optional so older controllers keep their serialized
// behavior. Only safe HTTP reads may use the shared admission path.
type SharedReadController interface {
	AcquireRead(context.Context, string) (func(error, *http.Response) error, error)
}
