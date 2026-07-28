package domain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAccountReportPresentationMatchesPythonGolden(t *testing.T) {
	netOrders := int64(1)
	netGMV := MustDecimal("12")
	netROI := MustDecimal("1.2")
	rows := []AccountReportRow{
		{
			Channel: Marketing, AdvertiserID: "1001", Name: "营销账户", Enabled: true,
			ChannelName: Marketing.DisplayName(), QueryStatus: "ok",
			MetricBasis: metricBasis(Marketing), Spend: MustDecimal("10"), Orders: 2,
			GMV: MustDecimal("20"), ROI: MustDecimal("2"), NetOrders1H: &netOrders,
			NetGMV1H: &netGMV, NetROI1H: &netROI,
		},
		{
			Channel: Qianchuan, AdvertiserID: "1002", Name: "千川账户", Enabled: true,
			ChannelName: Qianchuan.DisplayName(), QueryStatus: "failed",
			Error: &AccountReportFailure{
				Code: "api_error", Message: "remote failure",
				Details: map[string]any{"code": 40100, "message": "系统请求频率超限"},
			},
		},
	}
	result := NewAccountReportResult(rows, "2026-07-20", "2026-07-20")
	path := filepath.Join("..", "..", "..", "..", "contracts", "presentation", "managed-accounts-report.md")
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	wantMarkdown := strings.TrimSuffix(string(want), "\n")
	if result.Presentation.RenderedMarkdown != wantMarkdown {
		t.Fatalf("account report presentation differs from Python golden\n--- got ---\n%s\n--- want ---\n%s", result.Presentation.RenderedMarkdown, want)
	}
	if len(result.Presentation.Columns) != 13 || len(result.Presentation.RequiredSections) != 5 {
		t.Fatalf("mandatory presentation contract is incomplete: %#v", result.Presentation)
	}
}

func TestCrossChannelSummaryOnlyCombinesSpend(t *testing.T) {
	rows := []AccountReportRow{
		{Channel: Marketing, QueryStatus: "ok", Spend: MustDecimal("0.10"), GMV: MustDecimal("0.20")},
		{Channel: Qianchuan, QueryStatus: "ok", Spend: MustDecimal("0.20"), GMV: MustDecimal("0.60")},
	}
	summary := BuildAccountReportSummary(rows)
	if summary.TotalSpend.StringFixed(2) != "0.30" || summary.TotalGMV != nil || summary.WeightedROI != nil {
		t.Fatalf("cross-channel metrics were combined incorrectly: %#v", summary)
	}
	if summary.ChannelSummaries[Marketing].WeightedROI.String() != "2" ||
		summary.ChannelSummaries[Qianchuan].WeightedROI.String() != "3" {
		t.Fatalf("channel ROI summaries are incorrect: %#v", summary.ChannelSummaries)
	}
}
