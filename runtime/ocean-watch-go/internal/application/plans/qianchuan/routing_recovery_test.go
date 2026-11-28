package qianchuan

import (
	"context"
	"errors"
	wm "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/workmetadata"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	dq "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/qianchuan"
	"testing"
)

func TestGlobalTemplateDuplicateSettingsBeforeOfficialRead(t *testing.T) {
	for _, kind := range []string{"identical", "business", "plan_type"} {
		t.Run(kind, func(t *testing.T) {
			service, tokens, _, _, _ := routingTestService()
			rows := []dq.BatchItem{{InputIndex: 0, WorkURL: "https://www.douyin.com/video/" + batchWorkID(1)}, {InputIndex: 1, WorkURL: "https://v.douyin.com/" + batchWorkID(1)}}
			switch kind {
			case "business":
				rows[1].Business = "different"
			case "plan_type":
				rows[1].PlanType = "different"
			}
			r, err := service.BatchWorks(context.Background(), BatchWorksCommand{PlanTemplate: "qcpt_command", Items: rows})
			if kind == "identical" {
				if err != nil || r.PreflightID == "" || len(r.Skipped) != 1 || r.Skipped[0].Reason != "duplicate_input" {
					t.Fatalf("identical duplicate: %#v %v", r, err)
				}
				return
			}
			if !errors.Is(err, dq.ErrDuplicateItemConflict) || r.PreflightID != "" || len(tokens.queries) != 0 {
				t.Fatalf("conflict: preflight=%s queries=%d err=%v", r.PreflightID, len(tokens.queries), err)
			}
		})
	}
}

type failedRoutingMetadata struct {
	*routingTestLinks
	batchError bool
}

func (f failedRoutingMetadata) ResolveMetadata(_ context.Context, r wm.ResolveResult, _ int) (wm.ResolveResult, error) {
	if f.batchError {
		return wm.ResolveResult{}, domain.NewWorkLinkError("f2_cli_failed", "fixture")
	}
	for _, row := range r.Resolved {
		r.Skipped = append(r.Skipped, domain.SkippedWorkLink{InputIndex: row.InputIndex, InputURL: row.InputURL, AwemeItemID: row.AwemeItemID, Reason: "f2_metadata_query_failed", Status: "skipped"})
	}
	r.Resolved = nil
	return r, nil
}
func TestRoutingRecoversF2FailureOnlyThroughOfficialVerification(t *testing.T) {
	for _, tc := range []struct {
		name                                               string
		batchError, hasCreator, hasProduct, officialReject bool
	}{
		{"row_failure", false, true, true, false}, {"batch_failure", true, true, true, false}, {"official_rejection", true, true, true, true}, {"no_creator", true, false, true, false}, {"no_product", true, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, tokens, reader, links, writer := routingTestService()
			service.Links = failedRoutingMetadata{links, tc.batchError}
			item := dq.BatchItem{WorkURL: "https://www.douyin.com/video/" + batchWorkID(1)}
			if tc.hasCreator {
				item.CreatorDouyinID = batchVisibleID
			}
			if tc.hasProduct {
				item.ProductID = batchProductID
			}
			if tc.officialReject {
				reader.variant = "missing"
			}
			r, err := service.BatchWorks(context.Background(), BatchWorksCommand{Items: []dq.BatchItem{item}})
			if err != nil || reader.wrongScope != "" || writer.createCalls+writer.addCalls != 0 {
				t.Fatalf("routing error: %v %s", err, reader.wrongScope)
			}
			if !tc.hasProduct {
				if len(r.Pending) != 1 || len(tokens.queries) != 0 {
					t.Fatalf("unroutable row: %#v", r)
				}
				return
			}
			if len(r.Batches) != 1 || r.Batches[0].Result == nil {
				t.Fatalf("no child: %#v", r)
			}
			child := r.Batches[0].Result
			if !tc.hasCreator || tc.officialReject {
				if child.PreflightID != "" || len(child.Skipped) != 1 {
					t.Fatalf("invalid fallback: %#v", child)
				}
				if tc.officialReject && child.Skipped[0].Reason != "creator_identity_not_found" {
					t.Fatalf("missing official rejection: %#v", child.Skipped)
				}
				return
			}
			if child.PreflightID == "" || len(child.Results) != 1 || child.Results[0].Status != "would_create" || child.Performance.OwnerHintCache.Verified != 1 {
				t.Fatalf("missing official verification: %#v", child)
			}
		})
	}
}
