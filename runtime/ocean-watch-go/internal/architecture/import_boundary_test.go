package architecture

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOfficialSDKImportsStayInsideOceanEngineAdapter(t *testing.T) {
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imported := range file.Imports {
			value := strings.Trim(imported.Path.Value, `"`)
			if strings.HasPrefix(value, "github.com/oceanengine/ad_open_sdk_go") &&
				!strings.Contains(filepath.ToSlash(path), "/adapters/oceanengine/") {
				t.Errorf("official SDK import escaped adapter boundary: %s", path)
			}
			if strings.Contains(filepath.ToSlash(path), "/adapters/approvalbroker/") &&
				strings.Contains(value, "/internal/") &&
				!strings.HasSuffix(value, "/internal/domain/operations") &&
				!strings.HasSuffix(value, "/internal/ports/approval") {
				t.Errorf("approval broker imported a business, state or credential implementation: %s: %s", path, value)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
