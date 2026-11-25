// SPDX-License-Identifier: AGPL-3.0-only

package export

import (
	"strings"
	"testing"
)

// #1451: the messages an admin reads when an uploaded /export is refused.
func TestParseErrorsNameTheLineAndKey(t *testing.T) {
	_, err := Parse("/interface wireless security-profiles\nadd password=\"hunter2\"\n")
	if err == nil {
		t.Fatal("secret not refused")
	}
	for _, want := range []string{"line 2", `"password"`, "hide-sensitive"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("secret error %q lacks %q", err, want)
		}
	}
	_, err = Parse("/ip firewall filter\nadd action=accept chain=forward comment=\"foo\rbar\"\n")
	if err == nil {
		t.Fatal("control character not refused")
	}
	for _, want := range []string{"line 2", `"comment"`, "control character"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("control-char error %q lacks %q", err, want)
		}
	}
}
