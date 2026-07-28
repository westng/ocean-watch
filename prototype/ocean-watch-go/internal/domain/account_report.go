package domain

import (
	"encoding/json"
	"fmt"
	"strings"
)

var MarketingMetricBasis = map[string]string{
	"spend":         "stat_cost",
	"orders":        "in_app_order_count",
	"gmv":           "in_app_order_gmv",
	"roi":           "in_app_order_roi",
	"net_orders_1h": "in_app_order_net_count_1h",
	"net_gmv_1h":    "in_app_order_net_gmv_1h",
	"net_roi_1h":    "in_app_order_net_roi_1h",
}

var QianchuanMetricBasis = map[string]string{
	"spend":  "stat_cost",
	"orders": "total_pay_order_count_for_roi2",
	"gmv":    "total_pay_order_gmv_include_coupon_for_roi2",
	"roi":    "total_prepay_and_pay_order_roi2",
}

var accountMetricOrder = []string{
	"spend", "orders", "gmv", "roi", "net_orders_1h", "net_gmv_1h", "net_roi_1h",
}

var accountMetricLabels = map[string]string{
	"spend": "消耗", "orders": "订单", "gmv": "GMV", "roi": "ROI",
	"net_orders_1h": "1h 结算订单", "net_gmv_1h": "1h 结算金额",
	"net_roi_1h": "1h 结算 ROI",
}

type AccountMetrics struct {
	MetricBasis map[string]string `json:"metric_basis"`
	Spend       Decimal           `json:"spend"`
	Orders      int64             `json:"orders"`
	GMV         Decimal           `json:"gmv"`
	ROI         Decimal           `json:"roi"`
	NetOrders1H *int64            `json:"net_orders_1h"`
	NetGMV1H    *Decimal          `json:"net_gmv_1h"`
	NetROI1H    *Decimal          `json:"net_roi_1h"`
	RequestIDs  []string          `json:"request_ids"`
}

type AccountReportFailure struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

type AccountReportRow struct {
	Channel       Channel               `json:"channel"`
	AdvertiserID  string                `json:"advertiser_id"`
	Name          string                `json:"name"`
	Enabled       bool                  `json:"enabled"`
	AuthAccountID string                `json:"auth_account_id,omitempty"`
	ChannelName   string                `json:"channel_name"`
	QueryStatus   string                `json:"query_status"`
	MetricBasis   map[string]string     `json:"metric_basis,omitempty"`
	Spend         Decimal               `json:"spend,omitempty"`
	Orders        int64                 `json:"orders,omitempty"`
	GMV           Decimal               `json:"gmv,omitempty"`
	ROI           Decimal               `json:"roi,omitempty"`
	NetOrders1H   *int64                `json:"net_orders_1h,omitempty"`
	NetGMV1H      *Decimal              `json:"net_gmv_1h,omitempty"`
	NetROI1H      *Decimal              `json:"net_roi_1h,omitempty"`
	RequestIDs    []string              `json:"request_ids,omitempty"`
	Error         *AccountReportFailure `json:"error,omitempty"`
}

func (row AccountReportRow) MarshalJSON() ([]byte, error) {
	result := map[string]any{
		"channel": row.Channel, "advertiser_id": row.AdvertiserID,
		"name": row.Name, "enabled": row.Enabled, "channel_name": row.ChannelName,
		"query_status": row.QueryStatus,
	}
	if row.AuthAccountID != "" {
		result["auth_account_id"] = row.AuthAccountID
	}
	if row.QueryStatus == "ok" {
		result["metric_basis"] = row.MetricBasis
		result["spend"] = row.Spend
		result["orders"] = row.Orders
		result["gmv"] = row.GMV
		result["roi"] = row.ROI
		result["net_orders_1h"] = row.NetOrders1H
		result["net_gmv_1h"] = row.NetGMV1H
		result["net_roi_1h"] = row.NetROI1H
		result["request_ids"] = row.RequestIDs
	} else if row.Error != nil {
		result["error"] = row.Error
	}
	return json.Marshal(result)
}

type ChannelReportSummary struct {
	AccountCount     int               `json:"account_count"`
	TotalSpend       Decimal           `json:"total_spend"`
	TotalOrders      int64             `json:"total_orders"`
	TotalGMV         Decimal           `json:"total_gmv"`
	WeightedROI      Decimal           `json:"weighted_roi"`
	TotalNetOrders1H *int64            `json:"total_net_orders_1h"`
	TotalNetGMV1H    *Decimal          `json:"total_net_gmv_1h"`
	WeightedNetROI1H *Decimal          `json:"weighted_net_roi_1h"`
	MetricBasis      map[string]string `json:"metric_basis"`
}

type AccountReportSummary struct {
	AccountCount           int                              `json:"account_count"`
	SuccessfulAccountCount int                              `json:"successful_account_count"`
	FailedAccountCount     int                              `json:"failed_account_count"`
	TotalSpend             Decimal                          `json:"total_spend"`
	TotalGMV               *Decimal                         `json:"total_gmv"`
	WeightedROI            *Decimal                         `json:"weighted_roi"`
	AggregateGMVComparable bool                             `json:"aggregate_gmv_comparable"`
	MixedChannelNote       *string                          `json:"mixed_channel_note"`
	ChannelSummaries       map[Channel]ChannelReportSummary `json:"channel_summaries"`
}

type AccountReportDateRange struct {
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
}

type AccountReportResult struct {
	OK           bool                   `json:"ok"`
	Mode         string                 `json:"mode"`
	DateRange    AccountReportDateRange `json:"date_range"`
	Summary      AccountReportSummary   `json:"summary"`
	Accounts     []AccountReportRow     `json:"accounts"`
	Presentation Presentation           `json:"presentation"`
}

var AccountReportColumns = []PresentationColumn{
	{Field: "channel_name", Label: "渠道"},
	{Field: "name", Label: "账户名称"},
	{Field: "advertiser_id", Label: "广告主 ID"},
	{Field: "enabled_label", Label: "启用状态"},
	{Field: "query_status_label", Label: "查询状态"},
	{Field: "spend", Label: "消耗"},
	{Field: "orders", Label: "订单"},
	{Field: "gmv", Label: "GMV"},
	{Field: "roi", Label: "ROI"},
	{Field: "net_orders_1h", Label: "1h 结算订单"},
	{Field: "net_gmv_1h", Label: "1h 结算金额"},
	{Field: "net_roi_1h", Label: "1h 结算 ROI"},
	{Field: "error_summary", Label: "失败原因"},
}

var channelSummaryColumns = []PresentationColumn{
	{Field: "channel_name", Label: "渠道"},
	{Field: "account_count", Label: "成功账户"},
	{Field: "total_spend", Label: "消耗"},
	{Field: "total_orders", Label: "订单"},
	{Field: "total_gmv", Label: "GMV"},
	{Field: "weighted_roi", Label: "ROI"},
	{Field: "total_net_orders_1h", Label: "1h 结算订单"},
	{Field: "total_net_gmv_1h", Label: "1h 结算金额"},
	{Field: "weighted_net_roi_1h", Label: "1h 结算 ROI"},
}

var metricBasisColumns = []PresentationColumn{
	{Field: "channel_name", Label: "渠道"},
	{Field: "metric", Label: "指标"},
	{Field: "field", Label: "官方字段"},
}

func BuildAccountReportSummary(rows []AccountReportRow) AccountReportSummary {
	successful := make([]AccountReportRow, 0, len(rows))
	for _, row := range rows {
		if row.QueryStatus == "ok" {
			successful = append(successful, row)
		}
	}
	channelSummaries := map[Channel]ChannelReportSummary{}
	for _, channel := range channelOrder {
		selected := make([]AccountReportRow, 0)
		for _, row := range successful {
			if row.Channel == channel {
				selected = append(selected, row)
			}
		}
		if len(selected) != 0 {
			channelSummaries[channel] = summarizeChannel(channel, selected)
		}
	}
	totalSpend := Decimal{}
	for _, row := range successful {
		totalSpend = totalSpend.Add(row.Spend)
	}
	comparable := len(channelSummaries) <= 1
	var totalGMV *Decimal
	var weightedROI *Decimal
	var note *string
	if comparable {
		zero := Decimal{}
		totalGMV = &zero
		weightedROI = &zero
		for _, channel := range channelOrder {
			if summary, ok := channelSummaries[channel]; ok {
				gmv := summary.TotalGMV
				roi := summary.WeightedROI
				totalGMV = &gmv
				weightedROI = &roi
				break
			}
		}
	} else {
		message := "Marketing and Qianchuan GMV/ROI use different official metric definitions."
		note = &message
	}
	return AccountReportSummary{
		AccountCount: len(rows), SuccessfulAccountCount: len(successful),
		FailedAccountCount: len(rows) - len(successful), TotalSpend: totalSpend.Round(2),
		TotalGMV: totalGMV, WeightedROI: weightedROI,
		AggregateGMVComparable: comparable, MixedChannelNote: note,
		ChannelSummaries: channelSummaries,
	}
}

func summarizeChannel(channel Channel, rows []AccountReportRow) ChannelReportSummary {
	totalSpend := Decimal{}
	totalGMV := Decimal{}
	var totalOrders int64
	var totalNetOrders int64
	totalNetGMV := Decimal{}
	for _, row := range rows {
		totalSpend = totalSpend.Add(row.Spend)
		totalGMV = totalGMV.Add(row.GMV)
		totalOrders += row.Orders
		if row.NetOrders1H != nil {
			totalNetOrders += *row.NetOrders1H
		}
		if row.NetGMV1H != nil {
			totalNetGMV = totalNetGMV.Add(*row.NetGMV1H)
		}
	}
	weightedROI := Decimal{}
	if totalSpend.Sign() != 0 {
		weightedROI, _ = totalGMV.Divide(totalSpend)
	}
	result := ChannelReportSummary{
		AccountCount: len(rows), TotalSpend: totalSpend.Round(2), TotalOrders: totalOrders,
		TotalGMV: totalGMV.Round(2), WeightedROI: weightedROI.Round(4),
		MetricBasis: metricBasis(channel),
	}
	if channel == Marketing {
		netGMV := totalNetGMV.Round(2)
		netROI := Decimal{}
		if totalSpend.Sign() != 0 {
			netROI, _ = totalNetGMV.Divide(totalSpend)
		}
		netROI = netROI.Round(4)
		result.TotalNetOrders1H = &totalNetOrders
		result.TotalNetGMV1H = &netGMV
		result.WeightedNetROI1H = &netROI
	}
	return result
}

func metricBasis(channel Channel) map[string]string {
	source := MarketingMetricBasis
	if channel == Qianchuan {
		source = QianchuanMetricBasis
	}
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func NewAccountReportResult(rows []AccountReportRow, startDate, endDate string) AccountReportResult {
	summary := BuildAccountReportSummary(rows)
	return AccountReportResult{
		OK: allAccountReportsSucceeded(rows), Mode: "managed_accounts_spend",
		DateRange: AccountReportDateRange{StartDate: startDate, EndDate: endDate},
		Summary:   summary, Accounts: rows,
		Presentation: AccountReportPresentation(summary, rows, startDate, endDate),
	}
}

func allAccountReportsSucceeded(rows []AccountReportRow) bool {
	for _, row := range rows {
		if row.QueryStatus != "ok" {
			return false
		}
	}
	return true
}

func AccountReportPresentation(
	summary AccountReportSummary,
	rows []AccountReportRow,
	startDate string,
	endDate string,
) Presentation {
	return Presentation{
		Format: "markdown", Required: true,
		AllowColumnOmission: false, AllowColumnReordering: false,
		Columns:          AccountReportColumns,
		RequiredSections: []string{"date_range", "summary", "accounts", "channel_summaries", "metric_basis"},
		RenderedMarkdown: renderAccountReport(summary, rows, startDate, endDate),
	}
}

func renderAccountReport(
	summary AccountReportSummary,
	rows []AccountReportRow,
	startDate string,
	endDate string,
) string {
	dateRange := startDate
	if startDate != endDate {
		dateRange = startDate + " 至 " + endDate
	}
	lines := []string{
		"**查询日期：** " + dateRange,
		"",
		fmt.Sprintf(
			"**负责账户汇总：** 共 %d 个；成功 %d 个；失败 %d 个；总消耗 %s",
			summary.AccountCount, summary.SuccessfulAccountCount, summary.FailedAccountCount,
			formatMoney(summary.TotalSpend),
		),
	}
	if summary.AggregateGMVComparable && len(summary.ChannelSummaries) != 0 {
		lines = append(lines, "", fmt.Sprintf(
			"**同渠道成交汇总：** GMV %s；加权 ROI %s",
			formatMoney(*summary.TotalGMV), formatRatio(*summary.WeightedROI),
		))
	} else if summary.MixedChannelNote != nil {
		lines = append(lines, "", "**跨渠道说明：** "+*summary.MixedChannelNote)
	}
	accountRows := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		accountRows = append(accountRows, presentationAccountReportRow(row))
	}
	lines = append(lines, "", "### 账户明细", "", RenderMarkdownTable(AccountReportColumns, accountRows))
	channelRows := make([]map[string]any, 0, len(summary.ChannelSummaries))
	for _, channel := range channelOrder {
		if channelSummary, ok := summary.ChannelSummaries[channel]; ok {
			channelRows = append(channelRows, map[string]any{
				"channel_name": channel.DisplayName(), "account_count": channelSummary.AccountCount,
				"total_spend":         formatMoney(channelSummary.TotalSpend),
				"total_orders":        channelSummary.TotalOrders,
				"total_gmv":           formatMoney(channelSummary.TotalGMV),
				"weighted_roi":        formatRatio(channelSummary.WeightedROI),
				"total_net_orders_1h": optionalInt(channelSummary.TotalNetOrders1H),
				"total_net_gmv_1h":    optionalMoney(channelSummary.TotalNetGMV1H),
				"weighted_net_roi_1h": optionalRatio(channelSummary.WeightedNetROI1H),
			})
		}
	}
	lines = append(lines, "", "### 分渠道汇总", "", RenderMarkdownTable(channelSummaryColumns, channelRows))
	metricRows := make([]map[string]any, 0)
	for _, channel := range channelOrder {
		if !accountRowsContainChannel(rows, channel) {
			continue
		}
		basis := metricBasis(channel)
		for _, metric := range accountMetricOrder {
			if field, ok := basis[metric]; ok {
				metricRows = append(metricRows, map[string]any{
					"channel_name": channel.DisplayName(), "metric": accountMetricLabels[metric], "field": field,
				})
			}
		}
	}
	lines = append(lines, "", "### 指标口径", "", RenderMarkdownTable(metricBasisColumns, metricRows))
	return strings.Join(lines, "\n")
}

func presentationAccountReportRow(row AccountReportRow) map[string]any {
	successful := row.QueryStatus == "ok"
	result := map[string]any{
		"channel_name": row.ChannelName, "name": row.Name, "advertiser_id": row.AdvertiserID,
		"enabled_label": enabledLabel(row.Enabled), "query_status_label": "失败",
		"error_summary": accountFailureSummary(row.Error),
	}
	if successful {
		result["query_status_label"] = "成功"
		result["spend"] = formatMoney(row.Spend)
		result["orders"] = row.Orders
		result["gmv"] = formatMoney(row.GMV)
		result["roi"] = formatRatio(row.ROI)
		result["net_orders_1h"] = optionalInt(row.NetOrders1H)
		result["net_gmv_1h"] = optionalMoney(row.NetGMV1H)
		result["net_roi_1h"] = optionalRatio(row.NetROI1H)
		result["error_summary"] = "—"
	}
	return result
}

func accountFailureSummary(failure *AccountReportFailure) string {
	if failure == nil {
		return "—"
	}
	code := strings.TrimSpace(fmt.Sprint(failure.Details["code"]))
	message := strings.TrimSpace(fmt.Sprint(failure.Details["message"]))
	if code == "<nil>" {
		code = ""
	}
	if message == "<nil>" {
		message = ""
	}
	if code == "" {
		code = failure.Code
	}
	if message == "" {
		message = failure.Message
	}
	if code != "" && message != "" {
		return code + ": " + message
	}
	if message != "" {
		return message
	}
	if code != "" {
		return code
	}
	return "—"
}

func enabledLabel(enabled bool) string {
	if enabled {
		return "已启用"
	}
	return "已停用"
}

func formatMoney(value Decimal) string { return "¥" + value.StringFixed(2) }

func formatRatio(value Decimal) string { return value.Round(4).String() }

func optionalInt(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func optionalMoney(value *Decimal) any {
	if value == nil {
		return nil
	}
	return formatMoney(*value)
}

func optionalRatio(value *Decimal) any {
	if value == nil {
		return nil
	}
	return formatRatio(*value)
}

func accountRowsContainChannel(rows []AccountReportRow, channel Channel) bool {
	for _, row := range rows {
		if row.Channel == channel {
			return true
		}
	}
	return false
}
