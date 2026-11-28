package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/contracts"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/requestcontrol"
	port "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/official"
)

const cursorAdvertiser = "9007199254740993"

type cursorGrants struct{}

func (cursorGrants) LoadChannel(context.Context, string) (map[string]any, error) {
	const grant = "fixture-authorization"
	return map[string]any{
		"authorizations":   map[string]any{grant: map[string]any{"advertiser_ids": []any{cursorAdvertiser}, "token_revision": 7}},
		"advertiser_index": map[string]any{cursorAdvertiser: []any{grant}},
	}, nil
}

func (cursorGrants) UpdateChannel(context.Context, string, func(map[string]any) error) error {
	return errors.New("unexpected authorization write")
}

type cursorPages struct{ dispatchedPage string }

func (p *cursorPages) Read(_ context.Context, req port.Request) (port.Response, error) {
	p.dispatchedPage = string(req.Arguments["page"])
	return port.Response{Data: map[string]any{
		"ad_list":   []any{map[string]any{"ad_info": map[string]any{"id": json.Number(cursorAdvertiser), "name": "fixture"}, "stats_info": map[string]any{"stat_cost": json.Number("30")}}},
		"page_info": map[string]any{"page": json.Number(p.dispatchedPage), "page_size": json.Number("1"), "total_page": json.Number("2"), "total_num": json.Number("2")},
	}}, nil
}

func TestOfficialQueryCursorContinuesAcrossProcesses(t *testing.T) {
	const rootEnv, cursorEnv = "OCEAN_WATCH_TEST_CURSOR_ROOT", "OCEAN_WATCH_TEST_CURSOR"
	root := os.Getenv(rootEnv)
	if root == "" {
		root = t.TempDir()
	}
	backend := &cursorPages{}
	runtime, err := NewQianchuanRuntime(QianchuanOptions{StateRoot: root, Authorizations: cursorGrants{}, Tokens: &fixtureProbeLeaser{}, OfficialBackend: backend})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	ctx, _, _, err := requestcontrol.PrepareCommandContext(context.Background(), 16)
	if err != nil {
		t.Fatal(err)
	}
	req := contracts.OfficialQueryRequest{
		QueryID: "qianchuan_uni_promotion_list_v1", Scope: contracts.OfficialQueryScope{Kind: "advertiser", ID: cursorAdvertiser}, Limit: 1,
		Parameters: map[string]json.RawMessage{"fields": json.RawMessage(`["stat_cost"]`), "marketing_goal": json.RawMessage(`"VIDEO_PROM_GOODS"`), "start_time": json.RawMessage(`"2026-10-31 00:00:00"`), "end_time": json.RawMessage(`"2026-10-31 23:59:59"`)},
		Cursor:     os.Getenv(cursorEnv),
	}
	page, err := runtime.Official.Query(ctx, req)
	if req.Cursor != "" {
		if os.Getenv("OCEAN_WATCH_TEST_FOREIGN_CURSOR_ROOT") == "yes" {
			var failure *port.Failure
			if !errors.As(err, &failure) || failure.Code != "INVALID_CURSOR" || backend.dispatchedPage != "" {
				t.Fatalf("foreign state accepted or dispatched: %v", err)
			}
		} else if err != nil || page.HasMore || backend.dispatchedPage != "2" {
			t.Fatalf("failed to continue page 2: %v, page %s", err, backend.dispatchedPage)
		}
		return
	}
	if err != nil || !page.HasMore || page.Cursor == "" || backend.dispatchedPage != "1" {
		t.Fatalf("expected continuation from page 1: %v", err)
	}
	for _, tc := range []struct{ root, foreign string }{{root, "no"}, {t.TempDir(), "yes"}} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestOfficialQueryCursorContinuesAcrossProcesses$")
		cmd.Env = append(os.Environ(), rootEnv+"="+tc.root, cursorEnv+"="+page.Cursor, "OCEAN_WATCH_TEST_FOREIGN_CURSOR_ROOT="+tc.foreign)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("child failed: %v\n%s", err, output)
		}
	}
}

func TestOfficialCursorKeyConcurrentInitializationAndCorruption(t *testing.T) {
	root := t.TempDir()
	type result struct {
		key [32]byte
		err error
	}
	results := make(chan result, 8)
	start := make(chan struct{})
	for range cap(results) {
		go func() {
			<-start
			key, err := officialCursorKey(root)(context.Background())
			results <- result{key, err}
		}()
	}
	close(start)
	var first [32]byte
	for i := range cap(results) {
		r := <-results
		if r.err != nil || (i > 0 && r.key != first) {
			t.Fatalf("first-use providers disagreed: %v", r.err)
		}
		first = r.key
	}
	path := filepath.Join(root, "official-cursors", "signing.key")
	if err := os.WriteFile(path, []byte("malformed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := officialCursorKey(root)(context.Background()); err == nil {
		t.Fatal("replaced malformed key")
	}
}
