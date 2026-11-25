// SPDX-License-Identifier: AGPL-3.0-only

package servertls

import "testing"

// #1451: Current is what main.go reads to report a reload's fingerprint.
func TestCurrentFollowsReload(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := writeThrowawayCert(t, dir)
	cfg := Config{CertFile: certPath, KeyFile: keyPath}
	first, _, _, err := Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	r := NewReloader(cfg, first)
	if string(r.Current().Certificate[0]) != string(first.Certificate[0]) {
		t.Fatal("Current() is not the certificate the reloader was built with")
	}
	renewedCert, renewedKey := writeThrowawayCert(t, t.TempDir())
	copyFile(t, renewedCert, certPath)
	copyFile(t, renewedKey, keyPath)
	reloaded, err := r.Reload()
	if err != nil {
		t.Fatal(err)
	}
	if string(r.Current().Certificate[0]) != string(reloaded.Certificate[0]) || string(reloaded.Certificate[0]) == string(first.Certificate[0]) {
		t.Error("Current() did not move to the reloaded certificate")
	}
}
