// SPDX-License-Identifier: AGPL-3.0-only

package detect

import "testing"

func TestOvershootConfidence(t *testing.T) {
	cases := []struct {
		name      string
		count     int
		threshold int
		want      int
	}{
		{"exactly at threshold", 5, 5, 0},
		{"at the ceiling (3x threshold)", 15, 5, 100},
		{"beyond the ceiling clamps to 100", 100, 5, 100},
		{"halfway to the ceiling", 10, 5, 50},
		{"threshold of zero treated as maximally confident", 3, 0, 100},
		{"negative threshold treated as maximally confident", 3, -1, 100},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := overshootConfidence(tc.count, tc.threshold); got != tc.want {
				t.Errorf("overshootConfidence(%d, %d) = %d, want %d", tc.count, tc.threshold, got, tc.want)
			}
		})
	}
}
