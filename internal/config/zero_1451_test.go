// SPDX-License-Identifier: AGPL-3.0-only

package config

import "testing"

// #1451: ByteSize.Set is what the -max-memory flag calls.
func TestByteSizeSetParsesTheSameSyntaxAsTheConfigFile(t *testing.T) {
	var b ByteSize
	if err := b.Set("120MiB"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if want := ByteSize(120 << 20); b != want {
		t.Errorf("Set(120MiB) = %d, want %d", b, want)
	}
	prev := b
	if err := b.Set("lots"); err == nil {
		t.Error("Set accepted a non-size")
	}
	if b != prev {
		t.Errorf("a failed Set changed the value to %d", b)
	}
}
