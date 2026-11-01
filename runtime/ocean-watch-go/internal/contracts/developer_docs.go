package contracts

import (
	_ "embed"
	"encoding/json"
)

//go:embed catalog/developer-docs-schema-source-2026-11-01.json
var developerSchemaSourceJSON []byte

// DeveloperDocsInput is a fixed, reviewed public schema, independent of OAuth.
func DeveloperDocsInput(name string) (json.RawMessage, bool) {
	var source struct {
		Tools []struct {
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input_schema"`
		} `json:"tools"`
	}
	if json.Unmarshal(developerSchemaSourceJSON, &source) != nil {
		return nil, false
	}
	for _, tool := range source.Tools {
		if tool.Name == name {
			return append(json.RawMessage(nil), tool.Input...), true
		}
	}
	return nil, false
}
