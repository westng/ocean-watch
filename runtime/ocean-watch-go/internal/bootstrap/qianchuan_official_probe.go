package bootstrap

import (
	"context"
	"errors"
	"strconv"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/officialmcp"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/requestcontrol"
)

var ErrOfficialProbeAuthorization = errors.New("official MCP probe authorization preparation failed")

type officialProbeTokenLeaser interface {
	Ensure(context.Context, auth.TokenQuery) (auth.TokenLease, error)
}

var _ officialProbeTokenLeaser = (*auth.TokenManager)(nil)

// ScopedQianchuanOfficialProbe is a developer-only P0.5 handle. It keeps the
// advertiser and lease fixed for the session. No CLI/MCP route constructs it.
// Real use requires the explicitly authorized scope; P1 supplies the complete
// immutable instance and per-query policy before any production activation.
type ScopedQianchuanOfficialProbe struct {
	client       *officialmcp.Probe
	lease        auth.TokenLease
	advertiserID string
}

func OpenQianchuanOfficialProbe(ctx context.Context, manager officialProbeTokenLeaser, control requestcontrol.SharedController, advertiserID, authAccountID string) (*ScopedQianchuanOfficialProbe, error) {
	return openQianchuanOfficialProbe(ctx, manager, control, advertiserID, authAccountID, officialmcp.NewProbe)
}

func openQianchuanOfficialProbe(ctx context.Context, manager officialProbeTokenLeaser, control requestcontrol.SharedController, advertiserID, authAccountID string, open func(context.Context, string, requestcontrol.SharedController) (*officialmcp.Probe, error)) (*ScopedQianchuanOfficialProbe, error) {
	id, err := strconv.ParseInt(advertiserID, 10, 64)
	if err != nil || id < 1 || strconv.FormatInt(id, 10) != advertiserID || ctx == nil || manager == nil || control == nil {
		return nil, officialmcp.ErrProbeScope
	}
	if authAccountID != "" {
		id, err := strconv.ParseInt(authAccountID, 10, 64)
		if err != nil || id < 1 || strconv.FormatInt(id, 10) != authAccountID {
			return nil, officialmcp.ErrProbeScope
		}
	}
	lease, err := manager.Ensure(ctx, auth.TokenQuery{
		Channel: "qianchuan", AdvertiserID: advertiserID, AuthAccountID: authAccountID,
	})
	if err != nil || lease.Channel != "qianchuan" || lease.AuthorizationID == "" || lease.AccessToken == "" {
		return nil, ErrOfficialProbeAuthorization
	}
	scopedCtx, err := auth.WithAdvertiserTokenLease(ctx, lease, advertiserID)
	if err != nil {
		return nil, ErrOfficialProbeAuthorization
	}
	client, err := open(scopedCtx, lease.AccessToken, control)
	if err != nil {
		return nil, err
	}
	return &ScopedQianchuanOfficialProbe{client: client, lease: lease, advertiserID: advertiserID}, nil
}

func (probe *ScopedQianchuanOfficialProbe) Inspect(ctx context.Context) (officialmcp.ProbeDiscovery, error) {
	scopedCtx, err := auth.WithAdvertiserTokenLease(ctx, probe.lease, probe.advertiserID)
	if err != nil {
		return officialmcp.ProbeDiscovery{}, ErrOfficialProbeAuthorization
	}
	return probe.client.Inspect(scopedCtx)
}

func (probe *ScopedQianchuanOfficialProbe) Read(ctx context.Context, queryID, approvedInputSourceDigest string) (officialmcp.ProbeObservation, error) {
	scopedCtx, err := auth.WithAdvertiserTokenLease(ctx, probe.lease, probe.advertiserID)
	if err != nil {
		return officialmcp.ProbeObservation{}, ErrOfficialProbeAuthorization
	}
	return probe.client.ReadAdvertiser(scopedCtx, queryID, probe.advertiserID, approvedInputSourceDigest)
}

func (probe *ScopedQianchuanOfficialProbe) Counts() officialmcp.ProbeCounts {
	return probe.client.Counts()
}

func (probe *ScopedQianchuanOfficialProbe) ReadPlanPage(ctx context.Context, approvedInputSourceDigest string, input officialmcp.ProbePlanPageParams) (officialmcp.ProbePlanPageObservation, error) {
	scopedCtx, err := auth.WithAdvertiserTokenLease(ctx, probe.lease, probe.advertiserID)
	if err != nil {
		return officialmcp.ProbePlanPageObservation{}, ErrOfficialProbeAuthorization
	}
	return probe.client.ReadPlanPage(scopedCtx, probe.advertiserID, approvedInputSourceDigest, input)
}

func (probe *ScopedQianchuanOfficialProbe) Close() error {
	return probe.client.Close()
}
