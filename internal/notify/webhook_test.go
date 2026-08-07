// SPDX-License-Identifier: AGPL-3.0-only

package notify

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tomlawesome/mikroview/internal/flags"
)

func TestWebhookNotifierSendsJSONPayload(t *testing.T) {
	var gotBody webhookPayload
	var gotContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	n := NewWebhookNotifier(WebhookConfig{URL: server.URL})

	confidence := 42
	batch := []flags.Flag{
		{Type: flags.TypeCriticalPort, Target: "203.0.113.9", Detail: "5 attempts against port 22", Confidence: &confidence},
	}
	if err := n.Send(batch); err != nil {
		t.Fatalf("Send returned an error: %v", err)
	}

	if gotContentType != "application/json" {
		t.Errorf("expected application/json content-type, got %q", gotContentType)
	}
	if gotBody.Count != 1 {
		t.Errorf("expected count 1, got %d", gotBody.Count)
	}
	if len(gotBody.Flags) != 1 || gotBody.Flags[0].Target != "203.0.113.9" {
		t.Errorf("expected the batch's flag to be included, got %+v", gotBody.Flags)
	}
	if gotBody.Flags[0].Confidence == nil || *gotBody.Flags[0].Confidence != 42 {
		t.Errorf("expected confidence 42, got %+v", gotBody.Flags[0].Confidence)
	}
}

func TestWebhookNotifierSendsCustomHeaders(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	n := NewWebhookNotifier(WebhookConfig{
		URL:     server.URL,
		Headers: map[string]string{"Authorization": "Bearer tok123"},
	})

	if err := n.Send([]flags.Flag{{Detail: "x"}}); err != nil {
		t.Fatalf("Send returned an error: %v", err)
	}
	if gotAuth != "Bearer tok123" {
		t.Errorf("expected the configured Authorization header to be sent, got %q", gotAuth)
	}
}

func TestWebhookNotifierSkipsEmptyBatch(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	n := NewWebhookNotifier(WebhookConfig{URL: server.URL})

	if err := n.Send(nil); err != nil {
		t.Fatalf("expected no error for an empty batch, got %v", err)
	}
	if called {
		t.Error("expected no HTTP request for an empty batch")
	}
}

func TestWebhookNotifierAccepts2xxStatuses(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusCreated, http.StatusAccepted, http.StatusNoContent} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		}))
		n := NewWebhookNotifier(WebhookConfig{URL: server.URL})
		if err := n.Send([]flags.Flag{{Detail: "x"}}); err != nil {
			t.Errorf("status %d: expected no error, got %v", status, err)
		}
		server.Close()
	}
}

func TestWebhookNotifierErrorsOnNonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	n := NewWebhookNotifier(WebhookConfig{URL: server.URL})

	if err := n.Send([]flags.Flag{{Detail: "x"}}); err == nil {
		t.Error("expected an error for a non-2xx response")
	}
}
