// SPDX-License-Identifier: AGPL-3.0-only

package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
)

// webhookTimeout bounds how long a single Send waits on the configured
// URL -- same reasoning as pushoverTimeout: this runs on Dispatcher's
// single goroutine, so a hung request must not stall every other
// configured channel's next flush indefinitely.
const webhookTimeout = 10 * time.Second

// WebhookConfig points at an arbitrary JSON-POST receiver -- ntfy,
// Discord, Slack, Home Assistant, n8n, or anything else without a
// bespoke integration in this package. Headers is deliberately a plain
// map rather than a single bearer-token field: ntfy/Home
// Assistant/n8n-style receivers each expect auth in a different header
// (Authorization: Bearer ..., a custom X-... header, etc), so this
// covers all of them rather than picking one convention.
type WebhookConfig struct {
	URL     string
	Headers map[string]string
}

// webhookPayload is the JSON body POSTed to WebhookConfig.URL -- the
// batch itself plus a couple of denormalized summary fields (title,
// count) so a generic consumer that doesn't want to inspect the array
// still has something readable to show (e.g. a Discord/Slack message
// template keyed on {{title}}).
type webhookPayload struct {
	Title string       `json:"title"`
	Count int          `json:"count"`
	Flags []flags.Flag `json:"flags"`
}

// WebhookNotifier sends a batch of newly-raised flags as one JSON POST
// to an arbitrary URL (issue #96) -- the generic alternative to the
// SMTP/Pushover notifiers for any receiver without a bespoke
// integration in this package.
type WebhookNotifier struct {
	cfg    WebhookConfig
	client *http.Client
}

func NewWebhookNotifier(cfg WebhookConfig) *WebhookNotifier {
	return &WebhookNotifier{
		cfg: cfg,
		client: &http.Client{
			Timeout:       webhookTimeout,
			CheckRedirect: refuseCrossOriginRedirect,
		},
	}
}

// refuseCrossOriginRedirect stops a redirect from carrying this
// deployment's flag data, and the credential configured to reach the
// receiver, to a host the operator never named.
//
// Go's own protection is not enough here. The stdlib strips
// Authorization and Cookie when a redirect crosses hosts, but not
// arbitrary request headers -- and WebhookConfig.Headers exists
// precisely because ntfy, Home Assistant and n8n each want their
// credential in a *different*, custom header. config.go's own
// documentation steers operators towards putting it in one. So the
// header the stdlib does not strip is exactly the header most likely to
// hold the secret: measured against live test servers, a configured
// X-Api-Key was forwarded verbatim to a cross-host redirect target.
//
// Same-origin redirects are still followed, since a receiver
// redirecting within itself (a trailing slash, http->https on its own
// host) is ordinary and harmless. Anything else fails the send with an
// error the operator sees, rather than succeeding quietly against
// somewhere else.
//
// Deliberately *not* the SSRF dial guard internal/netclass uses: a
// webhook URL is chosen by the operator and very often points at a
// private address on purpose (Home Assistant or a self-hosted ntfy on
// the LAN). Refusing private destinations would break the intended use
// rather than protect it. What is guarded is the destination changing
// out from under the operator mid-request. See #285.
func refuseCrossOriginRedirect(req *http.Request, via []*http.Request) error {
	if len(via) == 0 {
		return nil
	}
	origin := via[0].URL
	if req.URL.Host == origin.Host && req.URL.Scheme == origin.Scheme {
		return nil
	}
	if req.URL.Host == origin.Host && origin.Scheme == "http" && req.URL.Scheme == "https" {
		return nil // an upgrade on the same host is strictly better
	}
	return fmt.Errorf(
		"notify/webhook: refusing a redirect from %s://%s to %s://%s -- the configured receiver must not hand this deployment's flag data, or the credential in notify.webhook.headers, to a different host",
		origin.Scheme, origin.Host, req.URL.Scheme, req.URL.Host)
}

func (n *WebhookNotifier) Send(batch []flags.Flag) error {
	if len(batch) == 0 {
		return nil
	}

	payload := webhookPayload{
		Title: fmt.Sprintf("mikroview: %d new flag%s", len(batch), plural(len(batch))),
		Count: len(batch),
		Flags: batch,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("notify/webhook: encoding payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, n.cfg.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("notify/webhook: building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// Set custom headers after Content-Type so a configured Headers entry
	// can still override it (e.g. a receiver wanting a different
	// content-type suffix) rather than always losing to the default.
	for k, v := range n.cfg.Headers {
		req.Header.Set(k, v)
	}

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("notify/webhook: %w", err)
	}
	defer resp.Body.Close()
	// 2xx is treated as success across the board rather than requiring
	// exactly 200 (unlike PushoverNotifier's fixed API contract) -- the
	// receivers this targets (ntfy, Discord, Slack, Home Assistant, n8n,
	// ...) don't agree on one status code for "accepted" (200, 201, 202,
	// and 204 are all in use).
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("notify/webhook: unexpected status %s", resp.Status)
	}
	return nil
}
