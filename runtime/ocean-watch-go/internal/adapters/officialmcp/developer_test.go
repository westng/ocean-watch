package officialmcp

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	docs "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/developerdocs"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/contracts"
)

func TestDeveloperMappingsUseCapturedExactSchemasAndExplicitStyle(t *testing.T) {
	for _, test := range []struct{ kind, language, key, want string }{{"search", "", "query", "fixture"}, {"schema", "", "path", "/open_api/v1.0/qianchuan/ad/get/"}, {"example", "go", "code_type", "Go"}, {"example", "java", "code_type", "Java"}} {
		input, _ := contracts.DeveloperDocsInput(developerNames[test.kind])
		tool := &mcp.Tool{Name: developerNames[test.kind], InputSchema: input}
		args, err := developerArguments(tool, docs.Request{Kind: test.kind, Query: "fixture", APIPath: "/open_api/v1.0/qianchuan/ad/get/", Language: test.language})
		if err != nil || args[test.key] != test.want {
			t.Fatalf("fixed mapping %s/%s failed", test.kind, test.language)
		}
		if args["app_id"] != nil || args["developer_id"] != nil || args["Access-Token"] != nil {
			t.Fatal("metadata or OAuth became remote arguments")
		}
	}
	input, _ := contracts.DeveloperDocsInput(developerNames["example"])
	tool := &mcp.Tool{Name: developerNames["example"], InputSchema: input}
	if _, err := developerArguments(tool, docs.Request{Kind: "example", Language: "python"}); !errors.Is(err, docs.ErrSchema) {
		t.Fatal("unverified language accepted")
	}
	var schema map[string]any
	_ = json.Unmarshal(input, &schema)
	schema["required"] = []string{"path", "code_type", "new_authority"}
	tool.InputSchema = schema
	if _, err := developerArguments(tool, docs.Request{Kind: "example", Language: "go"}); !errors.Is(err, docs.ErrSchema) {
		t.Fatal("new required authority bypassed frozen schema")
	}
}
