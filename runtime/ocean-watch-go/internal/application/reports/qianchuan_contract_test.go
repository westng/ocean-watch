package reports

import (
	"context"
	"errors"
	"testing"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	domainreports "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/reports"
	portreports "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/reports"
)

type failingReportSchema struct {
	*qianchuanUnifiedReaderStub
}

func (f failingReportSchema) FetchSchemas(context.Context, portreports.SchemaRequest) ([]domainreports.QianchuanSchema, error) {
	return nil, errors.New("fixture schema unavailable")
}

func TestQianchuanReportRejectsUnknownTopicBeforeCredentials(t *testing.T) {
	tokens := &qianchuanReportTokenStub{}
	service := Service{Tokens: tokens, UnifiedReader: &qianchuanUnifiedReaderStub{}}
	for _, topics := range [][]string{nil, {"AUTHOR"}} {
		if _, err := service.QianchuanSchema(t.Context(), QianchuanSchemaQuery{
			CredentialScope: CredentialScope{AdvertiserID: "1001"}, Topics: topics,
		}); err == nil {
			t.Fatal("missing or unknown schema topic was accepted")
		}
	}
	_, err := service.QianchuanCustom(t.Context(), QianchuanCustomQuery{
		CredentialScope: CredentialScope{AdvertiserID: "1001"}, StartDate: "2026-11-27", EndDate: "2026-11-27",
		DataTopic: "AUTHOR", Dimensions: []string{"author_id"}, Metrics: []string{"stat_cost"},
	})
	if err == nil || len(tokens.queries) != 0 {
		t.Fatalf("unknown topic reached credentials: %v", err)
	}
}

func TestQianchuanCustomReportRequiresCurrentAccountFields(t *testing.T) {
	for _, scenario := range []string{"dimension", "metric", "order", "filter", "schema_unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			reader := &qianchuanUnifiedReaderStub{}
			service := Service{Tokens: &qianchuanReportTokenStub{}, UnifiedReader: reader}
			query := QianchuanCustomQuery{
				CredentialScope: CredentialScope{AdvertiserID: "1001"}, StartDate: "2026-11-27", EndDate: "2026-11-27",
				DataTopic: QianchuanProductTopic, Dimensions: []string{"product_id"}, Metrics: []string{"stat_cost"},
			}
			switch scenario {
			case "dimension":
				query.Dimensions = []string{"author_id"}
			case "metric":
				query.Metrics = []string{"cost"}
			case "order":
				query.OrderField = "cost"
			case "filter":
				query.Filters = []QianchuanFilter{{Field: "author_name", Operator: 7, Values: []string{"fixture"}}}
			case "schema_unavailable":
				service.UnifiedReader = failingReportSchema{reader}
			}
			_, err := service.QianchuanCustom(t.Context(), query)
			var failure *domain.Error
			if err == nil || len(reader.dataRequests) != 0 {
				t.Fatalf("query ran without a valid schema: %v", err)
			}
			if scenario != "schema_unavailable" && (!errors.As(err, &failure) || failure.Code != "report_fields_unsupported") {
				t.Fatalf("missing field diagnostics: %v", err)
			}
		})
	}
}
