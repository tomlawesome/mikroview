// SPDX-License-Identifier: AGPL-3.0-only

package persist

import (
	"errors"
	"strings"
	"testing"
)

// #1451: a connection failure must never carry the DSN password into a log.

func TestDSNPassword(t *testing.T) {
	cases := map[string]string{
		"postgres://mv:s3cret@db.example.net:5432/mikroview": "s3cret",
		"  postgres://mv:s3cret@db.example.net/x  ":          "s3cret",
		"postgres://mv@db.example.net/x":                     "", // user, no password
		"postgres://db.example.net/x":                        "",
		"host=db.example.net user=mv password=abc":           "", // keyword form: nothing to parse
		"":               "",
		"postgres://%zz": "", // unparseable never fails
	}
	for dsn, want := range cases {
		if got := dsnPassword(dsn); got != want {
			t.Errorf("dsnPassword(%q) = %q, want %q", dsn, got, want)
		}
	}
}

func TestRedactRemovesTheDSNAndThePasswordSeparately(t *testing.T) {
	dsn := "postgres://mv:s3cretpw@db.example.net:5432/mikroview"
	// pgx echoing the whole DSN.
	got := redact(errors.New("cannot connect to "+dsn+": refused"), dsn).Error()
	if strings.Contains(got, "s3cretpw") || strings.Contains(got, dsn) || !strings.Contains(got, "<dsn>") {
		t.Errorf("verbatim DSN: %q", got)
	}
	// pgx quoting a normalised form: the exact DSN is absent, the password is not.
	got = redact(errors.New(`failed to connect to host=db.example.net user=mv password=s3cretpw`), dsn).Error()
	if strings.Contains(got, "s3cretpw") || !strings.Contains(got, "<redacted>") {
		t.Errorf("partial form: %q", got)
	}
	// Nothing sensitive: message unchanged.
	if got := redact(errors.New("timeout"), dsn).Error(); got != "timeout" {
		t.Errorf("plain error changed to %q", got)
	}
	if got := redact(errors.New("boom"), "").Error(); got != "boom" {
		t.Errorf("empty dsn: %q", got)
	}
}
