// SPDX-License-Identifier: AGPL-3.0-only

package servertls

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"
)

// A renewed certificate on disk must reach both listeners without a
// restart (#294 item 5). The failure this prevents arrives silently
// weeks after the renewal, as an expired certificate on the HTTPS and
// syslog listeners at once.
func TestReloaderPicksUpAReplacedCertificate(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := writeThrowawayCert(t, dir)
	cfg := Config{CertFile: certPath, KeyFile: keyPath}

	first, _, _, err := Load(cfg)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	r := NewReloader(cfg, first)

	served, err := r.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatalf("GetCertificate: %v", err)
	}
	if string(served.Certificate[0]) != string(first.Certificate[0]) {
		t.Fatal("the reloader is not serving the certificate it was built with")
	}

	// Replace what is on disk, as certbot would.
	renewedDir := t.TempDir()
	renewedCert, renewedKey := writeThrowawayCert(t, renewedDir)
	copyFile(t, renewedCert, certPath)
	copyFile(t, renewedKey, keyPath)

	// Nothing changes until told: mikroview cannot tell a finished
	// renewal from a half-written one, so it does not guess.
	stillOld, _ := r.GetCertificate(&tls.ClientHelloInfo{})
	if string(stillOld.Certificate[0]) != string(first.Certificate[0]) {
		t.Error("the certificate changed without a reload being asked for")
	}

	reloaded, err := r.Reload()
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if string(reloaded.Certificate[0]) == string(first.Certificate[0]) {
		t.Fatal("Reload returned the old certificate")
	}

	nowServed, _ := r.GetCertificate(&tls.ClientHelloInfo{})
	if string(nowServed.Certificate[0]) != string(reloaded.Certificate[0]) {
		t.Error("the reloaded certificate is not what handshakes will get")
	}
}

// The half that matters most: an operator who sends the signal expecting
// an improvement must not get an outage out of it, and would have little
// reason to connect the two.
func TestReloaderKeepsServingWhenAReloadFails(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := writeThrowawayCert(t, dir)
	cfg := Config{CertFile: certPath, KeyFile: keyPath}

	first, _, _, err := Load(cfg)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	r := NewReloader(cfg, first)

	// A half-written renewal.
	if err := os.WriteFile(certPath, []byte("-----BEGIN CERTIFICATE-----\ntrunc"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := r.Reload(); err == nil {
		t.Fatal("Reload accepted a broken certificate")
	}
	served, err := r.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatalf("GetCertificate after a failed reload: %v", err)
	}
	if string(served.Certificate[0]) != string(first.Certificate[0]) {
		t.Error("a failed reload changed what is being served -- the listener should keep the certificate it had")
	}
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	data, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

var _ = filepath.Join
