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
	return &WebhookNotifier{cfg: cfg, client: &http.Client{Timeout: webhookTimeout}}
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
