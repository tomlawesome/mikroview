// Package servertls provides mikroview's TLS certificate, either loaded
// from an operator-supplied cert/key pair or, as the zero-config
// default, a self-generated local certificate authority + leaf
// certificate. The local CA is trust-on-first-use, not a globally
// trusted root -- fine for an admin interface on infrastructure you
// already control, not a substitute for a real cert if you have one
// (Let's Encrypt, a corporate CA, etc -- see Config.CertFile/KeyFile).
package servertls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"time"
)

// caValidity/leafValidity are both long -- rotating a locally-trusted-
// on-first-use CA isn't providing meaningful security benefit at this
// trust tier, so there's no reason to force churn (and every rotation
// means asking an operator/reverse-proxy to re-establish trust).
// renewalWindow is how far ahead of a leaf's actual expiry Load
// proactively regenerates it (still signed by the same, already-trusted
// CA -- see generateLeaf).
const (
	caValidity     = 10 * 365 * 24 * time.Hour
	leafValidity   = 10 * 365 * 24 * time.Hour
	renewalWindow  = 30 * 24 * time.Hour
	filePermission = 0o600
)

var defaultHosts = []string{"localhost", "127.0.0.1"}

// Config is what the caller (main.go) already has in hand from
// internal/config.TLS.
type Config struct {
	// CertFile/KeyFile: operator-supplied cert -- skips local-CA
	// generation entirely if both are set.
	CertFile string
	KeyFile  string
	// Hosts: SANs a generated leaf cert should cover. Defaults to
	// localhost/127.0.0.1 if empty.
	Hosts []string
	// StorePath: where a generated CA + leaf persist across restarts,
	// so the trust step is a one-time cost. Left empty, generation
	// still works, it just produces a fresh (untrusted-again) CA every
	// restart -- the same optional-persistence contract every other
	// store in this codebase has (see flags.Open's doc comment).
	StorePath string
}

// Load returns a ready-to-serve certificate and, when mikroview
// generated its own CA, that CA's PEM bytes (for serving at a fixed,
// unauthenticated path so a browser or reverse proxy can fetch it to
// establish trust) -- nil when CertFile/KeyFile were used instead, since
// there's no mikroview-generated CA in that case.
func Load(cfg Config) (tls.Certificate, []byte, error) {
	if cfg.CertFile != "" && cfg.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return tls.Certificate{}, nil, fmt.Errorf("servertls: loading %s/%s: %w", cfg.CertFile, cfg.KeyFile, err)
		}
		return cert, nil, nil
	}

	hosts := cfg.Hosts
	if len(hosts) == 0 {
		hosts = defaultHosts
	}
	sortedHosts := slices.Clone(hosts)
	sort.Strings(sortedHosts)

	var ca *caPair
	if cfg.StorePath != "" {
		ca = loadStoredCA(cfg.StorePath)
	}
	if ca == nil {
		var err error
		ca, err = generateCA()
		if err != nil {
			return tls.Certificate{}, nil, fmt.Errorf("servertls: generating local CA: %w", err)
		}
	}

	if cfg.StorePath != "" {
		if cert, ok := loadStoredLeaf(cfg.StorePath, sortedHosts); ok {
			return cert, ca.certPEM, nil
		}
	}

	cert, err := generateLeaf(ca, sortedHosts)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("servertls: generating leaf certificate: %w", err)
	}

	if cfg.StorePath != "" {
		// Persistence failing is never fatal -- generation already
		// succeeded, so mikroview still starts; it just means the next
		// restart regenerates (a fresh CA, a fresh trust step) instead
		// of reusing this one. Same "swallow write failures, in-memory
		// state stays correct either way" reasoning
		// detect.SettingsStore.persistLocked already documents.
		saveStored(cfg.StorePath, ca, cert, sortedHosts)
	}

	return cert, ca.certPEM, nil
}

type caPair struct {
	cert    *x509.Certificate
	key     *ecdsa.PrivateKey
	certPEM []byte
}

func generateCA() (*caPair, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "mikroview local CA", Organization: []string{"mikroview"}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(caValidity),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	return &caPair{
		cert:    cert,
		key:     key,
		certPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
	}, nil
}

func generateLeaf(ca *caPair, hosts []string) (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := randomSerial()
	if err != nil {
		return tls.Certificate{}, err
	}
	dnsNames, ips := splitHosts(hosts)
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "mikroview"},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(leafValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     dnsNames,
		IPAddresses:  ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return tls.X509KeyPair(certPEM, keyPEM)
}

func splitHosts(hosts []string) (dnsNames []string, ips []net.IP) {
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			ips = append(ips, ip)
		} else {
			dnsNames = append(dnsNames, h)
		}
	}
	return dnsNames, ips
}

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	return rand.Int(rand.Reader, limit)
}

// --- persistence -----------------------------------------------------

type leafMeta struct {
	Hosts []string `json:"hosts"`
}

func storePaths(storePath string) (caCert, caKey, leafCert, leafKey, meta string) {
	return filepath.Join(storePath, "ca.crt"),
		filepath.Join(storePath, "ca.key"),
		filepath.Join(storePath, "leaf.crt"),
		filepath.Join(storePath, "leaf.key"),
		filepath.Join(storePath, "leaf-meta.json")
}

// loadStoredCA returns the previously-generated CA at storePath, or nil
// if none exists or it's unreadable/corrupt -- treated as "not found,"
// same "a corrupted state file never blocks startup" philosophy every
// other optional store in this codebase already follows (flags.Open,
// auth.Open, detect.OpenSettingsStore), so Load falls back to
// generating a fresh one.
func loadStoredCA(storePath string) *caPair {
	caCertPath, caKeyPath, _, _, _ := storePaths(storePath)
	certPEM, err := os.ReadFile(caCertPath)
	if err != nil {
		return nil
	}
	keyPEM, err := os.ReadFile(caKeyPath)
	if err != nil {
		return nil
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil
	}
	key, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil
	}
	if time.Now().After(cert.NotAfter) {
		return nil
	}
	return &caPair{cert: cert, key: key, certPEM: certPEM}
}

// loadStoredLeaf returns the previously-generated leaf certificate, if
// one exists, was issued for exactly this sorted host list, and isn't
// within its renewal window -- any mismatch (never generated, host list
// changed, close to expiry, corrupt) is reported via ok=false so the
// caller regenerates (reusing the already-loaded CA, so no re-trust is
// needed just because the leaf's SAN list changed).
func loadStoredLeaf(storePath string, sortedHosts []string) (tls.Certificate, bool) {
	_, _, leafCertPath, leafKeyPath, metaPath := storePaths(storePath)

	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		return tls.Certificate{}, false
	}
	var meta leafMeta
	if err := json.Unmarshal(metaData, &meta); err != nil {
		return tls.Certificate{}, false
	}
	if !slices.Equal(meta.Hosts, sortedHosts) {
		return tls.Certificate{}, false
	}

	cert, err := tls.LoadX509KeyPair(leafCertPath, leafKeyPath)
	if err != nil {
		return tls.Certificate{}, false
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil || time.Until(leaf.NotAfter) < renewalWindow {
		return tls.Certificate{}, false
	}
	return cert, true
}

func saveStored(storePath string, ca *caPair, cert tls.Certificate, sortedHosts []string) {
	if err := os.MkdirAll(storePath, 0o700); err != nil {
		return
	}
	caCertPath, caKeyPath, leafCertPath, leafKeyPath, metaPath := storePaths(storePath)

	caKeyDER, err := x509.MarshalECPrivateKey(ca.key)
	if err != nil {
		return
	}
	caKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: caKeyDER})
	os.WriteFile(caCertPath, ca.certPEM, filePermission)
	os.WriteFile(caKeyPath, caKeyPEM, filePermission)

	leafKeyDER, err := x509.MarshalECPrivateKey(cert.PrivateKey.(*ecdsa.PrivateKey))
	if err != nil {
		return
	}
	leafCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]})
	leafKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: leafKeyDER})
	os.WriteFile(leafCertPath, leafCertPEM, filePermission)
	os.WriteFile(leafKeyPath, leafKeyPEM, filePermission)

	metaData, err := json.Marshal(leafMeta{Hosts: sortedHosts})
	if err != nil {
		return
	}
	os.WriteFile(metaPath, metaData, filePermission)
}
