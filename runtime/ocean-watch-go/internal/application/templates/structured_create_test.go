package templates

import (
	"context"
	"errors"
	"testing"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/policy"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	dt "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/templates"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/resources"
)

type createAuthorizationFixture struct{ ids []string }

func (f createAuthorizationFixture) ReadChannel(context.Context, string) (domain.AuthorizationState, error) {
	return domain.AuthorizationState{AdvertiserIDs: f.ids}, nil
}

func structuredCreatorFixture(t *testing.T) (StructuredCreator, *lifecycleStoreSpy) {
	t.Helper()
	config, err := resources.DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	store := &lifecycleStoreSpy{config: config, revision: "revision-1"}
	return StructuredCreator{Store: store, Authorizations: createAuthorizationFixture{}}, store
}

func productCreateRequest() CreateRequest {
	return CreateRequest{Channel: "qianchuan", TemplateKind: "product", SourceTemplate: "default", Product: &dt.QianchuanProductCreateInput{TemplateName: "合成商品模板", AdvertiserID: "1001", ProductName: "合成商品", ProductIDs: []any{"2001"}}}
}

func TestStructuredCreateAsksForChoicesBeforeReadingState(t *testing.T) {
	for _, tc := range []struct {
		input   CreateRequest
		missing string
	}{{CreateRequest{}, "channel"}, {CreateRequest{Channel: "qianchuan"}, "template_kind"}, {CreateRequest{Channel: "marketing"}, "marketing.material_source_type"}} {
		result, err := (StructuredCreator{}).Create(context.Background(), tc.input, CreateOptions{})
		if err != nil || result.Status != "needs_input" || result.Changed || len(result.MissingFields) != 1 || result.MissingFields[0] != tc.missing || len(result.Choices[tc.missing]) < 2 {
			t.Fatalf("missing business choice was not structured: %+v %v", result, err)
		}
	}
}

func TestStructuredProductPreviewCommitAndDrift(t *testing.T) {
	for _, scenario := range []string{"commit", "input_changed", "config_changed", "missing_reference"} {
		t.Run(scenario, func(t *testing.T) {
			creator, store := structuredCreatorFixture(t)
			input := productCreateRequest()
			preview, err := creator.Create(context.Background(), input, CreateOptions{})
			if err != nil || preview.Status != "preview" || store.casCalls != 0 || preview.Template == nil {
				t.Fatalf("preview failed or wrote state: %+v %v", preview, err)
			}
			options := CreateOptions{Submit: true, ExpectedRevision: preview.StateVersion, PreviewDigest: preview.PreviewDigest}
			switch scenario {
			case "input_changed":
				input.Product.ProductName = "不同商品"
			case "config_changed":
				store.revision = "revision-2"
			case "missing_reference":
				options.PreviewDigest = ""
			}
			result, err := creator.Create(context.Background(), input, options)
			if scenario == "commit" {
				if err != nil || result.Status != "created" || !result.Changed || store.casCalls != 1 {
					t.Fatalf("reviewed commit failed: %+v %v", result, err)
				}
			} else if err == nil || store.casCalls != 0 {
				t.Fatal("changed or unreviewed template was saved")
			}
		})
	}
}

func TestStructuredCreateUsesSameLiveAndMarketingDomainRules(t *testing.T) {
	creator, store := structuredCreatorFixture(t)
	live := CreateRequest{Channel: "qianchuan", TemplateKind: "live", SourceTemplate: "default", Live: &dt.QianchuanLiveCreateInput{AdvertiserID: "1001", CreatorName: "合成直播间", AwemeID: "3001"}}
	result, err := creator.Create(context.Background(), live, CreateOptions{})
	if err != nil || result.Status != "preview" || store.casCalls != 0 {
		t.Fatalf("live preview failed: %+v %v", result, err)
	}
	marketing := CreateRequest{Channel: "marketing", SourceTemplate: "default", Marketing: &dt.MarketingCreateInput{TemplateName: "合成营销模板", AdvertiserID: "1001", Platform: "合成平台", TrafficSource: "CID", ProductID: "2001", ProductName: "合成商品", MaterialSourceType: "ACCOUNT_UPLOAD", SelectionMode: "MANUAL", MaxMaterials: 5, Titles: []any{"合成测试商品标题"}, SellingPoints: []any{"合成商品值得推荐"}, LandingPageURL: "test+cid://path?x=1&x=2", OpenURL: "testapp://path", TrackURL: "https://track.example.test/view", ActionTrackURL: "https://track.example.test/click", PlanSource: "合成来源"}}
	result, err = creator.Create(context.Background(), marketing, CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "preview" || result.Template == nil || store.casCalls != 0 {
		t.Fatalf("Marketing did not build a side-effect-free candidate: %+v", result)
	}
	links := result.Template["overrides"].(map[string]any)["links"].(map[string]any)
	if links["landing_page_url"] != marketing.Marketing.LandingPageURL {
		t.Fatal("structured creation changed opaque CID links")
	}
	if result.Status == "needs_input" && len(result.MissingFields) == 0 {
		t.Fatal("incomplete candidate omitted required field details")
	}
	committed, err := creator.Create(context.Background(), marketing, CreateOptions{Submit: true, ExpectedRevision: result.StateVersion, PreviewDigest: result.PreviewDigest})
	if err != nil || !committed.Changed || store.casCalls != 1 {
		t.Fatalf("Marketing structured commit failed: %+v %v", committed, err)
	}
}

func TestStructuredCreateEnforcesChannelAndAdvertiserPolicy(t *testing.T) {
	creator, store := structuredCreatorFixture(t)
	input := productCreateRequest()
	input.Marketing = &dt.MarketingCreateInput{}
	if _, err := creator.Create(context.Background(), input, CreateOptions{}); err == nil {
		t.Fatal("mixed channels accepted")
	}
	input = productCreateRequest()
	creator.Authorizations = createAuthorizationFixture{ids: []string{"9999"}}
	if _, err := creator.Create(context.Background(), input, CreateOptions{}); err == nil {
		t.Fatal("foreign authorization scope accepted")
	}
	instance, _ := policy.New(policy.Config{Mode: "plan", Toolsets: []string{"core"}, AllowedSubjects: map[string]map[string][]string{"qianchuan": {"advertiser": {"9999"}}}})
	creator.Policy = &instance
	if _, err := creator.Create(context.Background(), input, CreateOptions{}); !errors.Is(err, policy.ErrDenied) {
		t.Fatalf("JSON advertiser bypassed policy: %v", err)
	}
	if store.casCalls != 0 {
		t.Fatal("rejected input mutated configuration")
	}
}
