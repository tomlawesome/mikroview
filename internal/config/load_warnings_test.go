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
