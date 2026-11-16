// SPDX-License-Identifier: AGPL-3.0-only

package retention

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// captureLogger swaps the package logger for one writing to a buffer.
func captureLogger(t *testing.T) *lockedBuf {
	t.Helper()
	buf := &lockedBuf{}
	orig := logger
	logger = slog.New(slog.NewTextHandler(buf, nil))
	t.Cleanup(func() { logger = orig })
	return buf
}

func recs() []record {
	at := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	return []record{{Event: event(1, at), ReceivedAt: at}}
}

// breakCurrent closes the open day file's descriptor behind the store's
// back, so the store's own Close of it fails.
func breakCurrent(t *testing.T, s *Store) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		t.Fatal("no current day file")
	}
	if err := s.current.log.f.Close(); err != nil {
		t.Fatalf("closing descriptor: %v", err)
	}
}

func TestRolloverLogsFailedCloseAndStillRolls(t *testing.T) {
	s := openStore(t, t.TempDir(), testKey(t))
	buf := captureLogger(t)
	if err := s.appendToDay("2026-03-01", recs()); err != nil {
		t.Fatal(err)
	}
	breakCurrent(t, s)
	if err := s.appendToDay("2026-03-02", recs()); err != nil {
		t.Fatalf("roll-over must still go ahead: %v", err)
	}
	s.mu.Lock()
	day := s.current.day
	s.mu.Unlock()
	if day != "2026-03-02" {
		t.Fatalf("current day = %q, want 2026-03-02", day)
	}
	if out := buf.String(); !strings.Contains(out, "level=ERROR") || !strings.Contains(out, `msg="closing retained day file"`) {
		t.Fatalf("failed close at roll-over was not logged; log = %q", out)
	}
}

func TestStoreCloseLogsFailedCloseOfCurrentDay(t *testing.T) {
	s := openStore(t, t.TempDir(), testKey(t))
	buf := captureLogger(t)
	if err := s.appendToDay("2026-03-01", recs()); err != nil {
		t.Fatal(err)
	}
	breakCurrent(t, s)
	s.Close()
	if out := buf.String(); !strings.Contains(out, "level=ERROR") || !strings.Contains(out, `msg="closing retained day file"`) {
		t.Fatalf("failed close at shutdown was not logged; log = %q", out)
	}
}
