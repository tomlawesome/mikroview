// SPDX-License-Identifier: AGPL-3.0-only

package retention

import (
	"bytes"
	"reflect"
	"testing"
)

// tornHeader calls FrameFormat.TornHeader by reflection so this package's
// tests still build before the method exists and fail on behaviour.
func tornHeader(t *testing.T, ff FrameFormat, b []byte) bool {
	t.Helper()
	m := reflect.ValueOf(ff).MethodByName("TornHeader")
	if !m.IsValid() {
		t.Fatal("FrameFormat has no TornHeader method")
	}
	return m.Call([]reflect.Value{reflect.ValueOf(b)})[0].Bool()
}

// O1-R1: pins which short files count as a header torn by a crash.
func TestTornHeader(t *testing.T) {
	ff := dayFormat("2026-09-03")
	full := append([]byte(ff.Magic+string([]byte{ff.Version})), bytes.Repeat([]byte{0xAB}, saltBytes)...)
	cases := []struct {
		name string
		b    []byte
		want bool
	}{
		{"empty is not torn, it is absent", nil, false},
		{"all zeros, header length", make([]byte, ff.headerBytes()), true},
		{"all zeros, short", make([]byte, 11), true},
		{"magic, version, then zeros", append([]byte(ff.Magic+string([]byte{ff.Version})), make([]byte, saltBytes)...), true},
		{"prefix of magic", []byte(ff.Magic[:3]), true},
		{"a genuine header with a salt", full, false},
		{"one zero too many", make([]byte, ff.headerBytes()+1), false},
		{"plaintext JSON", []byte(`{"a":1}`), false},
		{"foreign bytes of header length", []byte("not a frame log at all"), false},
	}
	for _, c := range cases {
		if got := tornHeader(t, ff, c.b); got != c.want {
			t.Errorf("%s: TornHeader = %v, want %v", c.name, got, c.want)
		}
	}
}
