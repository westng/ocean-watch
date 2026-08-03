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
