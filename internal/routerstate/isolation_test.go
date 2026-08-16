// SPDX-License-Identifier: AGPL-3.0-only

package routerstate_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestPushedDataCannotTouchSuspicionMachinery is #186 step 4d's
// structural test, in the shape of authzMatrix and netclass's own
// isolation test: the build fails if a code path ever lets pushed
// router data reach a suspicion signal.
//
// Both directions are checked, because the invariant needs both. This
// package importing flags/detect would let pushed data *write* a signal
// (raise, lower, clear, exclude -- any of them); flags/detect importing
// this package would let a detector *read* pushed data into a scoring
// decision. #186 step 4a's rule is that router data can only ever make
// something look more suspicious via an explicit, narrowly-argued
// caller (the shape internal/detect/netclass.go established for #114)
// -- and no such caller exists yet, so today the correct number of
// edges between these packages is zero, enforced here rather than
// remembered.
func TestPushedDataCannotTouchSuspicionMachinery(t *testing.T) {
	const (
		flagsPkg       = "github.com/tomlawesome/mikroview/internal/flags"
		enginePkg      = "github.com/tomlawesome/mikroview/internal/engine"
		routerstatePkg = "github.com/tomlawesome/mikroview/internal/routerstate"
	)

	assertNoImport := func(dir string, forbidden ...string) {
		t.Helper()
		fset := token.NewFileSet()
		pkgs, err := parser.ParseDir(fset, dir, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parsing %s: %v", dir, err)
		}
		for name, pkg := range pkgs {
			if strings.HasSuffix(name, "_test") {
				continue // test packages may import anything
			}
			for path, file := range pkg.Files {
				for _, imp := range file.Imports {
					got := strings.Trim(imp.Path.Value, `"`)
					for _, bad := range forbidden {
						if got == bad {
							t.Errorf("%s imports %s -- pushed router data must not reach the suspicion machinery in either direction (#186 step 4d)",
								filepath.Join(filepath.Base(dir), filepath.Base(path)), bad)
						}
					}
				}
			}
		}
	}

	// internal/detect was the suspicion machinery when #186 wrote this;
	// internal/engine is (issue #405 deleted that package and this test
	// follows the thing it was guarding, not the name it had).
	assertNoImport(".", flagsPkg, enginePkg)
	assertNoImport("../flags", routerstatePkg)
	assertNoImport("../engine", routerstatePkg)
}
