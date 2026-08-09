// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"fmt"
	"strconv"
	"strings"
)

// ByteSize is a size in bytes, configured with a human suffix (e.g.
// "120MiB", "256MB") rather than a raw integer count -- see #244.
// store.maxMemory is the only field that uses it: a memory budget is
// what an operator actually has to reason about (it is what they set on
// a container), unlike an event count, whose meaning depends entirely on
// a per-deployment traffic rate that varies by four orders of magnitude.
type ByteSize int64

// byteSizeUnits accepts both decimal (SI, powers of 1000) and binary
// (IEC, powers of 1024) suffixes, and treats them as genuinely different
// values rather than interchangeable shorthand: 1MB is 1,000,000 bytes,
// 1MiB is 1,048,576 bytes, a ~5% difference that widens to ~7% at
// GB/GiB. Silently picking one for "MB" would silently under- or
// over-provision by that much.
var byteSizeUnits = map[string]int64{
	"B":   1,
	"KB":  1_000,
	"MB":  1_000_000,
	"GB":  1_000_000_000,
	"KIB": 1024,
	"MIB": 1024 * 1024,
	"GIB": 1024 * 1024 * 1024,
}

// ParseByteSize parses a human size string. A bare number with no suffix
// is bytes, for programmatic callers (env vars, tests) that would rather
// pass an exact figure than round-trip through a unit.
func ParseByteSize(s string) (ByteSize, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("byte size is empty")
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return ByteSize(n), nil
	}

	i := 0
	for i < len(s) && (s[i] == '-' || s[i] == '+' || s[i] == '.' || (s[i] >= '0' && s[i] <= '9')) {
		i++
	}
	if i == 0 {
		return 0, fmt.Errorf("%q does not start with a number", s)
	}
	numPart, unitPart := s[:i], strings.ToUpper(strings.TrimSpace(s[i:]))

	mult, ok := byteSizeUnits[unitPart]
	if !ok {
		return 0, fmt.Errorf("%q is not a recognised unit -- use B, KB, MB, GB, KiB, MiB or GiB", s[i:])
	}
	f, err := strconv.ParseFloat(numPart, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a number", numPart)
	}
	return ByteSize(f * float64(mult)), nil
}

// String renders the largest binary unit that keeps one digit before the
// decimal point -- "120.0MiB" rather than "125829120B" -- since this is
// what appears in warnings and startup logs, both read by a human.
func (b ByteSize) String() string {
	switch {
	case b >= 1<<30 || b <= -(1<<30):
		return fmt.Sprintf("%.1fGiB", float64(b)/(1<<30))
	case b >= 1<<20 || b <= -(1<<20):
		return fmt.Sprintf("%.1fMiB", float64(b)/(1<<20))
	case b >= 1<<10 || b <= -(1<<10):
		return fmt.Sprintf("%.1fKiB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%dB", int64(b))
	}
}

// UnmarshalText makes ByteSize a YAML scalar via yaml.v3's
// encoding.TextUnmarshaler fallback (the same mechanism time.Duration
// uses internally, just not built in for arbitrary types) -- so
// store.maxMemory reads directly out of the config file with no
// intermediate string field.
func (b *ByteSize) UnmarshalText(text []byte) error {
	v, err := ParseByteSize(string(text))
	if err != nil {
		return err
	}
	*b = v
	return nil
}

// Set implements flag.Value, so -max-memory accepts the same "120MiB"
// syntax as the YAML field and the environment variable rather than a
// third, bytes-only format.
func (b *ByteSize) Set(s string) error { return b.UnmarshalText([]byte(s)) }
