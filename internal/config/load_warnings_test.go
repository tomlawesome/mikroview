// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadWithProblemsSurfacesWarnings guards a bug that shipped for
// about ten minutes: LoadWithProblems originally called Load and then
// re-ran Validate, but the first pass had already clamped the bad value,
// so the second found nothing and returned zero warnings.
//
// The operator would have got a silently substituted default with no
// notification -- which is exactly the failure this whole feature exists
// to prevent, reintroduced by the feature itself. The result must come
// from the same Validate call that did the clamping.
func TestLoadWithProblemsSurfacesWarnings(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.yaml")
	os.WriteFile(p, []byte("store:\n  retention: -5m\n"), 0o600)

	cfg, res, err := LoadWithProblems(p, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(res.Warnings) == 0 {
		t.Fatal("a negative retention produced no warning -- the clamp happened but the operator is never told")
	}
	if res.Warnings[0].Applied == "" {
		t.Error("warning carries no Applied value, so the operator can't see what was substituted")
	}
	if cfg.Store.Retention <= 0 {
		t.Error("the safe default was reported but not actually applied")
	}
}

// TestUpgradeWithNoHistoryBlockAndNoKeyComesUpMemoryOnly pins #1357's
// core promise for an existing deployment: history.enabled now defaults
// to true, so a config.yaml written before this change -- no history:
// block at all -- decodes with the field untouched, at the new default.
// With no key mounted that must still come up rather than refuse to
// start, land on Enabled=false (there is nothing else it could honestly
// do with no key), and say so once as a warning -- never as a fatal
// problem, and never by deleting anything (that part is #1353/#1354's,
// unchanged here).
func TestUpgradeWithNoHistoryBlockAndNoKeyComesUpMemoryOnly(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.yaml")
	// A config file that predates history: entirely -- the shape an
	// upgrader's actual config.yaml is in.
	os.WriteFile(p, []byte("listen:\n  http: \":8080\"\n"), 0o600)

	cfg, res, err := LoadWithProblems(p, nil)
	if err != nil {
		t.Fatalf("an upgrade with no history: block and no key must come up, not refuse to start: %v", err)
	}
	if len(res.Fatal) != 0 {
		t.Errorf("fatal problems on a config that predates history entirely: %v", codes(res.Fatal))
	}
	if cfg.History.Enabled {
		t.Error("Enabled ended up true with no key mounted -- there is no unencrypted mode")
	}
	if p := has(res.Warnings, "CFG-0080"); p == nil {
		t.Errorf("no CFG-0080 warning -- the admin is never told history came up off, got %v", codes(res.Warnings))
	}
}
