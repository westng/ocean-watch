package localauth

import (
	"strings"
	"testing"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
)

func TestSavedAuthorizationPageIdentifiesAccountFailureAndPartialCoverage(t *testing.T) {
	view := auth.RecoverySession{Status: "failed", SyncRequired: true, ErrorCode: "ACCOUNT_SYNC_FAILED", FailureStage: "account_sync", FailureReason: "ACCOUNT_DISCOVERY_FAILED",
		FailureEndpoint: "/2/customer_center/advertiser/list/", FailureValidation: "INVALID_PAGINATION"}
	if !strings.Contains(recoveryStatusText(view), "授权已保存") {
		t.Fatal("a saved authorization was displayed as failed OAuth")
	}
	message := recoveryErrorMessage(view)
	if !strings.Contains(message, "客户中心广告主列表") || !strings.Contains(message, "分页信息") {
		t.Fatal("the page did not identify the failed account endpoint and validation")
	}
	view.FailureEndpoint, view.FailureValidation = "/2/advertiser/info/?secret=fixture-private-secret", "fixture-private-secret"
	if strings.Contains(recoveryErrorMessage(view), "fixture-private-secret") {
		t.Fatal("untrusted diagnostic strings reached the page")
	}
	if !strings.Contains(recoveryStatusText(auth.RecoverySession{Status: "completed", DiscoveryIssueCount: 1}), "部分账户角色未展开") {
		t.Fatal("the page hid incomplete role coverage")
	}
}
