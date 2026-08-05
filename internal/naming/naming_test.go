package naming

import "testing"

func TestResolverReturnsConfiguredNames(t *testing.T) {
	r := Resolver{
		Rules: map[string]string{"r13": "Block known scanners"},
		Hosts: map[string]string{"192.168.1.50": "Living room NAS"},
	}

	if got := r.Rule("r13"); got != "Block known scanners" {
		t.Errorf("Rule(\"r13\") = %q, want \"Block known scanners\"", got)
	}
	if got := r.Host("192.168.1.50"); got != "Living room NAS" {
		t.Errorf("Host(\"192.168.1.50\") = %q, want \"Living room NAS\"", got)
	}
}

func TestResolverMissReturnsEmptyString(t *testing.T) {
	r := Resolver{
		Rules: map[string]string{"r13": "Block known scanners"},
		Hosts: map[string]string{"192.168.1.50": "Living room NAS"},
	}

	if got := r.Rule("r99"); got != "" {
		t.Errorf("Rule(\"r99\") = %q, want empty", got)
	}
	if got := r.Host("10.0.0.1"); got != "" {
		t.Errorf("Host(\"10.0.0.1\") = %q, want empty", got)
	}
}

func TestZeroValueResolverIsUsable(t *testing.T) {
	var r Resolver
	if got := r.Rule("r13"); got != "" {
		t.Errorf("Rule() on zero value = %q, want empty", got)
	}
	if got := r.Host("192.168.1.50"); got != "" {
		t.Errorf("Host() on zero value = %q, want empty", got)
	}
}
