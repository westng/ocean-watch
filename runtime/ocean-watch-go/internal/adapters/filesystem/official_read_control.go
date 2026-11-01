package filesystem

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
)

// OfficialReadController retains exactly the SDK advertiser lock paths. Other
// documented subjects use a distinct authorization-bound namespace, never an
// organization/shop identifier masquerading as an advertiser.
type OfficialReadController struct{ Advertisers QianchuanRequestController }

func (controller OfficialReadController) Acquire(ctx context.Context, key string) (func(error, *http.Response) error, error) {
	return controller.AcquireRead(ctx, key)
}

func (controller OfficialReadController) AcquireRead(ctx context.Context, key string) (func(error, *http.Response) error, error) {
	if positiveDecimalID(key) {
		return controller.Advertisers.AcquireRead(ctx, key)
	}
	parts := strings.SplitN(key, ":", 4)
	if len(parts) != 4 || parts[0] != "subject" || parts[1] == "" || len(parts[1]) > 256 || strings.ContainsAny(parts[1], "/\\?#\x00\r\n\t ") {
		return nil, errors.New("invalid official read control scope")
	}
	switch parts[2] {
	case "authorization", "public_catalog", "agent", "shop", "customer_center", "organization":
	default:
		return nil, errors.New("invalid official read control scope")
	}
	if parts[2] != "public_catalog" && parts[3] == "" {
		return nil, errors.New("invalid official read control scope")
	}
	sum := sha256.Sum256([]byte(key))
	paths, err := controller.Advertisers.requestPathsForBase(ctx, "official-subject-"+hex.EncodeToString(sum[:]))
	if err != nil {
		return nil, err
	}
	return controller.Advertisers.acquireReadPaths(ctx, paths)
}
