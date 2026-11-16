// SPDX-License-Identifier: AGPL-3.0-only

package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// #1451: paths no other test reached.

func newHandshakeTestWriter(buf *bytes.Buffer) *handshakeWriter {
	return &handshakeWriter{
		logger: slog.New(slog.NewTextHandler(buf, nil)),
		gate:   newKeyedLimiter(handshakeLogWindow),
	}
}

func TestHandshakeWriterExplainsAndCollapsesRepeats(t *testing.T) {
	var buf bytes.Buffer
	w := newHandshakeTestWriter(&buf)
	line := "http: TLS handshake error from 198.51.100.7:61400: remote error: tls: unknown certificate\n"
	n, err := w.Write([]byte(line))
	if err != nil || n != len(line) {
		t.Fatalf("Write = %d, %v; want %d, nil", n, err, len(line))
	}
	first := buf.String()
	if !strings.Contains(first, "198.51.100.7 refused our certificate") || !strings.Contains(first, "Detail: remote error: tls: unknown certificate") {
		t.Errorf("first line = %q, want a plain-language explanation with the raw detail", first)
	}
	// The same peer and cause again, from a different source port, is quiet.
	buf.Reset()
	again := "http: TLS handshake error from 198.51.100.7:61401: remote error: tls: unknown certificate\n"
	if n, err := w.Write([]byte(again)); err != nil || n != len(again) {
		t.Fatalf("repeat Write = %d, %v", n, err)
	}
	if buf.Len() != 0 {
		t.Errorf("a repeat inside the window was logged again: %q", buf.String())
	}
	// A different peer is not hidden by it.
	other := "http: TLS handshake error from 198.51.100.8:1: remote error: tls: unknown certificate\n"
	if _, err := w.Write([]byte(other)); err != nil || !strings.Contains(buf.String(), "198.51.100.8") {
		t.Errorf("a different peer was swallowed: %q, %v", buf.String(), err)
	}
}

func TestHandshakeWriterPassesOtherServerErrorsThroughWhole(t *testing.T) {
	var buf bytes.Buffer
	w := newHandshakeTestWriter(&buf)
	msg := "http: panic serving 192.0.2.1:5555: boom\n"
	n, err := w.Write([]byte(msg))
	if err != nil || n != len(msg) {
		t.Fatalf("Write = %d, %v", n, err)
	}
	if !strings.Contains(buf.String(), "panic serving 192.0.2.1:5555: boom") || !strings.Contains(buf.String(), "level=WARN") {
		t.Errorf("logged %q, want the whole line as a warning", buf.String())
	}
}

func TestHandlerWithGroupReturnsTheSameHandler(t *testing.T) {
	h := &handler{}
	if got := h.WithGroup("g"); got != slog.Handler(h) {
		t.Error("WithGroup returned a different handler; mikroview never groups attrs")
	}
}
