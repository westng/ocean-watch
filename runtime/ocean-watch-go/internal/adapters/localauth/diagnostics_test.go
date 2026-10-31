package localauth

import (
	"io"
	"strings"
	"testing"
	"time"
)

func TestFailedAuthorizationPageShowsTheActualStageAndOfficialCode(t *testing.T) {
	fixture := newFlowFixture(t, time.Minute)
	fixture.failDiscovery.Store(true)
	view := fixture.authorize(t, fixture.start(t, "1001"))
	if view.FailureStage != "account_sync" || view.FailureReason != "OFFICIAL_API_ERROR" || view.OfficialCode != 40002 || !view.SyncRequired {
		t.Fatalf("failed SDK response was not classified: %+v", view)
	}
	response, err := fixture.client.Get(fixture.localURL(t, view).String())
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	page, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"失败环节：同步账户", "官方接口拒绝请求", "官方错误码：40002"} {
		if !strings.Contains(string(page), expected) {
			t.Fatalf("authorization page is missing actionable detail %q", expected)
		}
	}
	for _, private := range []string{"fixture-secret", "fixture-access", "fixture-refresh", "fixture discovery denied"} {
		if strings.Contains(string(page), private) {
			t.Fatal("authorization page leaked a credential or raw provider message")
		}
	}
}
