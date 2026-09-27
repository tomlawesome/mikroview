// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPoisonForTestHasNoProductionCaller keeps DefinitionsStore.PoisonForTest
// test-only (#1345 Q1-F1). It is exported so internal/api's handler tests
// can make a definitions save fail, which means the compiler would let
// production code call it too -- and a server that did would fail every
// later definition save. So scan every non-test Go file in the module: the
// only one allowed to name it is definitions_store.go, which defines it.
// Same shape as internal/auth's TestSetHashParamsForTestHasNoProductionCaller.
func TestPoisonForTestHasNoProductionCaller(t *testing.T) {
	root := filepath.Join("..", "..")
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("module root not found at %s: %v", root, err)
	}
	definition := filepath.Join("internal", "engine", "definitions_store.go")
	sawDefinition := false
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !strings.Contains(string(src), "PoisonForTest") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if rel == definition {
			sawDefinition = true
			return nil
		}
		t.Errorf("%s names PoisonForTest: it is for tests only and would make every later definition save fail", rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sawDefinition {
		t.Fatalf("did not find PoisonForTest in %s -- the scan is not looking where it thinks it is", definition)
	}
}
