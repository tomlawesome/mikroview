// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestParseByteSize(t *testing.T) {
	tests := []struct {
		in   string
		want ByteSize
	}{
		{"0", 0},
		{"120", 120},
		{"1B", 1},
		{"1KB", 1_000},
		{"1MB", 1_000_000},
		{"1GB", 1_000_000_000},
		{"1KiB", 1024},
		{"1MiB", 1024 * 1024},
		{"1GiB", 1024 * 1024 * 1024},
		{"120MiB", 120 * 1024 * 1024},
		{"1.5GiB", ByteSize(1.5 * 1024 * 1024 * 1024)},
		{"  256MB  ", 256_000_000},
		{"256mb", 256_000_000}, // case-insensitive
		{"256 MB", 256_000_000},
		{"-5MB", -5_000_000},
	}
	for _, tt := range tests {
		got, err := ParseByteSize(tt.in)
		if err != nil {
			t.Errorf("ParseByteSize(%q) returned error: %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseByteSize(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestParseByteSizeRejectsGarbage(t *testing.T) {
	for _, in := range []string{"", "   ", "banana", "MB", "10XB", "GiB120"} {
		if _, err := ParseByteSize(in); err == nil {
			t.Errorf("ParseByteSize(%q) should have failed, did not", in)
		}
	}
}

// 1MB (decimal) and 1MiB (binary) differ by about 5% -- exactly the
// mixup ParseByteSize exists to prevent, so it is worth a test that
// nails both down rather than trusting the table above alone.
func TestParseByteSizeDecimalVsBinaryAreDistinct(t *testing.T) {
	mb, err := ParseByteSize("1MB")
	if err != nil {
		t.Fatal(err)
	}
	mib, err := ParseByteSize("1MiB")
	if err != nil {
		t.Fatal(err)
	}
	if mb == mib {
		t.Fatalf("1MB and 1MiB parsed to the same value (%d) -- they differ by 48576 bytes", mb)
	}
	if mb != 1_000_000 || mib != 1_048_576 {
		t.Fatalf("1MB = %d, 1MiB = %d -- want 1000000 and 1048576", mb, mib)
	}
}

func TestByteSizeString(t *testing.T) {
	tests := []struct {
		in   ByteSize
		want string
	}{
		{0, "0B"},
		{512, "512B"},
		{1024, "1.0KiB"},
		{120 * 1024 * 1024, "120.0MiB"},
		{2 * 1024 * 1024 * 1024, "2.0GiB"},
		{-5 * 1024 * 1024, "-5.0MiB"},
	}
	for _, tt := range tests {
		if got := tt.in.String(); got != tt.want {
			t.Errorf("ByteSize(%d).String() = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// The real integration this type exists for: store.maxMemory has to come
// straight out of a YAML scalar the same way store.retention does.
func TestByteSizeUnmarshalsFromYAML(t *testing.T) {
	var target struct {
		MaxMemory ByteSize `yaml:"maxMemory"`
	}
	if err := yaml.Unmarshal([]byte("maxMemory: 120MiB\n"), &target); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	if want := ByteSize(120 * 1024 * 1024); target.MaxMemory != want {
		t.Errorf("MaxMemory = %d, want %d", target.MaxMemory, want)
	}
}

func TestByteSizeUnmarshalFromYAMLRejectsGarbage(t *testing.T) {
	var target struct {
		MaxMemory ByteSize `yaml:"maxMemory"`
	}
	if err := yaml.Unmarshal([]byte("maxMemory: banana\n"), &target); err == nil {
		t.Fatal("expected an error unmarshaling an unparseable byte size, got nil")
	}
}
