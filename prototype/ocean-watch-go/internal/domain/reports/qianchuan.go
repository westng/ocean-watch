package reports

import (
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
	domainqianchuan "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/qianchuan"
)

type MaterialRow struct {
	MaterialID string
	Values     map[string]any
}

type MaterialPage struct {
	Rows      []MaterialRow
	PageInfo  domainqianchuan.PageInfo
	RequestID string
}

type PlanSchema struct {
	Topic      string
	Dimensions []string
	Metrics    []string
	RequestID  string
}

type PlanMetricRow struct {
	AdID    string
	Metrics map[string]domain.Decimal
}

type PlanMetricPage struct {
	Rows      []PlanMetricRow
	PageInfo  domainqianchuan.PageInfo
	RequestID string
}
