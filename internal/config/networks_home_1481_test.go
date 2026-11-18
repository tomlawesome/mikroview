// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestNetworksHomeLoadsFromYAML(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte("networks:\n  home:\n    - 198.51.100.0/24\n    - 192.0.2.32/27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"198.51.100.0/24", "192.0.2.32/27"}
	if !reflect.DeepEqual(cfg.Networks.Home, want) {
		t.Errorf("Networks.Home = %v, want %v", cfg.Networks.Home, want)
	}
}

func TestNetworksHomeDefaultsToEmpty(t *testing.T) {
	cfg, err := Load("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Networks.Home) != 0 {
		t.Errorf("default Networks.Home = %v, want empty", cfg.Networks.Home)
	}
}

func TestNetworksHomeRoundTripsThroughYAMLAsHome(t *testing.T) {
	in := Networks{Home: []string{"198.51.100.0/24", "192.0.2.248/29"}}
	b, err := yaml.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "home:") {
		t.Errorf("marshalled %q lacks the yaml key \"home\"", b)
	}
	var out Networks
	if err := yaml.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Errorf("round trip = %+v, want %+v", out, in)
	}
}

func TestValidateAcceptsAValidNetworksHome(t *testing.T) {
	c := validCfg()
	c.Networks.Home = []string{"198.51.100.0/24", "192.0.2.32/27", "2001:db8:1::/48"}
	r := c.Validate()
	if p := firstOnKey(r, "networks.home"); p != nil {
		t.Errorf("valid networks.home produced a problem: %+v", *p)
	}
}

func TestValidateRejectsAZeroLengthNetworksHome(t *testing.T) {
	for _, bad := range []string{"0.0.0.0/0", "::/0"} {
		c := validCfg()
		c.Networks.Home = []string{"198.51.100.0/24", bad}
		r := c.Validate()
		if firstCode(r.Fatal, "networks.home") == "" {
			t.Errorf("networks.home %q: want a fatal problem on key networks.home, got fatal=%v warnings=%v", bad, codes(r.Fatal), codes(r.Warnings))
		}
	}
}

func TestValidateRejectsAnUnparsableNetworksHomeEntry(t *testing.T) {
	for _, bad := range []string{"not-a-cidr", "198.51.100.0/33", ""} {
		c := validCfg()
		c.Networks.Home = []string{bad}
		r := c.Validate()
		if firstCode(r.Fatal, "networks.home") == "" {
			t.Errorf("networks.home %q: want a fatal problem on key networks.home, got fatal=%v warnings=%v", bad, codes(r.Fatal), codes(r.Warnings))
		}
	}
}

func firstCode(ps []Problem, keyPrefix string) string {
	for _, p := range ps {
		if strings.HasPrefix(p.Key, keyPrefix) {
			return p.Code
		}
	}
	return ""
}

func firstOnKey(r Result, keyPrefix string) *Problem {
	for _, ps := range [][]Problem{r.Fatal, r.Warnings} {
		for i := range ps {
			if strings.HasPrefix(ps[i].Key, keyPrefix) {
				return &ps[i]
			}
		}
	}
	return nil
}
