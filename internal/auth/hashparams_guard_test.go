// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSetHashParamsForTestHasNoProductionCaller keeps SetHashParamsForTest
// test-only (#1392). It is exported so internal/api's TestMain can cheapen
// Argon2id for its own test binary, which means the compiler would let
// production code call it too -- and a server that did would hash every
// password at the 8 MiB test cost without anything else noticing. So scan
// every non-test Go file in the module: the only one allowed to name it is
// password.go, which defines it.
func TestSetHashParamsForTestHasNoProductionCaller(t *testing.T) {
	root := filepath.Join("..", "..")
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("module root not found at %s: %v", root, err)
	}
	definition := filepath.Join("internal", "auth", "password.go")
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
		if !strings.Contains(string(src), "SetHashParamsForTest") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if rel == definition {
			sawDefinition = true
			return nil
		}
		t.Errorf("%s names SetHashParamsForTest: it is for test binaries only and would weaken every password hash", rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sawDefinition {
		t.Fatalf("did not find SetHashParamsForTest in %s -- the scan is not looking where it thinks it is", definition)
	}
}
