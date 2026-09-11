// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/audit"
	"github.com/tomlawesome/mikroview/internal/auth"
	"github.com/tomlawesome/mikroview/internal/backupslice"
	"github.com/tomlawesome/mikroview/internal/backupvault"
)

// backupIngestServer is ingestTestServer with an empty vault and the
// slice receiver wired, which is what the endpoint needs to do anything.
func backupIngestServer(t *testing.T, device string) (*httptest.Server, *Server, string) {
	t.Helper()
	ts, s, token := ingestTestServer(t, device)
	v, err := backupvault.Open(t.TempDir(), testRetentionKey(t))
	if err != nil {
		t.Fatal(err)
	}
	s.Vault = v
	s.BackupSlices = backupslice.New(v)
	return ts, s, token
}

func postBackupSlice(t *testing.T, ts *httptest.Server, token string, body any) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/ingest/router-backup", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func decodeSliceResponse(t *testing.T, resp *http.Response) backupSliceResponse {
	t.Helper()
	defer resp.Body.Close()
	var out backupSliceResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decoding the response: %v", err)
	}
	return out
}

// realisticBackup is a plausible `.backup`: the RouterOS plain header
// and enough bytes to need several slices.
func realisticBackup(n int) []byte {
	body := append([]byte{0x88, 0xac, 0xa1, 0xb1}, bytes.Repeat([]byte("configuration"), n)...)
	return body
}

func TestBackupArrivesOverTheIngestChannelInSlices(t *testing.T) {
	ts, s, token := backupIngestServer(t, "rb5009")
	file := realisticBackup(6000) // ~78KB, so several slices
	const sliceSize = 32768
	totalSlices := (len(file) + sliceSize - 1) / sliceSize

	begin := postBackupSlice(t, ts, token, map[string]any{
		"op":          "begin",
		"kind":        backupvault.KindBackup,
		"totalBytes":  len(file),
		"totalSlices": totalSlices,
	})
	if begin.StatusCode != http.StatusOK {
		t.Fatalf("begin = %d, want 200", begin.StatusCode)
	}
	started := decodeSliceResponse(t, begin)
	if started.TransferID == "" {
		t.Fatal("begin returned no transfer id")
	}

	var last backupSliceResponse
	for i := 0; i < totalSlices; i++ {
		end := (i + 1) * sliceSize
		if end > len(file) {
			end = len(file)
		}
		resp := postBackupSlice(t, ts, token, map[string]any{
			"op":         "slice",
			"transferId": started.TransferID,
			"index":      i,
			"data":       base64.StdEncoding.EncodeToString(file[i*sliceSize : end]),
		})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("slice %d = %d, want 200", i, resp.StatusCode)
		}
		last = decodeSliceResponse(t, resp)
	}
	if !last.Done {
		t.Fatal("the last slice did not complete the transfer")
	}

	// It must land in the vault indistinguishably from an SFTP arrival.
	gens := s.Vault.Generations("rb5009")
	if len(gens) != 1 {
		t.Fatalf("the vault holds %d generations, want 1", len(gens))
	}
	got, err := s.Vault.Open("rb5009", gens[0].ID, backupvault.KindBackup)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(got, file) {
		t.Fatal("the reassembled backup does not match what was sent")
	}

	// And the audit trail says what arrived. The push script sends
	// neither kind nor size on a slice, so this line used to read
	// "kind= bytes=0" for every backup that ever completed (#1122).
	detail, ok := auditDetail(s, "ingest.router_backup")
	if !ok {
		t.Fatalf("no ingest.router_backup audit entry, got: %+v", s.Audit.Query(audit.Query{}).Entries)
	}
	want := fmt.Sprintf("kind=%s bytes=%d over the ingest channel", backupvault.KindBackup, len(file))
	if detail != want {
		t.Fatalf("the completion audit entry reads %q, want %q", detail, want)
	}
}

// auditDetail returns the detail of the newest entry with this action.
func auditDetail(s *Server, action string) (string, bool) {
	entries := s.Audit.Query(audit.Query{}).Entries
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Action == action {
			return entries[i].Detail, true
		}
	}
	return "", false
}

func TestBackupSliceNeedsAnIngestToken(t *testing.T) {
	ts, _, _ := backupIngestServer(t, "rb5009")
	resp := postBackupSlice(t, ts, "", map[string]any{"op": "begin", "kind": backupvault.KindBackup, "totalBytes": 100, "totalSlices": 1})
	resp.Body.Close()
	// 401 or 403, matching what the router-state push already returns
	// for a missing token: requireAuth refuses before the ingest mux is
	// reached at all.
	if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
		t.Fatalf("an unauthenticated push = %d, want 401 or 403", resp.StatusCode)
	}
}

// TestOneDeviceCannotContinueAnothersTransfer is the reason the transfer
// id is server-assigned and scoped to the device that began it: a token
// is readable by anyone with `read` on the router it came from (#186),
// so a second router's token must not be able to feed bytes into the
// first router's backup.
func TestOneDeviceCannotContinueAnothersTransfer(t *testing.T) {
	ts, s, token := backupIngestServer(t, "rb5009")

	admin, ok := s.Auth.ByUsername("admin")
	if !ok {
		t.Fatal("the admin account was not created")
	}
	otherToken, _, err := s.Tokens.Create("router-2", auth.TokenKindIngest, "hex-s", admin, time.Now())
	if err != nil {
		t.Fatalf("Tokens.Create: %v", err)
	}

	begin := postBackupSlice(t, ts, token, map[string]any{
		"op": "begin", "kind": backupvault.KindRsc, "totalBytes": 64, "totalSlices": 1,
	})
	started := decodeSliceResponse(t, begin)

	resp := postBackupSlice(t, ts, otherToken, map[string]any{
		"op": "slice", "transferId": started.TransferID, "index": 0,
		"data": base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("x"), 64)),
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("another device continuing the transfer = %d, want 404", resp.StatusCode)
	}
	if len(s.Vault.Routers()) != 0 {
		t.Fatal("a hijacked transfer reached the vault")
	}
}

func TestBackupSliceRefusalsAreReportedAsBadRequests(t *testing.T) {
	ts, s, token := backupIngestServer(t, "rb5009")

	cases := []struct {
		name string
		body map[string]any
		want int
	}{
		{"an unknown op", map[string]any{"op": "nonsense"}, http.StatusBadRequest},
		{"a file over the vault's cap", map[string]any{
			"op": "begin", "kind": backupvault.KindBackup,
			"totalBytes": backupvault.MaxFileBytes + 1, "totalSlices": 600,
		}, http.StatusBadRequest},
		{"an unknown kind", map[string]any{
			"op": "begin", "kind": "config", "totalBytes": 100, "totalSlices": 1,
		}, http.StatusBadRequest},
		{"a slice for an unknown transfer", map[string]any{
			"op": "slice", "transferId": "0123456789abcdef", "index": 0,
			"data": base64.StdEncoding.EncodeToString([]byte("x")),
		}, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := postBackupSlice(t, ts, token, tc.body)
			resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Fatalf("%s = %d, want %d", tc.name, resp.StatusCode, tc.want)
			}
		})
	}
	if len(s.Vault.Routers()) != 0 {
		t.Fatal("a refused transfer left something in the vault")
	}
}

func TestSlicesOutOfOrderAbortTheTransfer(t *testing.T) {
	ts, s, token := backupIngestServer(t, "rb5009")
	file := realisticBackup(4000)
	const sliceSize = 32768
	totalSlices := (len(file) + sliceSize - 1) / sliceSize

	begin := postBackupSlice(t, ts, token, map[string]any{
		"op": "begin", "kind": backupvault.KindBackup,
		"totalBytes": len(file), "totalSlices": totalSlices,
	})
	started := decodeSliceResponse(t, begin)

	// Skip slice 0 and offer slice 1. The router script aborts on any
	// refusal and starts over next run, so nothing is kept.
	resp := postBackupSlice(t, ts, token, map[string]any{
		"op": "slice", "transferId": started.TransferID, "index": 1,
		"data": base64.StdEncoding.EncodeToString(file[:sliceSize]),
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("an out-of-order slice = %d, want 400", resp.StatusCode)
	}
	if len(s.Vault.Routers()) != 0 {
		t.Fatal("an aborted transfer reached the vault")
	}
}

func TestBackupIngestRefusedWithNoVault(t *testing.T) {
	ts, s, token := backupIngestServer(t, "rb5009")
	s.Vault = nil
	s.BackupSlices = nil
	resp := postBackupSlice(t, ts, token, map[string]any{
		"op": "begin", "kind": backupvault.KindBackup, "totalBytes": 100, "totalSlices": 1,
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("a push with no vault = %d, want 503", resp.StatusCode)
	}
}

// failingBackupSink is a vault that cannot write -- a full disk, the
// case #1122 found, whose error names mikroview's own filesystem.
type failingBackupSink struct{ err error }

func (f failingBackupSink) Store(device, kind string, data []byte, now time.Time) error {
	return f.err
}

// TestAVaultFailureIsAServerFaultNotTheDevicesFault: the whole file
// arrived and verified, so the router did nothing wrong. It used to be
// answered with the vault's own error text -- absolute paths included --
// as a 400, and audited as that device's refusal.
func TestAVaultFailureIsAServerFaultNotTheDevicesFault(t *testing.T) {
	ts, s, token := backupIngestServer(t, "rb5009")
	const diskFull = "backupvault: writing /var/lib/mikroview/router-backups/9f2a/3.backup.enc: no space left on device"
	s.BackupSlices = backupslice.New(failingBackupSink{err: errors.New(diskFull)})

	file := realisticBackup(10) // one slice is enough
	begin := postBackupSlice(t, ts, token, map[string]any{
		"op": "begin", "kind": backupvault.KindBackup,
		"totalBytes": len(file), "totalSlices": 1,
	})
	if begin.StatusCode != http.StatusOK {
		t.Fatalf("begin = %d, want 200", begin.StatusCode)
	}
	started := decodeSliceResponse(t, begin)

	resp := postBackupSlice(t, ts, token, map[string]any{
		"op": "slice", "transferId": started.TransferID, "index": 0,
		"data": base64.StdEncoding.EncodeToString(file),
	})
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("a vault failure = %d, want 503", resp.StatusCode)
	}
	if strings.TrimSpace(string(body)) != backupSinkFailedMessage {
		t.Fatalf("the reply reads %q, want the fixed %q", strings.TrimSpace(string(body)), backupSinkFailedMessage)
	}
	if strings.Contains(string(body), "/var/lib") || strings.Contains(string(body), "no space") {
		t.Fatalf("the reply echoed the vault's own error to the router: %q", body)
	}

	entries := s.Audit.Query(audit.Query{}).Entries
	var found bool
	for _, e := range entries {
		if e.Action == "ingest.router_backup.refused" {
			t.Fatalf("a server fault was audited as the device's refusal: %+v", e)
		}
		if e.Action != "ingest.router_backup.failed" {
			continue
		}
		found = true
		if e.Actor != auditActorServer {
			t.Errorf("the entry's actor is %q, want %q -- a full disk is not the router's act", e.Actor, auditActorServer)
		}
		if e.Target != "rb5009" {
			t.Errorf("the entry's target is %q, want the device whose backup was lost", e.Target)
		}
		if strings.Contains(e.Detail, "/var/lib") {
			t.Errorf("the audit detail carries the vault's path: %q", e.Detail)
		}
	}
	if !found {
		t.Fatalf("no ingest.router_backup.failed audit entry, got: %+v", entries)
	}
}

// TestABackupOverAHundredAndTwentySlicesLands is #1123: every slice used
// to spend one of the device's 120 ingest tokens per 15 minutes, so a
// file needing more than that -- about 3.8MB -- was refused around slice
// 118 and could never be delivered at all.
func TestABackupOverAHundredAndTwentySlicesLands(t *testing.T) {
	ts, s, token := backupIngestServer(t, "rb5009")
	const sliceSize = 32768
	// 121 slices: one more than the whole per-window allowance.
	file := realisticBackup(121*sliceSize/13 + 1)
	totalSlices := (len(file) + sliceSize - 1) / sliceSize
	if totalSlices <= ingestLimiterThreshold {
		t.Fatalf("the test file needs %d slices, which is not over the %d-request allowance", totalSlices, ingestLimiterThreshold)
	}

	begin := postBackupSlice(t, ts, token, map[string]any{
		"op": "begin", "kind": backupvault.KindBackup,
		"totalBytes": len(file), "totalSlices": totalSlices,
	})
	if begin.StatusCode != http.StatusOK {
		t.Fatalf("begin = %d, want 200", begin.StatusCode)
	}
	started := decodeSliceResponse(t, begin)

	var last backupSliceResponse
	for i := 0; i < totalSlices; i++ {
		end := (i + 1) * sliceSize
		if end > len(file) {
			end = len(file)
		}
		resp := postBackupSlice(t, ts, token, map[string]any{
			"op": "slice", "transferId": started.TransferID, "index": i,
			"data": base64.StdEncoding.EncodeToString(file[i*sliceSize : end]),
		})
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			t.Fatalf("slice %d of %d = %d (%s), want 200", i, totalSlices, resp.StatusCode, strings.TrimSpace(string(body)))
		}
		last = decodeSliceResponse(t, resp)
	}
	if !last.Done {
		t.Fatal("the last slice did not complete the transfer")
	}

	gens := s.Vault.Generations("rb5009")
	if len(gens) != 1 {
		t.Fatalf("the vault holds %d generations, want 1", len(gens))
	}
	got, err := s.Vault.Open("rb5009", gens[0].ID, backupvault.KindBackup)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(got, file) {
		t.Fatal("the reassembled backup does not match what was sent")
	}
}

// TestASpentIngestAllowanceRefusesTheTransferNotTheSlice checks what
// #1123 left standing: the limit is charged once per transfer, so it is
// a begin that is refused, with a fixed message that tells the router
// nothing about mikroview's occupancy (#1122).
func TestASpentIngestAllowanceRefusesTheTransferNotTheSlice(t *testing.T) {
	ts, _, token := backupIngestServer(t, "rb5009")
	body := map[string]any{"op": "begin", "kind": backupvault.KindRsc, "totalBytes": 64, "totalSlices": 1}

	for i := 0; i < ingestLimiterThreshold; i++ {
		resp := postBackupSlice(t, ts, token, body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("begin %d of %d = %d, want 200", i+1, ingestLimiterThreshold, resp.StatusCode)
		}
	}

	resp := postBackupSlice(t, ts, token, body)
	got, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("the begin past the allowance = %d, want 429", resp.StatusCode)
	}
	if strings.TrimSpace(string(got)) != backupBusyMessage {
		t.Fatalf("the reply reads %q, want the fixed %q", strings.TrimSpace(string(got)), backupBusyMessage)
	}
}
