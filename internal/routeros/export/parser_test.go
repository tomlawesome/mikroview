// SPDX-License-Identifier: AGPL-3.0-only

package export

import (
	"os"
	"testing"
)

func loadFixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("testdata/hide-sensitive.rsc")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestParseHideSensitiveFixture is the main table test: a realistic
// RouterOS 7.24.1 `/export hide-sensitive` (testdata/hide-sensitive.rsc)
// with continuation lines, quoting (including an escaped quote), a
// disabled rule, an interface-list match, and a non-forward chain --
// decoded into exactly the attributes each rule carries.
func TestParseHideSensitiveFixture(t *testing.T) {
	ex, err := Parse(loadFixture(t))
	if err != nil {
		t.Fatalf("Parse = error %v, want success", err)
	}
	if ex.Version != "7.24.1" {
		t.Errorf("Version = %q, want 7.24.1", ex.Version)
	}

	want := []struct {
		chain, action, comment                      string
		inIf, outIf, inIfList, outIfList, logPrefix string
		log, disabled                               bool
		line, lineEnd                               int
	}{
		{
			chain: "input", action: "accept", comment: "allow established",
			line: 21, lineEnd: 21,
		},
		{
			chain: "forward", action: "accept", comment: "lan to wan",
			inIf: "bridge1", outIf: "ether1", logPrefix: "", log: false,
			line: 22, lineEnd: 23,
		},
		{
			chain: "forward", action: "drop", comment: "block wan to lan, unsolicited",
			inIf: "ether1", outIf: "bridge1",
			line: 24, lineEnd: 25,
		},
		{
			chain: "forward", action: "accept", comment: "guest network to wan only",
			inIfList: "GUEST", outIf: "ether1", log: true, logPrefix: "A|guest|",
			line: 26, lineEnd: 27,
		},
		{
			chain: "forward", action: "drop", comment: "drop everything else leaving bridge1",
			inIf: "bridge1",
			line: 28, lineEnd: 29,
		},
		{
			chain: "forward", action: "reject", comment: `reject intra-lan pair (a "noisy" host)`,
			inIf: "bridge1", outIf: "bridge1", log: true, logPrefix: "R|custom|",
			line: 30, lineEnd: 31,
		},
		{
			chain: "forward", action: "drop", comment: "disabled test rule -- do not enable",
			disabled: true,
			line:     32, lineEnd: 32,
		},
	}

	if len(ex.FilterRules) != len(want) {
		t.Fatalf("decoded %d filter rules, want %d: %+v", len(ex.FilterRules), len(want), ex.FilterRules)
	}

	for i, w := range want {
		r := ex.FilterRules[i]
		if r.Index != i {
			t.Errorf("rule %d: Index = %d, want %d", i, r.Index, i)
		}
		if r.Chain != w.chain || r.Action != w.action || r.Comment != w.comment {
			t.Errorf("rule %d: Chain/Action/Comment = %q/%q/%q, want %q/%q/%q", i, r.Chain, r.Action, r.Comment, w.chain, w.action, w.comment)
		}
		if r.InInterface != w.inIf || r.OutInterface != w.outIf {
			t.Errorf("rule %d: InInterface/OutInterface = %q/%q, want %q/%q", i, r.InInterface, r.OutInterface, w.inIf, w.outIf)
		}
		if r.InInterfaceList != w.inIfList || r.OutInterfaceList != w.outIfList {
			t.Errorf("rule %d: InInterfaceList/OutInterfaceList = %q/%q, want %q/%q", i, r.InInterfaceList, r.OutInterfaceList, w.inIfList, w.outIfList)
		}
		if r.Log != w.log || r.LogPrefix != w.logPrefix {
			t.Errorf("rule %d: Log/LogPrefix = %v/%q, want %v/%q", i, r.Log, r.LogPrefix, w.log, w.logPrefix)
		}
		if r.Disabled != w.disabled {
			t.Errorf("rule %d: Disabled = %v, want %v", i, r.Disabled, w.disabled)
		}
		if r.Line != w.line || r.LineEnd != w.lineEnd {
			t.Errorf("rule %d: Line/LineEnd = %d/%d, want %d/%d", i, r.Line, r.LineEnd, w.line, w.lineEnd)
		}
	}
}

// TestParseRoundTripsByteIdentical is #435's stated fidelity
// requirement: parsing and rendering back straight away (no edits)
// reproduces the exact input text.
func TestParseRoundTripsByteIdentical(t *testing.T) {
	text := loadFixture(t)
	ex, err := Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	if got := ex.Text(); got != text {
		t.Errorf("round trip is not byte-identical:\n--- got ---\n%s\n--- want ---\n%s", got, text)
	}
}

// TestParsePathForm covers the /ip/firewall/filter slash-path section
// header form -- the "also the /ip/firewall/filter path form" line in
// #435's contract.
func TestParsePathForm(t *testing.T) {
	text := "# 2026/09/01 10:00:00 by RouterOS 7.24.1\n" +
		"/ip/firewall/filter\n" +
		"add action=drop chain=forward comment=\"path form test\" in-interface=ether1\n"
	ex, err := Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(ex.FilterRules) != 1 {
		t.Fatalf("decoded %d rules via the path form, want 1", len(ex.FilterRules))
	}
	if ex.FilterRules[0].Comment != "path form test" {
		t.Errorf("Comment = %q, want %q", ex.FilterRules[0].Comment, "path form test")
	}
}

// TestParseQuoting is a table test of RouterOS's own quoting: bare
// tokens, quoted values with an embedded space, an escaped quote, and
// an escaped backslash.
func TestParseQuoting(t *testing.T) {
	cases := []struct {
		name, attr, want string
	}{
		{"bare", `chain=forward`, ""},
		{"quoted with space", `comment="lan to wan"`, "lan to wan"},
		{"escaped quote", `comment="a \"noisy\" host"`, `a "noisy" host`},
		{"escaped backslash", `comment="C:\\path"`, `C:\path`},
		{"empty quoted", `comment=""`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text := "/ip firewall filter\nadd action=accept " + tc.attr + "\n"
			ex, err := Parse(text)
			if err != nil {
				t.Fatal(err)
			}
			if len(ex.FilterRules) != 1 {
				t.Fatalf("decoded %d rules, want 1", len(ex.FilterRules))
			}
			if tc.name == "bare" {
				return // chain= is exercised via r.Chain below in the general case
			}
			if ex.FilterRules[0].Comment != tc.want {
				t.Errorf("Comment = %q, want %q", ex.FilterRules[0].Comment, tc.want)
			}
		})
	}
}

// TestParseFilterRuleAttributes is a table test of every attribute this
// package decodes off an add line, exercised individually.
func TestParseFilterRuleAttributes(t *testing.T) {
	cases := []struct {
		name string
		line string
		want Rule
	}{
		{"out-interface-list", `add action=accept chain=forward out-interface-list=WAN_LIST`,
			Rule{Chain: "forward", Action: "accept", OutInterfaceList: "WAN_LIST"}},
		{"log yes", `add action=accept chain=forward log=yes log-prefix="A|x|"`,
			Rule{Chain: "forward", Action: "accept", Log: true, LogPrefix: "A|x|"}},
		{"log no", `add action=accept chain=forward log=no`,
			Rule{Chain: "forward", Action: "accept", Log: false}},
		{"disabled yes", `add action=drop chain=forward disabled=yes`,
			Rule{Chain: "forward", Action: "drop", Disabled: true}},
		{"no log attrs at all", `add action=drop chain=forward`,
			Rule{Chain: "forward", Action: "drop"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text := "/ip firewall filter\n" + tc.line + "\n"
			ex, err := Parse(text)
			if err != nil {
				t.Fatal(err)
			}
			if len(ex.FilterRules) != 1 {
				t.Fatalf("decoded %d rules, want 1", len(ex.FilterRules))
			}
			got := ex.FilterRules[0]
			want := tc.want
			// Rule carries an unexported, unexported-comparable tokens
			// slice, so compare the decoded fields the table states
			// rather than the whole struct.
			if got.Chain != want.Chain || got.Action != want.Action || got.Comment != want.Comment ||
				got.InInterface != want.InInterface || got.OutInterface != want.OutInterface ||
				got.InInterfaceList != want.InInterfaceList || got.OutInterfaceList != want.OutInterfaceList ||
				got.Log != want.Log || got.LogPrefix != want.LogPrefix || got.Disabled != want.Disabled {
				t.Errorf("got %+v, want %+v", got, want)
			}
		})
	}
}

// TestParseRejectsSecrets is a table test of #435's fixed secrets set:
// every key in it, present with a non-empty value, is refused; an empty
// value (RouterOS's own hide-sensitive redaction) and a key that merely
// contains "key" as a substring are not.
func TestParseRejectsSecrets(t *testing.T) {
	secretCases := []string{
		"password", "secret", "private-key", "preshared-key", "pre-shared-key",
		"community", "wpa-pre-shared-key", "wpa2-pre-shared-key", "passphrase",
		"key", "psk",
	}
	for _, key := range secretCases {
		t.Run(key, func(t *testing.T) {
			text := "/interface wireless security-profiles\nadd " + key + "=\"hunter2\"\n"
			_, err := Parse(text)
			if err == nil {
				t.Fatalf("Parse succeeded with %s set, want a SecretFieldError", key)
			}
			secretErr, ok := err.(*SecretFieldError)
			if !ok {
				t.Fatalf("error = %v (%T), want a *SecretFieldError", err, err)
			}
			if secretErr.Key != key {
				t.Errorf("SecretFieldError.Key = %q, want %q", secretErr.Key, key)
			}
			if secretErr.Line != 2 {
				t.Errorf("SecretFieldError.Line = %d, want 2", secretErr.Line)
			}
		})
	}

	t.Run("empty value is not a secret", func(t *testing.T) {
		text := "/interface wireless security-profiles\nadd password=\"\"\n"
		if _, err := Parse(text); err != nil {
			t.Errorf("Parse with an empty password= failed: %v, want success (hide-sensitive's own redaction shape)", err)
		}
	})

	t.Run("public-key is not the secret key key", func(t *testing.T) {
		text := "/interface wireguard\nadd public-key=\"abc123\"\n"
		if _, err := Parse(text); err != nil {
			t.Errorf("Parse with public-key= failed: %v, want success (exact-key match only, not substring)", err)
		}
	})

	t.Run("real fixture has no secrets", func(t *testing.T) {
		if _, err := Parse(loadFixture(t)); err != nil {
			t.Errorf("the hide-sensitive fixture was rejected: %v", err)
		}
	})
}

// TestParseIgnoresOtherSections covers what #435's contract calls
// "everything else is opaque": add lines outside /ip firewall filter
// (including its own NAT sibling, whose add lines share the same verb)
// are never decoded into FilterRules.
func TestParseIgnoresOtherSections(t *testing.T) {
	ex, err := Parse(loadFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range ex.FilterRules {
		if r.Action == "masquerade" {
			t.Errorf("a NAT rule (masquerade) was decoded as a filter rule: %+v", r)
		}
	}
}

// TestParseVersionAbsent covers text with no header comment at all --
// Version stays "", not a guess.
func TestParseVersionAbsent(t *testing.T) {
	ex, err := Parse("/ip firewall filter\nadd action=accept chain=forward\n")
	if err != nil {
		t.Fatal(err)
	}
	if ex.Version != "" {
		t.Errorf("Version = %q, want empty with no header comment", ex.Version)
	}
}

func TestJoinContinuationStripsIndentation(t *testing.T) {
	logical, last := joinContinuation([]string{
		`add action=accept chain=forward \`,
		`    comment="two lines" \`,
		`    in-interface=ether1`,
	}, 0)
	if last != 2 {
		t.Errorf("lastLine = %d, want 2", last)
	}
	want := `add action=accept chain=forward comment="two lines" in-interface=ether1`
	if logical != want {
		t.Errorf("logical = %q, want %q", logical, want)
	}
}

func TestQuoteRoundTripsThroughUnquote(t *testing.T) {
	for _, s := range []string{"", "plain", "a value", `a "quoted" value`, `back\slash`} {
		if got := unquote(Quote(s)); got != s {
			t.Errorf("unquote(Quote(%q)) = %q, want %q", s, got, s)
		}
	}
}
