// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadCgroupLimit(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}

	cases := []struct {
		name string
		body string
		want ByteSize
	}{
		{"a real v2 limit", "536870912\n", 512 << 20},
		{"v2's unlimited word", "max\n", 0},
		{"v1's unlimited sentinel", "9223372036854771712\n", 0},
		{"nonsense", "not a number\n", 0},
		{"zero", "0\n", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := readCgroupLimit(write(tc.name, tc.body)); got != tc.want {
				t.Errorf("readCgroupLimit(%q) = %d, want %d", tc.body, got, tc.want)
			}
		})
	}

	if got := readCgroupLimit(filepath.Join(dir, "does-not-exist")); got != 0 {
		t.Errorf("a missing file gave %d, want 0 -- an unreadable limit is not a limit of zero", got)
	}
}

// The fixture is the real /proc/meminfo shape, whitespace and all --
// MemTotal is in KiB with a trailing unit, and it is not the first line.
func TestReadMemTotal(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "meminfo")
	body := "MemFree:         1234567 kB\nMemTotal:       16316492 kB\nMemAvailable:    9876543 kB\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, want := readMemTotal(p), ByteSize(16316492)*1024; got != want {
		t.Errorf("readMemTotal = %d, want %d", got, want)
	}
	if got := readMemTotal(filepath.Join(dir, "nope")); got != 0 {
		t.Errorf("a missing /proc/meminfo gave %d, want 0", got)
	}
}

// The headroom rule itself, at both ends of the host-size range: the
// flat 256 MiB reserve binds on a small host and the quarter binds on a
// large one, and the result is always expressed in ring bytes (i.e.
// already divided by the resident overhead).
func TestMaxMemoryCeilingHeadroomRule(t *testing.T) {
	cases := []struct {
		name  string
		basis ByteSize
		want  ByteSize
	}{
		{
			// 512 MiB: a quarter is 128 MiB, so the flat 256 MiB reserve
			// binds. 256 MiB left / 1.47 = 174.1 MiB -> 174 MiB.
			name:  "small host, the flat reserve binds",
			basis: 512 << 20,
			want:  174 << 20,
		},
		{
			// 16 GiB: a quarter is 4 GiB, well above the flat reserve.
			// 12 GiB / 1.47 = 8.16 GiB -> 8359 MiB.
			name:  "large host, the quarter binds",
			basis: 16 << 30,
			want:  8359 << 20,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ceilingFor(tc.basis); got != tc.want {
				t.Errorf("ceiling for a %s host = %s, want %s", tc.basis, got, tc.want)
			}
		})
	}
}

// A host with nothing to spare still offers a range rather than an
// inverted one.
func TestMaxMemoryCeilingNeverFallsBelowTheFloor(t *testing.T) {
	if got := ceilingFor(64 << 20); got != MinMaxMemory {
		t.Errorf("ceiling on a 64 MiB host = %s, want the %s floor", got, MinMaxMemory)
	}
}

// An instance already running on a deliberately large budget (#244's
// allowed case) must not be told its own current figure is out of range.
func TestMaxMemoryCeilingAdmitsTheRunningFigure(t *testing.T) {
	running := ByteSize(6) << 30
	b := MaxMemoryCeiling(running)
	if b.Max < running {
		t.Fatalf("ceiling %s is below the running figure %s", b.Max, running)
	}
	if err := b.ValidateMaxMemory(running); err != nil {
		t.Errorf("the figure this instance is already running on was refused: %v", err)
	}
}

func TestValidateMaxMemory(t *testing.T) {
	b := MemoryBounds{Min: MinMaxMemory, Max: 512 << 20}

	if err := b.ValidateMaxMemory(MinMaxMemory - 1); err == nil {
		t.Error("a figure below the floor was accepted")
	}
	if err := b.ValidateMaxMemory(b.Max + 1); err == nil {
		t.Error("a figure above the ceiling was accepted")
	}
	for _, ok := range []ByteSize{MinMaxMemory, 120 << 20, 512 << 20} {
		if err := b.ValidateMaxMemory(ok); err != nil {
			t.Errorf("ValidateMaxMemory(%s) = %v, want nil", ok, err)
		}
	}
}

// MaxMemoryCeiling reads this machine's own /proc and /sys, so it cannot
// be pinned to a fixture -- but it can be held to the invariants that
// must hold on any host, which is what a caller depends on.
func TestMaxMemoryCeilingOnThisHost(t *testing.T) {
	b := MaxMemoryCeiling(defaults().Store.MaxMemory)
	if b.Min != MinMaxMemory {
		t.Errorf("Min = %s, want %s", b.Min, MinMaxMemory)
	}
	if b.Max < b.Min {
		t.Errorf("Max %s is below Min %s -- an inverted range", b.Max, b.Min)
	}
	if err := b.ValidateMaxMemory(defaults().Store.MaxMemory); err != nil {
		t.Errorf("the shipped default was refused on this host: %v", err)
	}
	if b.HostTotal > 0 && b.Max >= b.HostTotal {
		t.Errorf("Max %s is not below the host's own %s -- no headroom was reserved", b.Max, b.HostTotal)
	}
}
