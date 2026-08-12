package oceanengine

import (
	"context"
	"testing"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/requestcontrol"
)

const testAuthorizationID = "fixture-authorization"

func testRequestContext(t testing.TB, channel string) context.Context {
	t.Helper()
	ctx, _, _ := controlledTestRequestContext(t, channel, testAuthorizationID, 65536)
	return ctx
}

func controlledTestRequestContext(
	t testing.TB,
	channel string,
	authorizationID string,
	limit int64,
) (context.Context, *requestcontrol.Budget, *requestcontrol.Metrics) {
	t.Helper()
	ctx, budget, metrics, err := requestcontrol.PrepareCommandContext(context.Background(), limit)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err = requestcontrol.WithAuthorization(ctx, channel, authorizationID)
	if err != nil {
		t.Fatal(err)
	}
	return ctx, budget, metrics
}
