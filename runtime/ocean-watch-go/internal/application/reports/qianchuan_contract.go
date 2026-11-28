package reports

import (
	"encoding/json"
	"errors"
	"slices"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/contracts"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	domainreports "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/reports"
)

// Use the frozen official contract for topic discovery and request validation.
func QianchuanReportTopics() ([]string, error) {
	query, found, err := contracts.ApprovedOfficialQueryByID("qianchuan_report_uni_promotion_config_get_v1")
	if err != nil {
		return nil, err
	}
	var input struct {
		Properties map[string]struct {
			Items struct{ Enum []string }
		}
	}
	if !found || json.Unmarshal(query.SourceInput, &input) != nil || len(input.Properties["data_topics"].Items.Enum) == 0 {
		return nil, errors.New("Qianchuan report topic contract is unavailable")
	}
	return input.Properties["data_topics"].Items.Enum, nil
}

func validateQianchuanReportTopics(topics []string) error {
	allowed, err := QianchuanReportTopics()
	if err != nil {
		return err
	}
	if len(topics) == 0 {
		return domain.NewError("report_topics_required", "choose a report topic from the published contract", 2, nil)
	}
	for _, topic := range topics {
		if !slices.Contains(allowed, topic) {
			return domain.NewError("report_topic_unsupported", "report topic is outside the published contract", 2, nil)
		}
	}
	return nil
}

func validateQianchuanReportFields(query QianchuanCustomQuery, schemas []domainreports.QianchuanSchema) error {
	if len(schemas) != 1 || schemas[0].Topic != query.DataTopic {
		return domain.NewError("report_schema_incomplete", "official report schema did not contain the requested topic", 1, nil)
	}
	schema := schemas[0]
	var unsupported []string
	for _, field := range query.Dimensions {
		if !slices.Contains(schema.Dimensions, field) {
			unsupported = append(unsupported, field)
		}
	}
	for _, field := range query.Metrics {
		if !slices.Contains(schema.Metrics, field) {
			unsupported = append(unsupported, field)
		}
	}
	if !slices.Contains(schema.Dimensions, query.OrderField) && !slices.Contains(schema.Metrics, query.OrderField) {
		unsupported = append(unsupported, query.OrderField)
	}
	for _, filter := range query.Filters {
		if !slices.Contains(schema.Dimensions, filter.Field) && !slices.Contains(schema.Metrics, filter.Field) {
			unsupported = append(unsupported, filter.Field)
		}
	}
	if len(unsupported) > 0 {
		return domain.NewError("report_fields_unsupported", "choose dimensions and metrics returned by the account report schema", 2, map[string]any{"unsupported_fields": uniqueNonEmpty(unsupported)})
	}
	return nil
}
