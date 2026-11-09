package oceanengine

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/pagination"
)

const (
	authorizedAccountsEndpoint = "/oauth2/advertiser/get/"
	customerAccountsEndpoint   = "/2/customer_center/advertiser/list/"
	enterpriseAccountsEndpoint = "/2/ebp/advertiser/list/"
	advertiserInfoEndpoint     = "/2/advertiser/info/"
)

// The account discovery boundary accepts the response forms used by StudioX
// without depending on generated enum/number decoders. It uses the same SDK
// HTTP client, host guard, authorization governor, timeout and request budget.
func (client *Client) accountData(ctx context.Context, endpoint, token string, query url.Values) (any, error) {
	switch endpoint {
	case authorizedAccountsEndpoint, customerAccountsEndpoint, enterpriseAccountsEndpoint, advertiserInfoEndpoint:
	default:
		return nil, errors.New("unsupported account discovery endpoint")
	}
	if client.profile.Name != ProfileAccountDiscovery {
		return nil, errors.New("account discovery requires its dedicated client profile")
	}
	parameters := url.Values{}
	for key, values := range query {
		parameters[key] = append([]string(nil), values...)
	}
	if endpoint == authorizedAccountsEndpoint {
		parameters.Set("access_token", token)
	}
	address := url.URL{Scheme: client.profile.Scheme, Host: client.profile.Host, Path: "/open_api" + endpoint, RawQuery: parameters.Encode()}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address.String(), nil)
	if err != nil {
		return nil, discoveryFailure(endpoint, "INVALID_REQUEST", err)
	}
	if endpoint != authorizedAccountsEndpoint {
		request.Header.Set("Access-Token", token)
	}
	response, err := client.sdk.ApiClient.GetConfig().HTTPClient.Do(request)
	if err != nil {
		return nil, discoveryFailure(endpoint, "", &SDKRequestError{cause: err})
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, discoveryFailure(endpoint, "", &EnvelopeError{HTTPStatus: response.StatusCode, RetryAfter: responseRetryAfter(response)})
	}
	decoder := json.NewDecoder(response.Body)
	decoder.UseNumber()
	var envelope map[string]any
	if err := decoder.Decode(&envelope); err != nil {
		return nil, discoveryFailure(endpoint, "INVALID_RESPONSE", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, discoveryFailure(endpoint, "INVALID_RESPONSE", errors.New("unexpected trailing response data"))
	}
	code, ok := discoveryInteger(envelope["code"])
	if !ok || code < 0 {
		return nil, discoveryFailure(endpoint, "INVALID_RESPONSE", errors.New("missing official business code"))
	}
	if code != 0 {
		return nil, discoveryFailure(endpoint, "", &EnvelopeError{Code: code, HTTPStatus: response.StatusCode, Message: discoveryText(envelope["message"]), RequestID: discoveryText(envelope["request_id"]), RetryAfter: responseRetryAfter(response)})
	}
	data, exists := envelope["data"]
	if !exists || data == nil {
		return nil, discoveryFailure(endpoint, "INVALID_RESPONSE", errors.New("missing account response data"))
	}
	return data, nil
}

type accountDiscoveryFailure struct {
	endpoint, validation string
	cause                error
}

func (err *accountDiscoveryFailure) Error() string { return "authorization account discovery failed" }
func (err *accountDiscoveryFailure) Unwrap() error { return err.cause }
func (err *accountDiscoveryFailure) AuthorizationFailureContext() (string, string) {
	return err.endpoint, err.validation
}

func discoveryFailure(endpoint, validation string, cause error) error {
	var contextual interface {
		error
		AuthorizationFailureContext() (string, string)
	}
	if errors.As(cause, &contextual) {
		return cause
	}
	return &accountDiscoveryFailure{endpoint: endpoint, validation: validation, cause: cause}
}

func discoveryText(value any) string {
	switch item := value.(type) {
	case string:
		return strings.TrimSpace(item)
	case json.Number:
		return item.String()
	default:
		return ""
	}
}

func discoveryInteger(value any) (int64, bool) {
	text := discoveryText(value)
	if text == "" {
		return 0, false
	}
	number, err := strconv.ParseInt(text, 10, 64)
	return number, err == nil
}

func discoveryID(row map[string]any, keys ...string) string {
	for _, key := range keys {
		if number, ok := discoveryInteger(row[key]); ok && number > 0 {
			return strconv.FormatInt(number, 10)
		}
	}
	return ""
}

func discoveryRows(data any, keys ...string) ([]any, error) {
	if rows, ok := data.([]any); ok {
		return rows, nil
	}
	if object, ok := data.(map[string]any); ok {
		for _, key := range keys {
			if value, exists := object[key]; exists {
				rows, ok := value.([]any)
				if ok {
					return rows, nil
				}
				return nil, errors.New("account response list is invalid")
			}
		}
	}
	return nil, errors.New("account response list is missing")
}

func discoveryPage(data map[string]any, expected int, rows []string) (pagination.Page[string], error) {
	info, ok := data["page_info"].(map[string]any)
	if !ok {
		return pagination.Page[string]{}, errors.New("missing account page_info")
	}
	pages, ok := discoveryInteger(info["total_page"])
	maxInt := int64(^uint(0) >> 1)
	if !ok || pages < 0 || pages > maxInt {
		return pagination.Page[string]{}, errors.New("invalid account total_page")
	}
	page := int64(expected)
	if value, present := info["page"]; present && value != nil {
		page, ok = discoveryInteger(value)
		if !ok || page != int64(expected) && !(pages == 0 && expected == 1 && page == 0) {
			return pagination.Page[string]{}, errors.New("account page does not match request")
		}
	}
	total, unknown := int64(0), true
	if value, present := info["total_number"]; present && value != nil {
		total, ok = discoveryInteger(value)
		if !ok || total < 0 || total > maxInt {
			return pagination.Page[string]{}, errors.New("invalid account total_number")
		}
		unknown = false
	}
	if pages == 0 {
		if expected != 1 || len(rows) != 0 || total != 0 {
			return pagination.Page[string]{}, errors.New("contradictory empty account pagination")
		}
		unknown = false
	}
	return pagination.Page[string]{Number: expected, TotalPages: int(pages), TotalNumber: int(total), TotalNumberUnknown: unknown, Rows: rows}, nil
}

func encodedDiscoveryIDs(ids []string) string {
	return "[" + strings.Join(ids, ",") + "]"
}
