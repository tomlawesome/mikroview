// SPDX-License-Identifier: AGPL-3.0-only

package servertls

import (
	"crypto/tls"
	"sync"
)

// Reloader holds the certificate both listeners serve, and lets it be
// replaced without a restart (#294 item 5).
//
// The problem it solves: mikroview built a static []tls.Certificate at
// startup and never looked at the files again. An operator running
// certbot or cert-manager -- the normal way to have a real certificate
// -- gets a renewed one on disk and mikroview keeps serving the old one
// until someone restarts it. Renewal is automatic and the restart is
// not, so the failure arrives silently, weeks later, as an expired
// certificate on both the HTTPS and syslog listeners at once. A router
// with check-certificate=yes stops sending logs at that point, which is
// the same outage this product exists to prevent someone missing.
//
// Deliberately a whole certificate swap rather than watching the files:
// mikroview does not know when a renewal has finished writing, and
// half a certificate is worse than an old one. The operator says when,
// via SIGHUP -- which is what certbot's --deploy-hook and
// cert-manager's reloader sidecars already exist to send.
type Reloader struct {
	cfg Config

	mu   sync.RWMutex
	cert tls.Certificate
}

// NewReloader returns a Reloader already holding cert, which the caller
// has just loaded with the same cfg. Taking the certificate rather than
// loading one keeps the startup path unchanged: it still fails loudly
// and exits if the first load fails, which a lazy load here would turn
// into a runtime handshake error instead.
func NewReloader(cfg Config, cert tls.Certificate) *Reloader {
	return &Reloader{cfg: cfg, cert: cert}
}

// GetCertificate is the tls.Config hook both listeners use, so a swap
// takes effect on the next handshake without touching either server.
//
// Existing connections keep the certificate they negotiated with, which
// is correct: TLS has no way to change it mid-connection, and a
// long-lived syslog connection from a router is not worth dropping over
// a renewal it will not notice.
func (r *Reloader) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cert := r.cert
	return &cert, nil
}

// Current returns the certificate in use, for callers that need the
// value rather than the hook.
func (r *Reloader) Current() tls.Certificate {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cert
}

// Reload re-reads the certificate and swaps it in, returning what it
// loaded so the caller can report on it.
//
// On failure nothing is swapped and the previous certificate stays in
// service. That is the important half: a reload triggered against a
// half-written or broken file must not take the listener down, because
// the operator sent the signal expecting an improvement and would have
// no obvious way to connect the outage to it. Serving a stale
// certificate and logging loudly is recoverable; serving none is not.
func (r *Reloader) Reload() (tls.Certificate, error) {
	cert, _, _, err := Load(r.cfg)
	if err != nil {
		return tls.Certificate{}, err
	}
	r.mu.Lock()
	r.cert = cert
	r.mu.Unlock()
	return cert, nil
}
