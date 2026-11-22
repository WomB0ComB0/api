package media_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHexagonalArchitectureBoundaries enforces dependency inversion rules via Go AST inspection
// (inspired by go-hexagonal/util/clean_arch):
//
//   - internal/domain/**      must NEVER import application, adapter, api, pgx, aws-sdk, or net/http
//   - internal/application/** must NEVER import adapter or api
//   - internal/api/**         must NEVER import adapter (only application, domain, config, pkg)
func TestHexagonalArchitectureBoundaries(t *testing.T) {
	fset := token.NewFileSet()

	forbiddenByLayer := map[string][]string{
		"internal/domain": {
			"api/media/internal/application",
			"api/media/internal/adapter",
			"api/media/internal/api",
			"github.com/jackc/pgx",
			"github.com/aws/aws-sdk-go-v2",
			"github.com/go-chi/chi",
			"net/http",
		},
		"internal/application": {
			"api/media/internal/adapter",
			"api/media/internal/api",
			"github.com/jackc/pgx",
			"github.com/aws/aws-sdk-go-v2",
			"github.com/go-chi/chi",
		},
		"internal/api": {
			"api/media/internal/adapter",
			"github.com/jackc/pgx",
			"github.com/aws/aws-sdk-go-v2",
		},
	}

	err := filepath.Walk("internal", func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return walkErr
		}

		slashPath := filepath.ToSlash(path)
		fileAST, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("failed to parse %s: %v", path, err)
		}

		for layerPrefix, forbidden := range forbiddenByLayer {
			if !strings.HasPrefix(slashPath, layerPrefix+"/") {
				continue
			}
			for _, imp := range fileAST.Imports {
				impPath := strings.Trim(imp.Path.Value, `"`)
				for _, banned := range forbidden {
					if impPath == banned || strings.HasPrefix(impPath, banned+"/") {
						t.Errorf("hexagonal violation in %s: layer %q must not import %q", slashPath, layerPrefix, impPath)
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk failed: %v", err)
	}
}
