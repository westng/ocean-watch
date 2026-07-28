package oceanengine

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	domainplans "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/plans"
	portqianchuan "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/ports/qianchuan"
)

const (
	qianchuanWriteAdvertiserID = "1000000000000001"
	qianchuanWriteAdID         = "2000000000000001"
	qianchuanWriteMaterialID   = "3000000000000001"
	qianchuanWriteToken        = "TEST_QIANCHUAN_WRITE_TOKEN_DO_NOT_USE"
)

type qianchuanWriteCall struct {
	Method string
	Path   string
	Token  string
	Body   string
}

type qianchuanWriteTransport struct {
	calls    []qianchuanWriteCall
	response func(qianchuanWriteCall) (int, string, error)
}

func (transport *qianchuanWriteTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	body := ""
	if request.Body != nil {
		payload, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		body = string(payload)
	}
	call := qianchuanWriteCall{
		Method: request.Method, Path: request.URL.Path,
		Token: request.Header.Get("Access-Token"), Body: body,
	}
	transport.calls = append(transport.calls, call)
	status, payload, err := http.StatusNotFound, `{"code":40400,"message":"unexpected synthetic route"}`, error(nil)
	if transport.response != nil {
		status, payload, err = transport.response(call)
	}
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(payload)),
		Request:    request,
	}, nil
}

func newQianchuanWriteAdapter(t *testing.T, transport http.RoundTripper) QianchuanWriteAdapter {
	t.Helper()
	factory, err := NewClientFactory(FactoryOptions{
		TransportFactory: func(HostProfile) http.RoundTripper { return transport },
	})
	if err != nil {
		t.Fatal(err)
	}
	return QianchuanWriteAdapter{Factory: factory}
}

func TestQianchuanWriteAdapterUsesGeneratedServices(t *testing.T) {
	transport := &qianchuanWriteTransport{}
	transport.response = func(call qianchuanWriteCall) (int, string, error) {
		switch call.Path {
		case "/open_api/v1.0/qianchuan/uni_aweme/ad/create/":
			return http.StatusOK, `{"code":0,"message":"OK","request_id":"create-request","data":{"ad_id":2000000000000001}}`, nil
		case "/open_api/v1.0/qianchuan/uni_promotion/ad/material/add/":
			return http.StatusOK, `{"code":0,"message":"OK","request_id":"add-request","data":{}}`, nil
		case "/open_api/v1.0/qianchuan/uni_promotion/ad/material/delete/":
			return http.StatusOK, `{"code":0,"message":"OK","request_id":"delete-request","data":{}}`, nil
		case "/open_api/v1.0/qianchuan/uni_promotion/ad/status/update/":
			return http.StatusOK, `{"code":0,"message":"OK","request_id":"status-request","data":{"results":[{"ad_id":2000000000000001,"flag":true}]}}`, nil
		case "/open_api/v1.0/qianchuan/uni_promotion/ad/budget/update/":
			return http.StatusOK, `{"code":0,"message":"OK","request_id":"budget-request","data":{"results":[{"ad_id":2000000000000001,"status":"SUCCESS"}]}}`, nil
		case "/open_api/v1.0/qianchuan/uni_promotion/ad/roi2_goal/update/":
			return http.StatusOK, `{"code":0,"message":"OK","request_id":"roi-request","data":{"results":[{"ad_id":2000000000000001,"status":"SUCCESS"}]}}`, nil
		default:
			return http.StatusNotFound, `{"code":40400,"message":"unexpected"}`, nil
		}
	}
	adapter := newQianchuanWriteAdapter(t, transport)
	ctx := testRequestContext(t, "qianchuan")
	created, err := adapter.CreatePlan(ctx, portqianchuan.CreatePlanRequest{
		AdvertiserID: qianchuanWriteAdvertiserID, AccessToken: qianchuanWriteToken,
		Payload: qianchuanCreatePayload(qianchuanWriteAdvertiserID),
	})
	if err != nil {
		t.Fatal(err)
	}
	added, err := adapter.AddMaterials(ctx, portqianchuan.MaterialWriteRequest{
		AdvertiserID: qianchuanWriteAdvertiserID, AccessToken: qianchuanWriteToken,
		AdID: qianchuanWriteAdID, Payload: qianchuanAddPayload(qianchuanWriteAdvertiserID, qianchuanWriteAdID),
	})
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := adapter.DeleteMaterials(ctx, portqianchuan.DeleteMaterialsRequest{
		AdvertiserID: qianchuanWriteAdvertiserID, AccessToken: qianchuanWriteToken,
		AdID: qianchuanWriteAdID, MaterialIDs: []string{qianchuanWriteMaterialID},
	})
	if err != nil {
		t.Fatal(err)
	}
	mutations := []portqianchuan.MutationRequest{
		{AdvertiserID: qianchuanWriteAdvertiserID, AccessToken: qianchuanWriteToken, Kind: portqianchuan.MutationStatus, AdIDs: []string{qianchuanWriteAdID}, Status: "DISABLE"},
		{AdvertiserID: qianchuanWriteAdvertiserID, AccessToken: qianchuanWriteToken, Kind: portqianchuan.MutationBudget, AdIDs: []string{qianchuanWriteAdID}, Value: "5000.25"},
		{AdvertiserID: qianchuanWriteAdvertiserID, AccessToken: qianchuanWriteToken, Kind: portqianchuan.MutationROI, AdIDs: []string{qianchuanWriteAdID}, Value: "1.75", DeepExternalAction: "AD_CONVERT_TYPE_LIVE_PAY_ROI"},
	}
	for _, mutation := range mutations {
		if _, err := adapter.UpdatePlan(ctx, mutation); err != nil {
			t.Fatal(err)
		}
	}
	if created.ObjectID != qianchuanWriteAdID || created.RequestID != "create-request" ||
		added.RequestID != "add-request" || deleted.RequestID != "delete-request" {
		t.Fatalf("Qianchuan write mapping changed: create=%#v add=%#v delete=%#v", created, added, deleted)
	}
	wantPaths := []string{
		"/open_api/v1.0/qianchuan/uni_aweme/ad/create/",
		"/open_api/v1.0/qianchuan/uni_promotion/ad/material/add/",
		"/open_api/v1.0/qianchuan/uni_promotion/ad/material/delete/",
		"/open_api/v1.0/qianchuan/uni_promotion/ad/status/update/",
		"/open_api/v1.0/qianchuan/uni_promotion/ad/budget/update/",
		"/open_api/v1.0/qianchuan/uni_promotion/ad/roi2_goal/update/",
	}
	gotPaths := make([]string, 0, len(transport.calls))
	for _, call := range transport.calls {
		gotPaths = append(gotPaths, call.Path)
		if call.Method != http.MethodPost || call.Token != qianchuanWriteToken {
			t.Fatalf("generated Qianchuan request changed: %#v", call)
		}
		var body map[string]any
		decoder := json.NewDecoder(strings.NewReader(call.Body))
		decoder.UseNumber()
		if err := decoder.Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["advertiser_id"].(json.Number).String() != qianchuanWriteAdvertiserID {
			t.Fatalf("high advertiser ID changed: %s", call.Body)
		}
	}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("Qianchuan generated service paths = %v", gotPaths)
	}
	if !strings.Contains(transport.calls[2].Body, qianchuanWriteMaterialID) {
		t.Fatalf("delete did not send material_id: %s", transport.calls[2].Body)
	}
}

func TestQianchuanWriteAdapterClassifiesDispatchBoundary(t *testing.T) {
	t.Run("payload blocked before transport", func(t *testing.T) {
		transport := &qianchuanWriteTransport{}
		adapter := newQianchuanWriteAdapter(t, transport)
		_, err := adapter.CreatePlan(testRequestContext(t, "qianchuan"), portqianchuan.CreatePlanRequest{
			AdvertiserID: qianchuanWriteAdvertiserID, AccessToken: qianchuanWriteToken,
			Payload: qianchuanCreatePayload("1000000000000002"),
		})
		assertWriteState(t, err, domainplans.DispatchNotSent)
		if len(transport.calls) != 0 {
			t.Fatal("mismatched Qianchuan payload reached transport")
		}
	})

	t.Run("unknown field blocked before transport", func(t *testing.T) {
		transport := &qianchuanWriteTransport{}
		adapter := newQianchuanWriteAdapter(t, transport)
		_, err := adapter.CreatePlan(testRequestContext(t, "qianchuan"), portqianchuan.CreatePlanRequest{
			AdvertiserID: qianchuanWriteAdvertiserID, AccessToken: qianchuanWriteToken,
			Payload: json.RawMessage(`{"advertiser_id":1000000000000001,"unknown":true}`),
		})
		assertWriteState(t, err, domainplans.DispatchNotSent)
		if len(transport.calls) != 0 {
			t.Fatal("unknown Qianchuan field reached transport")
		}
	})

	t.Run("disconnect after transport is not retried", func(t *testing.T) {
		transport := &qianchuanWriteTransport{response: func(qianchuanWriteCall) (int, string, error) {
			return 0, "", errors.New("synthetic connection reset")
		}}
		adapter := newQianchuanWriteAdapter(t, transport)
		_, err := adapter.CreatePlan(testRequestContext(t, "qianchuan"), portqianchuan.CreatePlanRequest{
			AdvertiserID: qianchuanWriteAdvertiserID, AccessToken: qianchuanWriteToken,
			Payload: qianchuanCreatePayload(qianchuanWriteAdvertiserID),
		})
		assertWriteState(t, err, domainplans.DispatchUnknown)
		if len(transport.calls) != 1 {
			t.Fatalf("unknown Qianchuan write call count = %d", len(transport.calls))
		}
	})

	t.Run("business rejection is acknowledged", func(t *testing.T) {
		transport := &qianchuanWriteTransport{response: func(qianchuanWriteCall) (int, string, error) {
			return http.StatusOK, `{"code":40000,"message":"fixture rejection","request_id":"rejected"}`, nil
		}}
		adapter := newQianchuanWriteAdapter(t, transport)
		_, err := adapter.CreatePlan(testRequestContext(t, "qianchuan"), portqianchuan.CreatePlanRequest{
			AdvertiserID: qianchuanWriteAdvertiserID, AccessToken: qianchuanWriteToken,
			Payload: qianchuanCreatePayload(qianchuanWriteAdvertiserID),
		})
		assertWriteState(t, err, domainplans.DispatchAcknowledged)
		if len(transport.calls) != 1 {
			t.Fatalf("rejected Qianchuan write call count = %d", len(transport.calls))
		}
	})

	t.Run("missing ad id remains unknown", func(t *testing.T) {
		transport := &qianchuanWriteTransport{response: func(qianchuanWriteCall) (int, string, error) {
			return http.StatusOK, `{"code":0,"message":"OK","request_id":"missing-id","data":{}}`, nil
		}}
		adapter := newQianchuanWriteAdapter(t, transport)
		_, err := adapter.CreatePlan(testRequestContext(t, "qianchuan"), portqianchuan.CreatePlanRequest{
			AdvertiserID: qianchuanWriteAdvertiserID, AccessToken: qianchuanWriteToken,
			Payload: qianchuanCreatePayload(qianchuanWriteAdvertiserID),
		})
		assertWriteState(t, err, domainplans.DispatchUnknown)
	})
}

func TestQianchuanWriteAdapterRejectsPartialMutation(t *testing.T) {
	transport := &qianchuanWriteTransport{response: func(call qianchuanWriteCall) (int, string, error) {
		if call.Path != "/open_api/v1.0/qianchuan/uni_promotion/ad/status/update/" {
			return http.StatusNotFound, `{"code":40400,"message":"unexpected"}`, nil
		}
		return http.StatusOK, `{"code":0,"message":"OK","request_id":"partial","data":{"results":[{"ad_id":2000000000000001,"flag":false,"error":{"error_code":40100,"error_message":"fixture row failed"}}]}}`, nil
	}}
	adapter := newQianchuanWriteAdapter(t, transport)
	result, err := adapter.UpdatePlan(testRequestContext(t, "qianchuan"), portqianchuan.MutationRequest{
		AdvertiserID: qianchuanWriteAdvertiserID, AccessToken: qianchuanWriteToken,
		Kind: portqianchuan.MutationStatus, AdIDs: []string{qianchuanWriteAdID}, Status: "DISABLE",
	})
	assertWriteState(t, err, domainplans.DispatchAcknowledged)
	if len(result.RowErrors) != 1 || result.RowErrors[0].ObjectID != qianchuanWriteAdID ||
		result.RowErrors[0].Code != "40100" || result.RowErrors[0].Message != "fixture row failed" {
		t.Fatalf("partial Qianchuan mutation mapping changed: %#v", result)
	}
}

func qianchuanCreatePayload(advertiserID string) json.RawMessage {
	return json.RawMessage(`{"advertiser_id":` + advertiserID + `,"name":"Fixture plan","marketing_goal":"VIDEO_PROM_GOODS","product_ids":[4000000000000001],"delivery_setting":{"smart_bid_type":"SMART_BID_CUSTOM","roi2_goal":1.75,"budget":5000,"video_schedule_type":"SCHEDULE_FROM_NOW"}}`)
}

func qianchuanAddPayload(advertiserID, adID string) json.RawMessage {
	return json.RawMessage(`{"advertiser_id":` + advertiserID + `,"ad_id":` + adID + `,"multi_product_creative_list":[]}`)
}
