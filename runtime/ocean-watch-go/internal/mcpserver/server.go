package mcpserver

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/filesystem"
	applicationtemplates "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/templates"
	domaintemplates "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/templates"
)

type Runtime struct {
	Query     applicationtemplates.Query
	LogWriter io.Writer
	Now       func() time.Time
	RequestID func() string
}

type toolFailure struct {
	Code      string
	Message   string
	Retryable bool
	Details   map[string]any
}

type errorEnvelope struct {
	OK        bool         `json:"ok"`
	RequestID string       `json:"request_id"`
	Error     errorPayload `json:"error"`
}

type errorPayload struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Retryable bool           `json:"retryable"`
	Details   map[string]any `json:"details"`
}

type listInput struct {
	Channel string `json:"channel"`
	Limit   int    `json:"limit"`
	Cursor  string `json:"cursor"`
}

type listOutput struct {
	OK           bool           `json:"ok"`
	RequestID    string         `json:"request_id"`
	StateVersion string         `json:"state_version"`
	Source       string         `json:"source"`
	TotalCount   int            `json:"total_count"`
	Items        []templateItem `json:"items"`
	NextCursor   *string        `json:"next_cursor"`
}

type templateItem struct {
	TemplateID           string  `json:"template_id"`
	Channel              string  `json:"channel"`
	TemplateKind         string  `json:"template_kind"`
	Name                 string  `json:"name"`
	Status               *string `json:"status"`
	AdvertiserID         *string `json:"advertiser_id"`
	ReadyForPlanCreation bool    `json:"ready_for_plan_creation"`
}

type getInput struct {
	Channel    string `json:"channel"`
	TemplateID string `json:"template_id"`
}

type getOutput struct {
	OK           bool           `json:"ok"`
	RequestID    string         `json:"request_id"`
	StateVersion string         `json:"state_version"`
	Source       string         `json:"source"`
	Template     templateDetail `json:"template"`
}

type templateDetail struct {
	TemplateID            string   `json:"template_id"`
	Channel               string   `json:"channel"`
	TemplateKind          string   `json:"template_kind"`
	Name                  string   `json:"name"`
	Status                *string  `json:"status"`
	ReadyForPlanCreation  bool     `json:"ready_for_plan_creation"`
	AdvertiserID          *string  `json:"advertiser_id"`
	ProductID             *string  `json:"product_id"`
	ProductIDs            []string `json:"product_ids"`
	ProductName           *string  `json:"product_name"`
	CreatorName           *string  `json:"creator_name"`
	AwemeID               *string  `json:"aweme_id"`
	MaterialSourceType    *string  `json:"material_source_type"`
	DailyBudget           *float64 `json:"daily_budget"`
	ROIGoal               *float64 `json:"roi_goal"`
	SmartBidType          *string  `json:"smart_bid_type"`
	ProjectNameTemplate   *string  `json:"project_name_template"`
	PromotionNameTemplate *string  `json:"promotion_name_template"`
	ValidationIssues      []string `json:"validation_issues"`
}

type cursorPayload struct {
	Version      int    `json:"v"`
	Channel      string `json:"channel"`
	StateVersion string `json:"state_version"`
	Offset       int    `json:"offset"`
}

func IsServeCommand(args []string) bool {
	return len(args) == 3 && args[0] == "mcp" && args[1] == "serve" && args[2] == "--stdio"
}

func RunManaged(ctx context.Context, version string) error {
	userHome, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	store, err := filesystem.NewManagedConfigStore(os.Getenv, userHome)
	if err != nil {
		return err
	}
	if err := retainManagedEnvironment(os.Getenv, os.Clearenv, os.Setenv); err != nil {
		return err
	}
	runtime := Runtime{
		Query:     applicationtemplates.Query{Store: store, VersionedStore: store},
		LogWriter: os.Stderr,
	}
	server := runtime.NewServer(version)
	err = server.Run(ctx, &mcp.StdioTransport{})
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func retainManagedEnvironment(
	getenv func(string) string,
	clearenv func(),
	setenv func(string, string) error,
) error {
	names := []string{"LANG", "LC_ALL", "LC_CTYPE"}
	if runtime.GOOS == "windows" {
		names = append(names, "SystemRoot", "WINDIR")
	}
	values := make(map[string]string, len(names))
	for _, name := range names {
		if value := getenv(name); value != "" {
			values[name] = value
		}
	}
	clearenv()
	for _, name := range names {
		if value := values[name]; value != "" {
			if err := setenv(name, value); err != nil {
				return fmt.Errorf("retain MCP runtime environment: %w", err)
			}
		}
	}
	return nil
}

func (runtime Runtime) NewServer(version string) *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{Name: "ocean-watch", Version: version},
		&mcp.ServerOptions{
			Instructions: "Use these tools only for the current OS user's local Ocean Watch template state.",
			Logger:       slog.New(slog.DiscardHandler),
			Capabilities: &mcp.ServerCapabilities{},
		},
	)
	readOnly, closedWorld := false, false
	annotations := &mcp.ToolAnnotations{
		DestructiveHint: &readOnly, IdempotentHint: true,
		OpenWorldHint: &closedWorld, ReadOnlyHint: true,
	}
	listAnnotations := *annotations
	listAnnotations.Title = "列出投放模板"
	server.AddTool(&mcp.Tool{
		Name: "list_templates", Annotations: &listAnnotations,
		Description: "当用户想查找、浏览或选择本地巨量营销或巨量千川投放模板时调用。返回可供 get_template 使用的稳定字符串 ID；不读取官方账户数据。",
		InputSchema: json.RawMessage(listInputSchema), OutputSchema: json.RawMessage(listOutputSchema),
	}, runtime.listTemplates)
	getAnnotations := *annotations
	getAnnotations.Title = "查看投放模板详情"
	server.AddTool(&mcp.Tool{
		Name: "get_template", Annotations: &getAnnotations,
		Description: "当用户已经给出或在当前会话中选中了一个本地投放模板，并需要查看安全详情或创建计划前的就绪状态时调用。必须使用渠道和精确模板 ID。",
		InputSchema: json.RawMessage(getInputSchema), OutputSchema: json.RawMessage(getOutputSchema),
	}, runtime.getTemplate)
	return server
}

func (runtime Runtime) listTemplates(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	started := runtime.now()
	requestID := runtime.requestID()
	input, failure := decodeListInput(request.Params.Arguments)
	if failure != nil {
		return runtime.failureResult(started, requestID, "list_templates", *failure), nil
	}
	versioned, err := runtime.Query.ListVersioned(ctx, input.Channel, false)
	if err != nil {
		return runtime.failureResult(started, requestID, "list_templates", mapQueryError(err)), nil
	}
	items, err := presentList(versioned.Value)
	if err != nil {
		return runtime.failureResult(started, requestID, "list_templates", toolFailure{
			Code: "CONFIG_INVALID", Message: "local template state is invalid", Details: map[string]any{},
		}), nil
	}
	offset := 0
	if input.Cursor != "" {
		cursor, cursorFailure := decodeCursor(input.Cursor, input.Channel, versioned.StateVersion, len(items))
		if cursorFailure != nil {
			return runtime.failureResult(started, requestID, "list_templates", *cursorFailure), nil
		}
		offset = cursor.Offset
	}
	end := offset + input.Limit
	if end > len(items) {
		end = len(items)
	}
	page := append([]templateItem(nil), items[offset:end]...)
	var nextCursor *string
	if end < len(items) {
		encoded, encodeErr := encodeCursor(cursorPayload{
			Version: 1, Channel: input.Channel, StateVersion: versioned.StateVersion, Offset: end,
		})
		if encodeErr != nil {
			return runtime.failureResult(started, requestID, "list_templates", internalFailure()), nil
		}
		nextCursor = &encoded
	}
	output := listOutput{
		OK: true, RequestID: requestID, StateVersion: versioned.StateVersion, Source: "local_state",
		TotalCount: len(items), Items: page, NextCursor: nextCursor,
	}
	return runtime.successResult(started, requestID, "list_templates", output), nil
}

func (runtime Runtime) getTemplate(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	started := runtime.now()
	requestID := runtime.requestID()
	input, failure := decodeGetInput(request.Params.Arguments)
	if failure != nil {
		return runtime.failureResult(started, requestID, "get_template", *failure), nil
	}
	versioned, err := runtime.Query.ShowExactVersioned(ctx, input.Channel, input.TemplateID)
	if err != nil {
		return runtime.failureResult(started, requestID, "get_template", mapQueryError(err)), nil
	}
	detail, err := presentDetail(versioned.Value)
	if err != nil {
		return runtime.failureResult(started, requestID, "get_template", toolFailure{
			Code: "CONFIG_INVALID", Message: "local template state is invalid", Details: map[string]any{},
		}), nil
	}
	output := getOutput{
		OK: true, RequestID: requestID, StateVersion: versioned.StateVersion,
		Source: "local_state", Template: detail,
	}
	return runtime.successResult(started, requestID, "get_template", output), nil
}

func decodeListInput(raw json.RawMessage) (listInput, *toolFailure) {
	input := listInput{Channel: "all", Limit: 50}
	if err := decodeStrict(raw, &input); err != nil ||
		input.Channel != "all" && input.Channel != "marketing" && input.Channel != "qianchuan" ||
		input.Limit < 1 || input.Limit > 100 || len(input.Cursor) > 512 {
		failure := invalidArgumentFailure()
		return listInput{}, &failure
	}
	return input, nil
}

func decodeGetInput(raw json.RawMessage) (getInput, *toolFailure) {
	var input getInput
	if err := decodeStrict(raw, &input); err != nil ||
		input.Channel != "marketing" && input.Channel != "qianchuan" ||
		strings.TrimSpace(input.TemplateID) != input.TemplateID || len([]rune(input.TemplateID)) < 1 || len([]rune(input.TemplateID)) > 256 {
		failure := invalidArgumentFailure()
		return getInput{}, &failure
	}
	return input, nil
}

func decodeStrict(raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("arguments must contain one JSON object")
	}
	return nil
}

func encodeCursor(cursor cursorPayload) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeCursor(value, channel, stateVersion string, total int) (cursorPayload, *toolFailure) {
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		failure := toolFailure{Code: "CURSOR_INVALID", Message: "cursor is invalid", Details: map[string]any{}}
		return cursorPayload{}, &failure
	}
	var cursor cursorPayload
	if err := decodeStrict(payload, &cursor); err != nil || cursor.Version != 1 || cursor.Channel != channel {
		failure := toolFailure{Code: "CURSOR_INVALID", Message: "cursor is invalid", Details: map[string]any{}}
		return cursorPayload{}, &failure
	}
	if cursor.StateVersion != stateVersion {
		failure := toolFailure{Code: "STATE_CHANGED", Message: "local template state changed; restart the listing", Retryable: true, Details: map[string]any{}}
		return cursorPayload{}, &failure
	}
	if cursor.Offset < 0 || cursor.Offset >= total {
		failure := toolFailure{Code: "CURSOR_INVALID", Message: "cursor is invalid", Details: map[string]any{}}
		return cursorPayload{}, &failure
	}
	return cursor, nil
}

func mapQueryError(err error) toolFailure {
	if errors.Is(err, os.ErrPermission) {
		return toolFailure{Code: "LOCAL_ACCESS_DENIED", Message: "local template state is not readable", Details: map[string]any{}}
	}
	if errors.Is(err, os.ErrNotExist) {
		return toolFailure{Code: "CONFIG_UNAVAILABLE", Message: "local template state is unavailable", Details: map[string]any{}}
	}
	if errors.Is(err, filesystem.ErrManagedConfigInvalid) {
		return toolFailure{Code: "CONFIG_INVALID", Message: "local template state is invalid", Details: map[string]any{}}
	}
	var templateError *domaintemplates.Error
	if errors.As(err, &templateError) {
		message := strings.ToLower(templateError.Message)
		if strings.Contains(message, "not found") {
			return toolFailure{Code: "TEMPLATE_NOT_FOUND", Message: "template was not found", Details: map[string]any{}}
		}
		if strings.Contains(message, "not unique") || strings.Contains(message, "does not match template_id") {
			return toolFailure{Code: "TEMPLATE_ID_CONFLICT", Message: "template ID is inconsistent or not unique", Details: map[string]any{}}
		}
		return toolFailure{Code: "CONFIG_INVALID", Message: "local template state is invalid", Details: map[string]any{}}
	}
	return internalFailure()
}

func invalidArgumentFailure() toolFailure {
	return toolFailure{Code: "INVALID_ARGUMENT", Message: "tool arguments are invalid", Details: map[string]any{}}
}

func internalFailure() toolFailure {
	return toolFailure{Code: "INTERNAL_ERROR", Message: "internal tool error", Details: map[string]any{}}
}

func (runtime Runtime) successResult(started time.Time, requestID, tool string, output any) *mcp.CallToolResult {
	runtime.log(started, requestID, tool, "ok", "")
	return resultFor(output, false)
}

func (runtime Runtime) failureResult(started time.Time, requestID, tool string, failure toolFailure) *mcp.CallToolResult {
	runtime.log(started, requestID, tool, "error", failure.Code)
	return resultFor(errorEnvelope{
		OK: false, RequestID: requestID,
		Error: errorPayload{Code: failure.Code, Message: failure.Message, Retryable: failure.Retryable, Details: failure.Details},
	}, true)
}

func resultFor(value any, isError bool) *mcp.CallToolResult {
	payload, err := json.Marshal(value)
	if err != nil {
		payload = []byte(`{"ok":false,"request_id":"serialization","error":{"code":"INTERNAL_ERROR","message":"internal tool error","retryable":false,"details":{}}}`)
		isError = true
	}
	return &mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: string(payload)}},
		StructuredContent: json.RawMessage(payload), IsError: isError,
	}
}

func (runtime Runtime) now() time.Time {
	if runtime.Now != nil {
		return runtime.Now()
	}
	return time.Now().UTC()
}

func (runtime Runtime) requestID() string {
	if runtime.RequestID != nil {
		return runtime.RequestID()
	}
	payload := make([]byte, 16)
	if _, err := rand.Read(payload); err != nil {
		return hex.EncodeToString([]byte(fmt.Sprintf("%d", runtime.now().UnixNano())))
	}
	return hex.EncodeToString(payload)
}

func (runtime Runtime) log(started time.Time, requestID, tool, status, errorCode string) {
	if runtime.LogWriter == nil {
		return
	}
	record := struct {
		Timestamp  string `json:"timestamp"`
		Level      string `json:"level"`
		RequestID  string `json:"request_id"`
		Tool       string `json:"tool"`
		DurationMS int64  `json:"duration_ms"`
		Status     string `json:"status"`
		ErrorCode  string `json:"error_code,omitempty"`
	}{
		Timestamp: runtime.now().Format(time.RFC3339Nano), Level: "info", RequestID: requestID,
		Tool: tool, DurationMS: runtime.now().Sub(started).Milliseconds(), Status: status, ErrorCode: errorCode,
	}
	_ = json.NewEncoder(runtime.LogWriter).Encode(record)
}
