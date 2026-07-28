package oceanengine

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	domainplans "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/plans"
	portmarketing "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/ports/marketing"
)

func TestMarketingMutationAdapterUsesGeneratedServicesAndReadback(t *testing.T) {
	tests := []struct {
		name          string
		kind          portmarketing.MutationKind
		objectID      string
		status        string
		value         string
		writePath     string
		readPath      string
		writeData     string
		readRow       string
		expectedField string
		expectedValue string
	}{
		{
			name: "project status", kind: portmarketing.MutationProjectStatus,
			objectID: marketingPlanProjectID, status: "DISABLE",
			writePath:     "/open_api/v3.0/project/status/update/",
			readPath:      "/open_api/v3.0/project/list/",
			writeData:     `{"project_ids":[9007199254740991],"errors":[]}`,
			readRow:       `{"project_id":9007199254740991,"opt_status":"DISABLE"}`,
			expectedField: "opt_status", expectedValue: "DISABLE",
		},
		{
			name: "promotion status", kind: portmarketing.MutationPromotionStatus,
			objectID: marketingPlanPromotionID, status: "ENABLE",
			writePath:     "/open_api/v3.0/promotion/status/update/",
			readPath:      "/open_api/v3.0/promotion/list/",
			writeData:     `{"promotion_ids":[9007199254740993],"errors":[]}`,
			readRow:       `{"promotion_id":9007199254740993,"opt_status":"ENABLE"}`,
			expectedField: "opt_status", expectedValue: "ENABLE",
		},
		{
			name: "promotion budget", kind: portmarketing.MutationPromotionBudget,
			objectID: marketingPlanPromotionID, value: "500.25",
			writePath:     "/open_api/v3.0/promotion/budget/update/",
			readPath:      "/open_api/v3.0/promotion/list/",
			writeData:     `{"promotion_ids":[9007199254740993],"errors":[]}`,
			readRow:       `{"promotion_id":9007199254740993,"budget":500.25}`,
			expectedField: "budget", expectedValue: "500.25",
		},
		{
			name: "promotion bid", kind: portmarketing.MutationPromotionBid,
			objectID: marketingPlanPromotionID, value: "2.35",
			writePath:     "/open_api/v3.0/promotion/bid/update/",
			readPath:      "/open_api/v3.0/promotion/list/",
			writeData:     `{"promotion_ids":[9007199254740993],"errors":[]}`,
			readRow:       `{"promotion_id":9007199254740993,"bid":2.35}`,
			expectedField: "bid", expectedValue: "2.35",
		},
		{
			name: "project ROI", kind: portmarketing.MutationProjectROI,
			objectID: marketingPlanProjectID, value: "1.7",
			writePath:     "/open_api/v3.0/project/roigoal/update/",
			readPath:      "/open_api/v3.0/project/list/",
			writeData:     `{"project_ids":[9007199254740991],"errors":[]}`,
			readRow:       `{"project_id":9007199254740991,"delivery_setting":{"roi_goal":1.7}}`,
			expectedField: "roi_goal", expectedValue: "1.7",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transport := &marketingPlanTransport{}
			transport.response = func(call marketingPlanCall) (int, string, error) {
				switch call.Path {
				case test.writePath:
					return http.StatusOK, `{"code":0,"message":"OK","request_id":"mutation-request","data":` + test.writeData + `}`, nil
				case test.readPath:
					return http.StatusOK, marketingMutationReadFixture(test.readRow), nil
				default:
					return http.StatusNotFound, `{"code":40400,"message":"unexpected"}`, nil
				}
			}
			adapter := newMarketingPlanAdapter(t, transport)
			request := portmarketing.MutationRequest{
				AdvertiserID: marketingPlanAdvertiserID, AccessToken: marketingPlanToken,
				Kind: test.kind, ObjectIDs: []string{test.objectID},
				Status: test.status, Value: test.value,
			}
			writeResult, err := adapter.ApplyMutation(testRequestContext(t, "marketing"), request)
			if err != nil {
				t.Fatal(err)
			}
			snapshots, err := adapter.ReadMutation(testRequestContext(t, "marketing"), request)
			if err != nil {
				t.Fatal(err)
			}
			if writeResult.RequestID != "mutation-request" || len(writeResult.RowErrors) != 0 ||
				len(snapshots) != 1 || snapshots[0].ObjectID != test.objectID || len(transport.calls) != 2 {
				t.Fatalf("mutation mapping changed: write=%#v read=%#v calls=%#v", writeResult, snapshots, transport.calls)
			}
			observed := snapshots[0].Value
			if test.status != "" {
				observed = snapshots[0].Status
			}
			if observed != test.expectedValue {
				t.Fatalf("readback value = %q, want %q", observed, test.expectedValue)
			}
			assertMarketingMutationWriteCall(t, transport.calls[0], test, request)
			assertMarketingMutationReadCall(t, transport.calls[1], test)
		})
	}
}

func TestMarketingMutationAdapterMapsPartialErrors(t *testing.T) {
	secondID := "9007199254740994"
	transport := &marketingPlanTransport{response: func(call marketingPlanCall) (int, string, error) {
		if call.Path != "/open_api/v3.0/promotion/budget/update/" {
			return http.StatusNotFound, `{"code":40400,"message":"unexpected"}`, nil
		}
		return http.StatusOK, `{"code":0,"message":"OK","request_id":"partial","data":{"promotion_ids":[9007199254740993],"errors":[{"promotion_id":9007199254740994,"error_message":"fixture rejection"}]}}`, nil
	}}
	result, err := newMarketingPlanAdapter(t, transport).ApplyMutation(
		testRequestContext(t, "marketing"),
		portmarketing.MutationRequest{
			AdvertiserID: marketingPlanAdvertiserID, AccessToken: marketingPlanToken,
			Kind:      portmarketing.MutationPromotionBudget,
			ObjectIDs: []string{marketingPlanPromotionID, secondID}, Value: "500",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.RowErrors, map[string]string{secondID: "fixture rejection"}) ||
		len(transport.calls) != 1 {
		t.Fatalf("partial row errors changed: result=%#v calls=%#v", result, transport.calls)
	}
}

func TestMarketingMutationAdapterNeverRetriesUnknownWrite(t *testing.T) {
	transport := &marketingPlanTransport{response: func(marketingPlanCall) (int, string, error) {
		return 0, "", errors.New("synthetic disconnect after dispatch")
	}}
	_, err := newMarketingPlanAdapter(t, transport).ApplyMutation(
		testRequestContext(t, "marketing"),
		portmarketing.MutationRequest{
			AdvertiserID: marketingPlanAdvertiserID, AccessToken: marketingPlanToken,
			Kind:      portmarketing.MutationProjectStatus,
			ObjectIDs: []string{marketingPlanProjectID}, Status: "DISABLE",
		},
	)
	assertWriteState(t, err, domainplans.DispatchUnknown)
	if len(transport.calls) != 1 {
		t.Fatalf("unknown mutation write was retried %d times", len(transport.calls))
	}
}

func assertMarketingMutationWriteCall(
	t *testing.T,
	call marketingPlanCall,
	test struct {
		name          string
		kind          portmarketing.MutationKind
		objectID      string
		status        string
		value         string
		writePath     string
		readPath      string
		writeData     string
		readRow       string
		expectedField string
		expectedValue string
	},
	request portmarketing.MutationRequest,
) {
	t.Helper()
	if call.Method != http.MethodPost || call.Path != test.writePath || call.Token != marketingPlanToken {
		t.Fatalf("generated mutation write changed: %#v", call)
	}
	var body map[string]any
	decoder := json.NewDecoder(strings.NewReader(call.Body))
	decoder.UseNumber()
	if err := decoder.Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["advertiser_id"].(json.Number).String() != marketingPlanAdvertiserID {
		t.Fatalf("mutation advertiser ID changed: %s", call.Body)
	}
	data := body["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("mutation request row count changed: %#v", data)
	}
	row := data[0].(map[string]any)
	idField := "promotion_id"
	if request.Kind == portmarketing.MutationProjectStatus || request.Kind == portmarketing.MutationProjectROI {
		idField = "project_id"
	}
	if row[idField].(json.Number).String() != test.objectID {
		t.Fatalf("mutation object ID changed: %#v", row)
	}
	value, exists := row[test.expectedField]
	if !exists {
		t.Fatalf("mutation request field %q is missing: %#v", test.expectedField, row)
	}
	if number, ok := value.(json.Number); ok {
		value = number.String()
	}
	if value != test.expectedValue {
		t.Fatalf("mutation request value = %#v, want %q", value, test.expectedValue)
	}
}

func assertMarketingMutationReadCall(
	t *testing.T,
	call marketingPlanCall,
	test struct {
		name          string
		kind          portmarketing.MutationKind
		objectID      string
		status        string
		value         string
		writePath     string
		readPath      string
		writeData     string
		readRow       string
		expectedField string
		expectedValue string
	},
) {
	t.Helper()
	if call.Method != http.MethodGet || call.Path != test.readPath || call.Token != marketingPlanToken ||
		call.Query.Get("page") != "1" || call.Query.Get("page_size") != "100" {
		t.Fatalf("generated mutation readback changed: %#v", call)
	}
	var filtering map[string]any
	decoder := json.NewDecoder(strings.NewReader(call.Query.Get("filtering")))
	decoder.UseNumber()
	if err := decoder.Decode(&filtering); err != nil {
		t.Fatal(err)
	}
	ids := filtering["ids"].([]any)
	if len(ids) != 1 || ids[0].(json.Number).String() != test.objectID {
		t.Fatalf("mutation readback was not scoped by exact ID: %#v", filtering)
	}
	var fields []string
	if err := json.Unmarshal([]byte(call.Query.Get("fields")), &fields); err != nil {
		t.Fatal(err)
	}
	if !containsString(fields, test.expectedField) &&
		!(test.kind == portmarketing.MutationProjectROI && containsString(fields, "delivery_setting")) {
		t.Fatalf("mutation readback fields changed: %v", fields)
	}
}

func marketingMutationReadFixture(row string) string {
	return `{"code":0,"message":"OK","request_id":"readback","data":{"list":[` + row +
		`],"page_info":{"page":1,"page_size":100,"total_page":1,"total_number":1}}}`
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
