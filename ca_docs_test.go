// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The API reference's GET /ca.crt row must not tell an operator running
// tls.enabled: false that the endpoint does not exist for them.
//
// It does exist for them. A CA is generated whenever cfg.TLS.Enabled ||
// cfg.Listen.SyslogTLS != "", and the handler below is registered on
// caCertPEM != nil alone -- cfg.TLS.Enabled does not gate it. So the
// reverse-proxy deployment, which turns mikroview's own HTTP TLS off and
// still runs the syslog TLS listener, serves /ca.crt over plain HTTP.
//
// That deployment is exactly the one that needs the CA most: its router
// connects to the syslog port directly and must trust the certificate
// mikroview generated for it. #382 found the row claiming the opposite,
// while the TLS section 450 lines further down documented the real rule
// correctly and told the reader to fetch GET /ca.crt for this case -- so
// the file contradicted itself, and the wrong half was the half in the
// API reference.
func TestCACertDocRowDoesNotExcludeTLSDisabled(t *testing.T) {
	docs, err := os.ReadFile("docs/configuration.md")
	if err != nil {
		t.Fatalf("reading the configuration reference: %v", err)
	}

	m := regexp.MustCompile("(?m)^\\| `GET /ca\\.crt` \\|(.*)$").FindSubmatch(docs)
	if m == nil {
		t.Fatal("found no GET /ca.crt row in docs/configuration.md's API reference -- this test is not looking where it thinks it is")
	}
	row := string(m[1])

	// Targeted at the *exclusion* claim, not at any mention of the
	// setting: the corrected row names tls.enabled: false too, to tell
	// the reverse-proxy reader the endpoint is theirs over plain HTTP.
	// The clause bound stops at a sentence end, so "never for an
	// operator-supplied cert. With `tls.enabled: false` ..." reads as
	// the two separate statements it is.
	if excludes := regexp.MustCompile(`(?:never|not present|absent|only present when TLS is on)[^.]{0,60}tls\.enabled: false`); excludes.MatchString(row) {
		t.Errorf("the GET /ca.crt row says tls.enabled: false excludes the endpoint, which main.go's caCertPEM != nil registration does not: %q", row)
	}
	if !strings.Contains(row, "syslogTls") {
		t.Errorf("the GET /ca.crt row should name listen.syslogTls as the other thing that makes mikroview generate a CA, so the reverse-proxy deployment knows the endpoint is there for it: %q", row)
	}
}
