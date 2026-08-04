package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
)

type wizardStoreSpy struct {
	config      map[string]any
	revision    string
	reads       int
	casCalls    int
	casRevision string
	written     map[string]any
	compareErr  error
}

func (store *wizardStoreSpy) Read(context.Context) (map[string]any, error) {
	store.reads++
	return store.config, nil
}

func (store *wizardStoreSpy) ReadWithRevision(context.Context) (map[string]any, string, error) {
	store.reads++
	return store.config, store.revision, nil
}

func (store *wizardStoreSpy) CompareAndSwap(_ context.Context, revision string, updated map[string]any) error {
	store.casCalls++
	store.casRevision = revision
	store.written = updated
	return store.compareErr
}

type authorizationReaderStub struct {
	state domain.AuthorizationState
}

func (reader authorizationReaderStub) ReadChannel(context.Context, string) (domain.AuthorizationState, error) {
	return reader.state, nil
}

func TestQianchuanTemplateWizardCancellationIsWriteFree(t *testing.T) {
	store := &wizardStoreSpy{config: map[string]any{"future": "preserved"}, revision: "r1"}
	stdout := new(bytes.Buffer)
	code := RunQianchuanTemplatesInteractive(
		context.Background(), "create", nil, store, authorizationReaderStub{},
		"/synthetic/config.json",
		strings.NewReader("0\n2000000000000101\n测试商品全称\n测试商品\n8000000000000101\n用户模板\n\nn\n"), stdout,
	)
	if code != 0 {
		t.Fatalf("got exit %d: %s", code, stdout.String())
	}
	if store.reads != 1 || store.casCalls != 0 || store.config["future"] != "preserved" {
		t.Fatalf("canceled wizard mutated state: %#v", store)
	}
}

func TestOpaqueValuePreservesCIDLinkCharacters(t *testing.T) {
	want := "  custom+cid://opaque/path?repeat=1&repeat=2&TODO=保留&encoded=%2f%2F#Fragment  "
	reader := newPromptReader(strings.NewReader(want+"\n"), new(bytes.Buffer))
	got, err := reader.opaqueValue("CID 链接", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("opaque CID input changed: got %q want %q", got, want)
	}
}

func TestQianchuanTemplateWizardConfirmationUsesOneCAS(t *testing.T) {
	store := &wizardStoreSpy{config: map[string]any{"future": "preserved"}, revision: "captured-revision"}
	stdout := new(bytes.Buffer)
	code := RunQianchuanTemplatesInteractive(
		context.Background(), "create", nil, store, authorizationReaderStub{},
		"/synthetic/config.json",
		strings.NewReader("0\n2000000000000101\n测试商品全称\n测试商品\n8000000000000101\n用户模板\n\ny\n"), stdout,
	)
	if code != 0 {
		t.Fatalf("got exit %d: %s", code, stdout.String())
	}
	if store.reads != 1 || store.casCalls != 1 || store.casRevision != "captured-revision" {
		t.Fatalf("confirmed wizard did not use one captured CAS: %#v", store)
	}
	if store.written == nil || store.written["future"] != "preserved" {
		t.Fatalf("confirmed wizard lost unknown config fields: %#v", store.written)
	}
	templates := store.written["qianchuan_product_templates"].(map[string]any)
	if len(templates) != 1 {
		t.Fatalf("confirmed wizard wrote unexpected templates: %#v", templates)
	}
	var bindings map[string]any
	for _, value := range templates {
		bindings = value.(map[string]any)["bindings"].(map[string]any)
	}
	if bindings["product_name"] != "测试商品全称" || bindings["product_short_name"] != "测试商品" {
		t.Fatalf("confirmed wizard did not preserve product names: %#v", bindings)
	}
}

func TestQianchuanTemplateWizardConflictDoesNotRetryOrOverwrite(t *testing.T) {
	store := &wizardStoreSpy{
		config: map[string]any{"future": "newer-value"}, revision: "stale-revision",
		compareErr: errors.New("configuration changed while this operation was running; reload and retry"),
	}
	stdout := new(bytes.Buffer)
	code := RunQianchuanTemplatesInteractive(
		context.Background(), "create", nil, store, authorizationReaderStub{},
		"/synthetic/config.json",
		strings.NewReader("0\n2000000000000101\n测试商品全称\n测试商品\n8000000000000101\n用户模板\n\ny\n"), stdout,
	)
	if code != 2 {
		t.Fatalf("got exit %d: %s", code, stdout.String())
	}
	if store.reads != 1 || store.casCalls != 1 || store.config["future"] != "newer-value" {
		t.Fatalf("conflicting wizard retried or overwrote state: %#v", store)
	}
	if !strings.Contains(stdout.String(), "configuration changed while this operation was running") {
		t.Fatalf("conflict was not surfaced: %s", stdout.String())
	}
}

func TestQianchuanTemplateWizardRejectsAdvertiserOutsideAuthorizationIndex(t *testing.T) {
	store := &wizardStoreSpy{config: map[string]any{}, revision: "r1"}
	authorizations := authorizationReaderStub{state: domain.AuthorizationState{
		AuthorizationCount: 1,
		AdvertiserIDs:      []string{"2000000000000101", "2000000000000102"},
	}}
	stdout := new(bytes.Buffer)
	code := RunQianchuanTemplatesInteractive(
		context.Background(), "create", nil, store, authorizations,
		"/synthetic/config.json",
		strings.NewReader("0\n2999999999999999\n2000000000000102\n测试商品全称\n测试商品\n8000000000000101\n用户模板\n\nn\n"), stdout,
	)
	if code != 0 {
		t.Fatalf("got exit %d: %s", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "广告主 2999999999999999 不在当前巨量千川授权范围内") {
		t.Fatalf("out-of-scope advertiser was not rejected: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"advertiser_id": "2000000000000102"`) {
		t.Fatalf("authorized replacement was not used: %s", stdout.String())
	}
	if store.casCalls != 0 {
		t.Fatalf("canceled authorization test wrote state: %#v", store)
	}
}
