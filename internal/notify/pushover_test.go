// SPDX-License-Identifier: AGPL-3.0-only

package notify

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/tomlawesome/mikroview/internal/flags"
)

func TestPushoverNotifierSendsFormEncodedRequest(t *testing.T) {
	var gotForm url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		gotForm = r.Form
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	confidence := 42
	n := NewPushoverNotifier(PushoverConfig{Token: "tok123", User: "usr456"})
	n.apiURL = server.URL

	batch := []flags.Flag{
		{Type: flags.TypeCriticalPort, Target: "203.0.113.9", Detail: "5 attempts against port 22", Confidence: &confidence},
	}
	if err := n.Send(batch); err != nil {
		t.Fatalf("Send returned an error: %v", err)
	}

	if gotForm.Get("token") != "tok123" || gotForm.Get("user") != "usr456" {
		t.Errorf("expected token/user to be sent, got %+v", gotForm)
	}
	if !strings.Contains(gotForm.Get("title"), "1 new flag") {
		t.Errorf("expected title to name the batch size, got %q", gotForm.Get("title"))
	}
	msg := gotForm.Get("message")
	if !strings.Contains(msg, "203.0.113.9") || !strings.Contains(msg, "5 attempts against port 22") || !strings.Contains(msg, "42%") {
		t.Errorf("expected the message to describe the flag, got %q", msg)
	}
}

func TestPushoverNotifierCapsLongBatches(t *testing.T) {
	var gotMessage string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		gotMessage = r.Form.Get("message")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	n := NewPushoverNotifier(PushoverConfig{Token: "t", User: "u"})
	n.apiURL = server.URL

	batch := make([]flags.Flag, pushoverMaxLines+5)
	for i := range batch {
		batch[i] = flags.Flag{Type: flags.TypePortScan, Target: "1.2.3.4", Detail: "detail"}
	}
	if err := n.Send(batch); err != nil {
		t.Fatalf("Send returned an error: %v", err)
	}

	if !strings.Contains(gotMessage, "and 5 more") {
		t.Errorf("expected a truncation trailer for a batch over pushoverMaxLines, got %q", gotMessage)
	}
	if len(gotMessage) > pushoverMaxMessageLen {
		t.Errorf("expected the message to stay within pushoverMaxMessageLen, got %d chars", len(gotMessage))
	}
}

func TestPushoverNotifierSkipsEmptyBatch(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	n := NewPushoverNotifier(PushoverConfig{Token: "t", User: "u"})
	n.apiURL = server.URL

	if err := n.Send(nil); err != nil {
		t.Fatalf("expected no error for an empty batch, got %v", err)
	}
	if called {
		t.Error("expected no HTTP request for an empty batch")
	}
}

func TestPushoverNotifierErrorsOnNonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	n := NewPushoverNotifier(PushoverConfig{Token: "bad", User: "bad"})
	n.apiURL = server.URL

	if err := n.Send([]flags.Flag{{Detail: "x"}}); err == nil {
		t.Error("expected an error for a non-200 response")
	}
}

// The message is built from a flag's Target and Detail, which come from
// a syslog line whoever sent it wrote -- so a multi-byte rune can land
// across the byte cap. A plain slice there emits invalid UTF-8.
func TestPushoverMessageTruncatesOnARuneBoundary(t *testing.T) {
	// Long enough to be cut, and built so the cut lands mid-sequence for
	// at least one of the four offsets tested.
	for pad := 0; pad < 4; pad++ {
		fs := []flags.Flag{{
			Type:   flags.TypePortScan,
			Target: strings.Repeat("a", pad) + strings.Repeat("é", pushoverMaxMessageLen),
			Detail: "detail",
		}}
		msg := (&PushoverNotifier{}).message(fs)
		if len(msg) > pushoverMaxMessageLen {
			t.Errorf("pad=%d: message is %d bytes, over the %d cap", pad, len(msg), pushoverMaxMessageLen)
		}
		if !utf8.ValidString(msg) {
			t.Errorf("pad=%d: truncation produced invalid UTF-8", pad)
		}
	}
}
