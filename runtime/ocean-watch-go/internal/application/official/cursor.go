package official

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/contracts"
)

// Services constructed directly without a state root retain process-local cursors.
var processCursorKey = func() [32]byte {
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		panic("cannot initialize query cursor signing")
	}
	return key
}()

func (service Service) cursorKey(ctx context.Context) ([32]byte, error) {
	if service.CursorKey == nil {
		return processCursorKey, nil
	}
	return service.CursorKey(ctx)
}

type pageCursor struct {
	Version  int    `json:"v"`
	Binding  string `json:"binding"`
	Page     int    `json:"page"`
	Offset   int    `json:"offset"`
	Expires  int64  `json:"expires"`
	Upstream string `json:"upstream,omitempty"`
}

func queryBinding(query contracts.ApprovedOfficialQuery, request contracts.OfficialQueryRequest, authorization string) string {
	data, _ := json.Marshal(struct {
		Contract, Query, Authorization, AuthAccount string
		Scope                                       contracts.OfficialQueryScope
		Parameters                                  map[string]json.RawMessage
		Limit                                       int
	}{query.Policy.ContractFingerprint, request.QueryID, authorization, request.AuthAccountID, request.Scope, request.Parameters, request.Limit})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func encodeQueryCursor(key [32]byte, query contracts.ApprovedOfficialQuery, request contracts.OfficialQueryRequest, authorization string, page, offset int, upstream string) string {
	data, _ := json.Marshal(pageCursor{Version: 1, Binding: queryBinding(query, request, authorization), Page: page, Offset: offset, Expires: time.Now().Add(time.Hour).Unix(), Upstream: upstream})
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write(data)
	return base64.RawURLEncoding.EncodeToString(data) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func decodeQueryCursorState(key [32]byte, query contracts.ApprovedOfficialQuery, request contracts.OfficialQueryRequest, authorization string) (pageCursor, error) {
	parts := strings.Split(request.Cursor, ".")
	if len(parts) != 2 {
		return pageCursor{}, errors.New("invalid query cursor")
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return pageCursor{}, err
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return pageCursor{}, err
	}
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write(data)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return pageCursor{}, errors.New("invalid query cursor")
	}
	var cursor pageCursor
	if json.Unmarshal(data, &cursor) != nil || cursor.Version != 1 || cursor.Binding != queryBinding(query, request, authorization) || cursor.Page < 1 || cursor.Page > 10000 || cursor.Offset < 0 || cursor.Offset > 1000 || cursor.Expires < time.Now().Unix() {
		return pageCursor{}, errors.New("invalid query cursor")
	}
	if len(cursor.Upstream) > 4096 {
		return pageCursor{}, errors.New("invalid query cursor")
	}
	return cursor, nil
}

func catalogBinding(request CatalogQuery) string {
	data, _ := json.Marshal([]any{request.QueryID, request.Group, request.Keyword, request.Limit, contracts.OfficialQueryContractVersion})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func encodeCatalogCursor(request CatalogQuery, offset int) string {
	data, _ := json.Marshal(struct {
		Binding string `json:"binding"`
		Offset  int    `json:"offset"`
	}{catalogBinding(request), offset})
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeCatalogCursor(request CatalogQuery) (int, error) {
	if request.Cursor == "" {
		return 0, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(request.Cursor)
	if err != nil {
		return 0, err
	}
	var cursor struct {
		Binding string `json:"binding"`
		Offset  int    `json:"offset"`
	}
	if json.Unmarshal(data, &cursor) != nil || cursor.Binding != catalogBinding(request) || cursor.Offset < 0 {
		return 0, errors.New("invalid catalog cursor")
	}
	return cursor.Offset, nil
}

type taskCursor struct {
	Version int    `json:"v"`
	Binding string `json:"binding"`
	Offset  int    `json:"offset"`
	Expires int64  `json:"expires"`
}

func encodeTaskCursor(key [32]byte, binding string, offset int) string {
	data, _ := json.Marshal(taskCursor{Version: 1, Binding: binding, Offset: offset, Expires: time.Now().Add(time.Hour).Unix()})
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write(data)
	return base64.RawURLEncoding.EncodeToString(data) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func decodeTaskCursor(key [32]byte, binding, encoded string, total int) (int, error) {
	if encoded == "" {
		return 0, nil
	}
	parts := strings.Split(encoded, ".")
	if len(parts) != 2 || len(encoded) > 4096 {
		return 0, errors.New("invalid task cursor")
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return 0, errors.New("invalid task cursor")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0, errors.New("invalid task cursor")
	}
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write(data)
	var cursor taskCursor
	if !hmac.Equal(signature, mac.Sum(nil)) || json.Unmarshal(data, &cursor) != nil || cursor.Version != 1 || cursor.Binding != binding || cursor.Offset < 0 || cursor.Offset >= total || cursor.Expires <= time.Now().Unix() {
		return 0, errors.New("invalid task cursor")
	}
	return cursor.Offset, nil
}
