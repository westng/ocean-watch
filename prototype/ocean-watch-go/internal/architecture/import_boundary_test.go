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
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
