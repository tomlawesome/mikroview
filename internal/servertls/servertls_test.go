// SPDX-License-Identifier: AGPL-3.0-only

package servertls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func leafOf(t *testing.T, cert tls.Certificate) *x509.Certificate {
	t.Helper()
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	return leaf
}

func TestGenerateCoversConfiguredHosts(t *testing.T) {
	cert, caPEM, _, err := Load(Config{Hosts: []string{"mikroview.local", "192.168.1.50"}})
	if err != nil {
		t.Fatal(err)
	}
	if caPEM == nil {
		t.Fatal("expected a generated CA's PEM bytes, got nil")
	}
	leaf := leafOf(t, cert)
	if len(leaf.DNSNames) != 1 || leaf.DNSNames[0] != "mikroview.local" {
		t.Errorf("expected DNSNames [mikroview.local], got %v", leaf.DNSNames)
	}
	if len(leaf.IPAddresses) != 1 || !leaf.IPAddresses[0].Equal(net.ParseIP("192.168.1.50")) {
		t.Errorf("expected IPAddresses [192.168.1.50], got %v", leaf.IPAddresses)
	}
}

func TestGenerateDefaultsHostsWhenUnset(t *testing.T) {
	cert, _, _, err := Load(Config{})
	if err != nil {
		t.Fatal(err)
	}
	leaf := leafOf(t, cert)
	if len(leaf.DNSNames) != 1 || leaf.DNSNames[0] != "localhost" {
		t.Errorf("expected DNSNames [localhost], got %v", leaf.DNSNames)
	}
	if len(leaf.IPAddresses) != 1 || !leaf.IPAddresses[0].Equal(net.ParseIP("127.0.0.1")) {
		t.Errorf("expected IPAddresses [127.0.0.1], got %v", leaf.IPAddresses)
	}
}

func TestPersistenceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{Hosts: []string{"mikroview.local"}, StorePath: dir}

	cert1, caPEM1, _, err := Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cert2, caPEM2, _, err := Load(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if string(caPEM1) != string(caPEM2) {
		t.Error("expected the second Load to reuse the persisted CA, got a different one")
	}
	if leafOf(t, cert1).SerialNumber.Cmp(leafOf(t, cert2).SerialNumber) != 0 {
		t.Error("expected the second Load to reuse the persisted leaf cert, got a different one")
	}
}

func TestHostsChangeRegeneratesLeafButKeepsCA(t *testing.T) {
	dir := t.TempDir()

	cert1, caPEM1, _, err := Load(Config{Hosts: []string{"mikroview.local"}, StorePath: dir})
	if err != nil {
		t.Fatal(err)
	}
	cert2, caPEM2, _, err := Load(Config{Hosts: []string{"mikroview.local", "other.local"}, StorePath: dir})
	if err != nil {
		t.Fatal(err)
	}

	if string(caPEM1) != string(caPEM2) {
		t.Error("expected the same CA to be reused across a hosts-list change, got a different one")
	}
	leaf1, leaf2 := leafOf(t, cert1), leafOf(t, cert2)
	if leaf1.SerialNumber.Cmp(leaf2.SerialNumber) == 0 {
		t.Error("expected a new leaf cert after the hosts list changed, got the same one")
	}
	if len(leaf2.DNSNames) != 2 {
		t.Errorf("expected the regenerated leaf to cover both hosts, got %v", leaf2.DNSNames)
	}
}

func TestNearExpiryLeafIsRenewed(t *testing.T) {
	dir := t.TempDir()
	hosts := []string{"localhost"}

	ca, err := generateCA()
	if err != nil {
		t.Fatal(err)
	}
	// Build a leaf that's within the 30-day renewal window by hand
	// (generateLeaf always uses the full leafValidity), then persist it
	// exactly as saveStored would.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "mikroview"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour), // well within renewalWindow
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     hosts,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	nearExpiry, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	saveStored(dir, ca, nearExpiry, hosts)

	cert, caPEM, _, err := Load(Config{Hosts: hosts, StorePath: dir})
	if err != nil {
		t.Fatal(err)
	}
	if string(caPEM) != string(ca.certPEM) {
		t.Error("expected the existing CA to still be reused for the renewed leaf")
	}
	if leafOf(t, cert).SerialNumber.Cmp(big.NewInt(2)) == 0 {
		t.Error("expected the near-expiry leaf to be renewed with a new serial, got the same one")
	}
}

// TestUnwritableStorePathSurfacesPersistErrButStillLoads reproduces the
// "hardened container" (--read-only root filesystem) scenario found via
// the CI smoke test: generation must still succeed and return a usable
// cert, but the caller needs to know persistence silently failed so it
// can warn an operator, rather than the CA quietly regenerating (and
// needing to be re-trusted) on every restart with no indication why.
func TestUnwritableStorePathSurfacesPersistErrButStillLoads(t *testing.T) {
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o500); err != nil { // read+execute, no write
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(parent, 0o700) }) // let t.TempDir() clean up

	storePath := filepath.Join(parent, "tls")

	cert, caPEM, persistErr, err := Load(Config{Hosts: []string{"mikroview.local"}, StorePath: storePath})
	if err != nil {
		t.Fatalf("expected Load to still succeed with an unwritable store path, got err: %v", err)
	}
	if persistErr == nil {
		t.Error("expected a non-nil persistErr for an unwritable store path")
	}
	if caPEM == nil {
		t.Error("expected a usable generated CA despite the persist failure")
	}
	_ = leafOf(t, cert)
}

func TestFilesSkipGeneration(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := writeThrowawayCert(t, dir)

	cert, caPEM, _, err := Load(Config{CertFile: certPath, KeyFile: keyPath})
	if err != nil {
		t.Fatal(err)
	}
	if caPEM != nil {
		t.Error("expected no generated CA when CertFile/KeyFile are supplied, got non-nil")
	}
	leaf := leafOf(t, cert)
	if leaf.Subject.CommonName != "throwaway-test-cert" {
		t.Errorf("expected the supplied cert to be used verbatim, got CommonName %q", leaf.Subject.CommonName)
	}
}

func TestCorruptStoreFallsBackToRegeneration(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ca.crt"), []byte("not a cert"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ca.key"), []byte("not a key"), 0o600); err != nil {
		t.Fatal(err)
	}

	cert, caPEM, _, err := Load(Config{StorePath: dir})
	if err != nil {
		t.Fatalf("expected a corrupt store to be treated as absent, not fail Load: %v", err)
	}
	if caPEM == nil {
		t.Error("expected a freshly generated CA despite the corrupt store")
	}
	_ = leafOf(t, cert)
}

// writeThrowawayCert writes a minimal self-signed cert/key pair to dir,
// standing in for an "operator-supplied" cert.
func writeThrowawayCert(t *testing.T, dir string) (certPath, keyPath string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "throwaway-test-cert"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	certPath = filepath.Join(dir, "throwaway.crt")
	keyPath = filepath.Join(dir, "throwaway.key")
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}

// os.WriteFile's mode argument applies only when it creates the file, so
// the 0600 intent held on a fresh install and silently did not wherever
// the file already existed with wider permissions -- a restore from a
// backup taken under a different umask, a bind-mounted host directory, a
// volume copied between machines. These are the CA and leaf private
// keys. See #285.
func TestPersistedKeysAreTightenedOnRewrite(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{Hosts: []string{"localhost"}, StorePath: dir}

	if _, _, err, _ := Load(cfg); err != nil {
		t.Fatalf("first Load: %v", err)
	}

	// Widen everything, the way a restore or a bind mount would.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("nothing was persisted, so this test would prove nothing")
	}
	for _, e := range entries {
		if err := os.Chmod(filepath.Join(dir, e.Name()), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Force a rewrite by asking for a different SAN set.
	cfg.Hosts = []string{"localhost", "mikroview.local"}
	if _, _, err, _ := Load(cfg); err != nil {
		t.Fatalf("second Load: %v", err)
	}

	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("%s has mode %o after rewrite, want 600 -- a pre-existing world-readable key stays readable", e.Name(), got)
		}
	}
}
