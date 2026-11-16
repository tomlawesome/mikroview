// SPDX-License-Identifier: AGPL-3.0-only

package notify

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/tomlawesome/mikroview/internal/flags"
)

// #1451: a webhook receiver must not be able to redirect flag data (or the
// credential in notify.webhook.headers) to a different host.

func redirectReq(t *testing.T, raw string) *http.Request {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Request{URL: u}
}

func TestRefuseCrossOriginRedirectPolicy(t *testing.T) {
	via := []*http.Request{redirectReq(t, "http://hooks.example.net/in")}
	cases := []struct {
		to      string
		refused bool
	}{
		{"http://hooks.example.net/other", false},
		{"https://hooks.example.net/in", false}, // same-host upgrade
		{"http://evil.example.org/in", true},
		{"https://evil.example.org/in", true},
		{"http://hooks.example.net:8080/in", true}, // different Host (port)
	}
	for _, c := range cases {
		err := refuseCrossOriginRedirect(redirectReq(t, c.to), via)
		if (err != nil) != c.refused {
			t.Errorf("redirect to %s: err = %v, refused should be %v", c.to, err, c.refused)
		}
	}
	// A downgrade is refused.
	if err := refuseCrossOriginRedirect(redirectReq(t, "http://hooks.example.net/in"), []*http.Request{redirectReq(t, "https://hooks.example.net/in")}); err == nil {
		t.Error("an https to http downgrade was followed")
	}
	if err := refuseCrossOriginRedirect(redirectReq(t, "http://x.example.org"), nil); err != nil {
		t.Errorf("no prior request: %v", err)
	}
	err := refuseCrossOriginRedirect(redirectReq(t, "http://evil.example.org/in"), via)
	if err == nil || !strings.Contains(err.Error(), "evil.example.org") {
		t.Errorf("refusal %v should name the destination", err)
	}
}

func TestWebhookSendDoesNotFollowARedirectToAnotherHost(t *testing.T) {
	var leaked bool
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked = true }))
	defer evil.Close()
	// Different Host value (hostname vs loopback IP) so it is cross-origin.
	target := strings.Replace(evil.URL, "127.0.0.1", "localhost", 1)
	recv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target, http.StatusTemporaryRedirect)
	}))
	defer recv.Close()

	n := NewWebhookNotifier(WebhookConfig{URL: recv.URL, Headers: map[string]string{"Authorization": "Bearer test-token"}})
	err := n.Send([]flags.Flag{{Type: flags.TypeCriticalPort, Target: "203.0.113.9", Detail: "x"}})
	if err == nil {
		t.Error("Send succeeded through a cross-origin redirect")
	}
	if leaked {
		t.Error("the redirect target was contacted")
	}
}
