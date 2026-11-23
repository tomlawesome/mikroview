// SPDX-License-Identifier: AGPL-3.0-only

package hosts

import (
	"strings"
	"testing"
)

// #1451: a key taken from a URL path is checked before it reaches the audit trail.
func TestValidateKey(t *testing.T) {
	for _, ok := range []string{"ether1|192.0.2.1", "a", strings.Repeat("k", maxKeyLength)} {
		if err := ValidateKey(ok); err != nil {
			t.Errorf("ValidateKey(%q) = %v, want nil", ok, err)
		}
	}
	for name, bad := range map[string]string{
		"empty":       "",
		"too long":    strings.Repeat("k", maxKeyLength+1),
		"control":     "a\x00b",
		"newline":     "a\nb",
		"bidi":        "a‮b",
		"invalid utf": "a\xffb",
	} {
		if err := ValidateKey(bad); err == nil {
			t.Errorf("%s: ValidateKey accepted %q", name, bad)
		}
	}
}
