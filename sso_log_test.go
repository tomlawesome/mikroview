// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"errors"
	"os"
	"strings"
	"testing"

	goidc "github.com/tomlawesome/gauntlet/oidc"
)

// #1480: the start-up line an operator reads when SSO comes up must not
// say strangers are kept out unless gauntlet's own rule,
// AllowIssuerWithPolicy, says the issuer and policy together keep them
// out. Restricted() ("the policy narrows anything") is not that test: a
// group allowlist on a shared issuer narrows something and still admits
// any account at that provider that can join or claim the group.
//
// ssoActiveLogLine(issuer, policy) is the helper startOIDC logs.
func TestSSOActiveLogLine(t *testing.T) {
	const (
		selfHosted = "https://auth.example.com/application/o/mikroview/"
		shared     = "https://accounts.google.com"
	)
	cases := []struct {
		name   string
		issuer string
		policy goidc.Policy
		// keepsOut is gauntlet's verdict, checked below so a change in
		// gauntlet's rule shows up here rather than in a wrong log line.
		keepsOut     bool
		wantContains []string
		wantAbsent   []string
	}{
		{
			name:         "no restriction",
			issuer:       selfHosted,
			policy:       goidc.Policy{},
			keepsOut:     true, // the self-hosted issuer is the access control
			wantContains: []string{selfHosted, "for any account that issuer vouches for"},
			wantAbsent:   []string{"restricted"},
		},
		{
			name:     "group allowlist only, shared issuer",
			issuer:   shared,
			policy:   goidc.Policy{AllowedGroups: []string{"mikroview"}},
			keepsOut: false,
			// Exact wording is the builder's; it must not claim the door is shut.
			wantContains: []string{shared},
			wantAbsent:   []string{"restricted", "permitted accounts"},
		},
		{
			name:         "pinned to one tenant",
			issuer:       shared,
			policy:       goidc.Policy{RequiredClaims: map[string][]string{"hd": {"example.com"}}},
			keepsOut:     true,
			wantContains: []string{shared, "restricted"},
			wantAbsent:   []string{"for any account that issuer vouches for"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := goidc.AllowIssuerWithPolicy(tc.issuer, tc.policy)
			if got := err == nil; got != tc.keepsOut {
				t.Fatalf("gauntlet's AllowIssuerWithPolicy(%q) = %v; this case assumes keepsOut=%v", tc.issuer, err, tc.keepsOut)
			}
			if err != nil && !errors.Is(err, goidc.ErrMultiTenantIssuer) {
				t.Fatalf("AllowIssuerWithPolicy: unexpected error %v", err)
			}
			line := ssoActiveLogLine(tc.issuer, tc.policy)
			for _, s := range tc.wantContains {
				if !strings.Contains(line, s) {
					t.Errorf("log line %q does not contain %q", line, s)
				}
			}
			for _, s := range tc.wantAbsent {
				if strings.Contains(strings.ToLower(line), strings.ToLower(s)) {
					t.Errorf("log line %q contains %q, which this policy does not earn", line, s)
				}
			}
		})
	}
}

// The start-up path logs through the helper, not the deprecated
// Restricted() (staticcheck SA1019, #1462).
func TestStartOIDCDoesNotUseRestricted(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(src), ".Restricted()") {
		t.Error("main.go still calls the deprecated oidc.Policy.Restricted()")
	}
	if !strings.Contains(string(src), "ssoActiveLogLine(") {
		t.Error("main.go does not log the SSO start-up line through ssoActiveLogLine")
	}
}
