// SPDX-License-Identifier: AGPL-3.0-only

package ingest

import (
	"encoding/json"
	"testing"
)

// #1451: paths no other test reached.

// A rule with no dst-port covers every port but names none; NamesPorts is
// the one that tells a scoped rule from an unscoped one (#1018).
func TestNamesPortsDistinguishesScopedFromUnscopedRules(t *testing.T) {
	cases := []struct {
		spec  string
		ports []int
		want  bool
	}{
		{"", []int{22}, false},
		{"   ", []int{22}, false},
		{"22", []int{22}, true},
		{"22", []int{23}, false},
		{"22,80", []int{443, 80}, true},
		{"1000-2000", []int{1500}, true},
		{"1000-2000", []int{999, 2001}, false},
		{"22", nil, false},
		{"ssh", []int{22}, false}, // a name RouterOS resolves is not a claim we can make
	}
	for _, c := range cases {
		if got := NamesPorts(c.spec, c.ports); got != c.want {
			t.Errorf("NamesPorts(%q, %v) = %v, want %v", c.spec, c.ports, got, c.want)
		}
	}
}

func TestRouterOSListMarshalsAsAnArrayEvenWhenNil(t *testing.T) {
	var nilList RouterOSList
	b, err := json.Marshal(nilList)
	if err != nil || string(b) != "[]" {
		t.Errorf("nil list marshals as %q, %v; want []", b, err)
	}
	b, err = json.Marshal(RouterOSList{"a", "b"})
	if err != nil || string(b) != `["a","b"]` {
		t.Errorf("list marshals as %q, %v", b, err)
	}
	type wrap struct {
		L RouterOSList `json:"l"`
	}
	b, _ = json.Marshal(wrap{})
	if string(b) != `{"l":[]}` {
		t.Errorf("a struct holding a nil list marshals as %s, want an empty array", b)
	}
}
