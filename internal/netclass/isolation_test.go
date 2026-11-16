// SPDX-License-Identifier: AGPL-3.0-only

package netclass_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNetClassDoesNotImportSuspicionMachinery is the structural guard the
// #114 findings asked for, in the shape of authzMatrix: the build fails
// if this package ever imports internal/flags or internal/engine.
//
// The whole design of #114 is that a network-class match is display
// context, not a suspicion signal -- a datacenter label alone covers
// >10% of routable IPv4. Any path from classification to a confidence
// score has to go through a caller that decides, deliberately and
// direction-aware, to allow it. Wiring flags or the evaluation engine
// straight into this package would erase that boundary quietly, so the
// boundary is a compiled-in test rather than a comment someone can drift
// past. (internal/detect was the engine when this was written; issue
// #405 deleted it, and this guards what replaced it.)
func TestNetClassDoesNotImportSuspicionMachinery(t *testing.T) {
	forbidden := []string{
		"github.com/tomlawesome/mikroview/internal/flags",
		"github.com/tomlawesome/mikroview/internal/engine",
	}

	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading package directory: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		path := entry.Name()
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		if strings.HasSuffix(file.Name.Name, "_test") {
			continue // the test package itself may import anything
		}
		for _, imp := range file.Imports {
			got := strings.Trim(imp.Path.Value, `"`)
			for _, bad := range forbidden {
				if got == bad {
					t.Errorf("%s imports %s -- netclass must not reach the suspicion machinery; a match is display context, not a score (#114)",
						filepath.Base(path), bad)
				}
			}
		}
	}
}
