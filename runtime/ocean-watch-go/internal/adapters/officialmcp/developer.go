package officialmcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	docs "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/developerdocs"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/contracts"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/official"
)

var developerNames = map[string]string{"search": "open_api_doc_gen", "schema": "open_api_schema_gen", "example": "open_sdk_example_code_tool"}

type DeveloperClient struct{ base http.RoundTripper }

func NewDeveloperClient() *DeveloperClient { return &DeveloperClient{base: http.DefaultTransport} }

type developerTransport struct{ base http.RoundTripper }

func (transport developerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "https" || r.URL.Host != "open.oceanengine.com" || r.URL.User != nil || r.URL.Fragment != "" {
		return nil, docs.ErrConnection
	}
	if r.URL.Path != "/sse" && r.URL.Path != "/messages" && r.URL.Path != "/message" && r.URL.Path != "/mcp/messages" {
		return nil, docs.ErrConnection
	}
	response, err := transport.base.RoundTrip(r)
	if err == nil && response != nil && response.Body != nil {
		response.Body = &developerBody{Reader: io.LimitReader(response.Body, 8<<20), Closer: response.Body}
	}
	return response, err
}

type developerBody struct {
	io.Reader
	io.Closer
}

// Inspect is a maintainer-only metadata read. It is not a Host tool and does
// not dispatch documentation or business queries.
func (client *DeveloperClient) Inspect(ctx context.Context, metadata docs.Metadata) ([]official.Tool, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	session, tools, err := client.connect(ctx, metadata)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	result := make([]official.Tool, 0, len(developerNames))
	for _, kind := range []string{"search", "schema", "example"} {
		tool := tools[developerNames[kind]]
		if tool == nil {
			return nil, docs.ErrSchema
		}
		input, err := json.Marshal(tool.InputSchema)
		if err != nil || strings.Contains(string(input), metadata.AppID) || strings.Contains(string(input), metadata.DeveloperID) {
			return nil, docs.ErrSchema
		}
		digest := sha256.Sum256(input)
		result = append(result, official.Tool{Name: tool.Name, InputSchema: input, SourceDigest: hex.EncodeToString(digest[:])})
	}
	return result, nil
}
func (client *DeveloperClient) connect(ctx context.Context, metadata docs.Metadata) (*mcp.ClientSession, map[string]*mcp.Tool, error) {
	endpoint := "https://open.oceanengine.com/sse?" + url.Values{"app_id": {metadata.AppID}, "developer_id": {metadata.DeveloperID}}.Encode()
	httpClient := &http.Client{Transport: developerTransport{base: client.base}, CheckRedirect: func(*http.Request, []*http.Request) error { return docs.ErrConnection }}
	peer := mcp.NewClient(&mcp.Implementation{Name: "ocean-watch-developer-docs", Version: "1"}, &mcp.ClientOptions{Logger: slog.New(slog.DiscardHandler)})
	session, err := peer.Connect(ctx, &mcp.SSEClientTransport{Endpoint: endpoint, HTTPClient: httpClient}, nil)
	if err != nil {
		return nil, nil, docs.ErrConnection
	}
	tools := map[string]*mcp.Tool{}
	cursor := ""
	seen := map[string]bool{}
	for page := 0; page < 4; page++ {
		listed, err := session.ListTools(ctx, &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			_ = session.Close()
			return nil, nil, docs.ErrConnection
		}
		for _, tool := range listed.Tools {
			for _, name := range developerNames {
				if tool.Name == name {
					if tools[name] != nil {
						_ = session.Close()
						return nil, nil, docs.ErrSchema
					}
					tools[name] = tool
				}
			}
		}
		if listed.NextCursor == "" {
			return session, tools, nil
		}
		if len(listed.NextCursor) > 4096 || seen[listed.NextCursor] {
			break
		}
		seen[listed.NextCursor] = true
		cursor = listed.NextCursor
	}
	_ = session.Close()
	return nil, nil, docs.ErrSchema
}

// The mapping admits only these documented purposes and bounded scalar fields.
// Unknown required arguments never become a generic remote-tool gateway.
func developerArguments(tool *mcp.Tool, request docs.Request) (map[string]any, error) {
	if tool == nil || tool.Name != developerNames[request.Kind] {
		return nil, docs.ErrSchema
	}
	encoded, err := json.Marshal(tool.InputSchema)
	if err != nil {
		return nil, docs.ErrSchema
	}
	approved, found := contracts.DeveloperDocsInput(tool.Name)
	if !found {
		return nil, docs.ErrSchema
	}
	if _, err := CheckSchemaCompatibility(approved, encoded); err != nil {
		return nil, docs.ErrSchema
	}
	args := map[string]any{}
	switch request.Kind {
	case "search":
		args["query"] = request.Query
	case "schema":
		args["path"] = request.APIPath
	case "example":
		args["path"] = request.APIPath
		switch request.Language {
		case "go":
			args["code_type"] = "Go"
		case "java":
			args["code_type"] = "Java"
		default:
			return nil, docs.ErrSchema
		}
	default:
		return nil, docs.ErrSchema
	}
	if ValidateSchemaArguments(encoded, args) != nil {
		return nil, docs.ErrSchema
	}
	return args, nil
}
func (client *DeveloperClient) Validate(ctx context.Context, metadata docs.Metadata) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	session, tools, err := client.connect(ctx, metadata)
	if err != nil {
		return err
	}
	defer session.Close()
	for kind, name := range developerNames {
		_, err := developerArguments(tools[name], docs.Request{Kind: kind, Query: "千川官方接口", APIPath: "/open_api/v1.0/qianchuan/ad/get/", Language: "go"})
		if err != nil {
			return err
		}
	}
	return nil
}
func (client *DeveloperClient) Query(ctx context.Context, metadata docs.Metadata, request docs.Request) (docs.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	session, tools, err := client.connect(ctx, metadata)
	if err != nil {
		return docs.Result{}, err
	}
	defer session.Close()
	name := developerNames[request.Kind]
	args, err := developerArguments(tools[name], request)
	if err != nil {
		return docs.Result{}, err
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if ctx.Err() != nil {
		return docs.Result{}, &docs.Failure{Stage: "query_timeout_or_cancelled"}
	}
	if err != nil || result == nil {
		return docs.Result{}, &docs.Failure{Stage: "protocol_query"}
	}
	if result.IsError {
		return docs.Result{}, &docs.Failure{Stage: "remote_query_rejected"}
	}
	output := docs.Result{Source: "official_developer_mcp", Kind: request.Kind, Language: request.Language, Snippets: []string{}}
	remaining := 64 << 10
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			textValue := strings.ReplaceAll(strings.ReplaceAll(text.Text, metadata.AppID, "[app_id]"), metadata.DeveloperID, "[developer_id]")
			if len(textValue) > remaining {
				textValue = textValue[:remaining]
				for !utf8.ValidString(textValue) {
					textValue = textValue[:len(textValue)-1]
				}
				output.Truncated = true
			}
			output.Snippets = append(output.Snippets, textValue)
			remaining -= len(textValue)
			if remaining == 0 {
				output.Truncated = true
				break
			}
		}
	}
	if len(output.Snippets) == 0 {
		return output, docs.ErrSchema
	}
	return output, nil
}
