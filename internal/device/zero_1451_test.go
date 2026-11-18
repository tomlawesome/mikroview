// SPDX-License-Identifier: AGPL-3.0-only

package device

import "testing"

// #1451: EnrolLine is the syslog gate's cheap test for "give this line to TryEnrol".
func TestEnrolLineRecognisesTheMarkerWhateverTheTokenState(t *testing.T) {
	r := NewRegistry(nil)
	tok := "abcdefghij0123456789"
	for _, line := range []string{
		"mikroview-enrol " + tok,
		"<134>Nov 16 12:00:00 rb script,info mikroview-enrol " + tok + " extra",
	} {
		if !r.EnrolLine([]byte(line)) {
			t.Errorf("EnrolLine(%q) = false", line)
		}
	}
	for _, line := range []string{
		"", "firewall,info forward: in:ether1", "mikroview-enrol short", "mikroview-enrol ABCDEFGHIJ0123456789", "mikroview-enrol",
	} {
		if r.EnrolLine([]byte(line)) {
			t.Errorf("EnrolLine(%q) = true", line)
		}
	}
}
