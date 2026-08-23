// SPDX-License-Identifier: AGPL-3.0-only

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
	"errors"
	"fmt"
	"io/fs"
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
//
// Every error is fatal and the caller should stop starting (#535).
// That includes the two failures this used to survive: a stored CA that
// is present but unusable, and a newly generated CA that cannot be
// written to StorePath.
//
// Both used to be warnings, on the reasoning that the in-memory
// certificate is genuinely usable so the server may as well serve. What
// that missed is who else is affected. A regenerated CA is a *new*
// trust anchor: the router pushing syslog over TLS rejects it and stops
// delivering, so mikroview goes quiet in exactly the way it exists to
// warn about. Refusing to start puts the operator in the logs, where
// the reason is stated plainly, instead of leaving a warning to scroll
// past while everything looks fine.
//
// reusedCA reports whether the CA came from StorePath rather than being
// minted here, so the caller can say "reusing" instead of "generated".
// It used to say "generated a local CA" on every start, reused or not,
// which is what made a genuinely regenerating deployment impossible to
// spot from the logs.
//
// Only the CA is treated this way. A missing or unusable *leaf* costs
// one regeneration and no re-trust, because clients pin the CA -- see
// loadStoredLeaf.
func Load(cfg Config) (cert tls.Certificate, caCertPEM []byte, reusedCA bool, err error) {
	if cfg.CertFile != "" && cfg.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return tls.Certificate{}, nil, false, fmt.Errorf("servertls: loading %s/%s: %w", cfg.CertFile, cfg.KeyFile, err)
		}
		return cert, nil, false, nil
	}

	hosts := cfg.Hosts
	if len(hosts) == 0 {
		hosts = defaultHosts
	}
	sortedHosts := slices.Clone(hosts)
	sort.Strings(sortedHosts)

	var ca *caPair
	if cfg.StorePath != "" {
		stored, err := loadStoredCA(cfg.StorePath)
		if err != nil {
			return tls.Certificate{}, nil, false, fmt.Errorf(
				"servertls: %w -- refusing to start, because generating a new CA "+
					"would break every browser, reverse proxy and router that trusts "+
					"the current one. Fix the permissions or contents of %s, or remove "+
					"the files there to deliberately start over with a new CA that "+
					"everything must re-trust", err, cfg.StorePath)
		}
		ca = stored
		reusedCA = ca != nil
	}
	if ca == nil {
		var err error
		ca, err = generateCA()
		if err != nil {
			return tls.Certificate{}, nil, false, fmt.Errorf("servertls: generating local CA: %w", err)
		}
	}

	if cfg.StorePath != "" {
		if cert, ok := loadStoredLeaf(cfg.StorePath, sortedHosts, ca); ok {
			return cert, ca.certPEM, reusedCA, nil
		}
	}

	leaf, err := generateLeaf(ca, sortedHosts)
	if err != nil {
		return tls.Certificate{}, nil, false, fmt.Errorf("servertls: generating leaf certificate: %w", err)
	}

	if cfg.StorePath != "" {
		if saveErr := saveStored(cfg.StorePath, ca, leaf, sortedHosts); saveErr != nil {
			return tls.Certificate{}, nil, false, fmt.Errorf(
				"servertls: persisting to %s: %w -- refusing to start, because a CA "+
					"that cannot be saved is regenerated on every restart, and each "+
					"one has to be trusted again by every browser, reverse proxy and "+
					"router. Give mikroview a writable data directory (the shipped "+
					"deploy/docker-compose.yml mounts the mikroview-data volume at "+
					"/var/lib/mikroview for this)", cfg.StorePath, saveErr)
		}
	}

	return leaf, ca.certPEM, reusedCA, nil
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
// The three outcomes are deliberately distinct, because they used to be
// one (#535). Every branch below returned a bare nil, so "no CA has ever
// been stored" and "the CA is right there but I cannot read it" were the
// same answer -- and the answer to both was to mint a new CA and
// overwrite the old one, destroying the only copy of the trust anchor
// the router was pinned to. The log line said "generated a local CA",
// identical to a first run.
//
//   - (nil, nil)  no CA stored yet. A first run; the caller generates.
//   - (ca, nil)   a usable CA. Reused.
//   - (nil, err)  CA material is present and cannot be used. Fatal:
//     the caller stops rather than replacing it.
//
// An expired CA is the first case, not the third: it is intact, its time
// is simply up, and regenerating is the only thing left to do. Ten years
// on, that re-trust is unavoidable rather than a fault.
func loadStoredCA(storePath string) (*caPair, error) {
	caCertPath, caKeyPath, _, _, _ := storePaths(storePath)
	certPEM, err := os.ReadFile(caCertPath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", caCertPath, err)
	}
	keyPEM, err := os.ReadFile(caKeyPath)
	if errors.Is(err, fs.ErrNotExist) {
		// The certificate is there and its key is not. Not a first run,
		// and not recoverable: without the key nothing can be signed
		// with this CA again.
		return nil, fmt.Errorf("%s exists but its key %s is missing", caCertPath, caKeyPath)
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", caKeyPath, err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, fmt.Errorf("%s is not valid PEM", caCertPath)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", caCertPath, err)
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, fmt.Errorf("%s is not valid PEM", caKeyPath)
	}
	key, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", caKeyPath, err)
	}
	if time.Now().After(cert.NotAfter) {
		return nil, nil
	}
	return &caPair{cert: cert, key: key, certPEM: certPEM}, nil
}

// loadStoredLeaf returns the previously-generated leaf certificate, if
// one exists, was issued for exactly this sorted host list, isn't within
// its renewal window, and still chains to ca -- any mismatch (never
// generated, host list changed, close to expiry, corrupt, issued by a
// CA that is no longer the one in use) is reported via ok=false so the
// caller regenerates (reusing the already-loaded CA, so no re-trust is
// needed just because the leaf's SAN list changed).
//
// The chain check is the part that is easy to leave out. If ca.crt or
// ca.key becomes unreadable or corrupt while the three leaf files stay
// intact, the caller above mints a fresh CA and would otherwise keep
// serving the old leaf -- a pair that validates against nothing, so
// every client that had trusted the original CA fails, and the only
// clue is a certificate error. Regenerating instead costs one leaf and
// leaves the already-trusted CA arrangement intact where it can be.
func loadStoredLeaf(storePath string, sortedHosts []string, ca *caPair) (tls.Certificate, bool) {
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
	// Verified against this CA specifically, not against the host's
	// trust store: a local CA is in neither, and the question here is
	// only "did the CA we are about to serve alongside sign this leaf".
	pool := x509.NewCertPool()
	pool.AddCert(ca.cert)
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots: pool,
		// The leaf is checked for expiry above; this call is about the
		// signature chain, and passing the same clock twice would make
		// a renewal-window miss look like a chain failure.
		CurrentTime: leaf.NotBefore,
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		return tls.Certificate{}, false
	}
	return cert, true
}

// saveStored returns the first error it hits (e.g. a read-only
// filesystem -- see the "hardened container" smoke test) rather than
// silently discarding it: persistence failing is still never fatal to
// Load (the caller already has a working in-memory cert regardless),
// but an operator running with a read-only root filesystem deserves to
// know the CA is being regenerated -- and re-trusted -- on every
// restart instead of persisting once, the same way flags/auth/detector-
// settings all surface their own persistence failures as a warning.
func saveStored(storePath string, ca *caPair, cert tls.Certificate, sortedHosts []string) error {
	if err := os.MkdirAll(storePath, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", storePath, err)
	}
	caCertPath, caKeyPath, leafCertPath, leafKeyPath, metaPath := storePaths(storePath)

	caKeyDER, err := x509.MarshalECPrivateKey(ca.key)
	if err != nil {
		return fmt.Errorf("marshaling CA key: %w", err)
	}
	caKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: caKeyDER})
	if err := writeSecret(caCertPath, ca.certPEM); err != nil {
		return err
	}
	if err := writeSecret(caKeyPath, caKeyPEM); err != nil {
		return err
	}

	leafKeyDER, err := x509.MarshalECPrivateKey(cert.PrivateKey.(*ecdsa.PrivateKey))
	if err != nil {
		return fmt.Errorf("marshaling leaf key: %w", err)
	}
	leafCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]})
	leafKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: leafKeyDER})
	if err := writeSecret(leafCertPath, leafCertPEM); err != nil {
		return err
	}
	if err := writeSecret(leafKeyPath, leafKeyPEM); err != nil {
		return err
	}

	metaData, err := json.Marshal(leafMeta{Hosts: sortedHosts})
	if err != nil {
		return fmt.Errorf("marshaling leaf metadata: %w", err)
	}
	if err := writeSecret(metaPath, metaData); err != nil {
		return err
	}
	return nil
}

// writeSecret writes b to path at filePermission, and enforces that mode
// on a file that already exists.
//
// os.WriteFile's mode argument only applies when it *creates* the file:
// on a rewrite the existing permissions are kept. So the 0600 intent
// held on a fresh install and silently did not on any path where the
// file already existed with wider permissions -- a restore from a
// backup taken with a different umask, a bind-mounted host directory, a
// volume copied between machines. These are the CA and leaf private
// keys; a world-readable one would stay world-readable across every
// subsequent restart, with nothing saying so. See #285.
func writeSecret(path string, b []byte) error {
	if err := os.WriteFile(path, b, filePermission); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := os.Chmod(path, filePermission); err != nil {
		return fmt.Errorf("setting permissions on %s: %w", path, err)
	}
	return nil
}
