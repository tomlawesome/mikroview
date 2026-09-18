// SPDX-License-Identifier: AGPL-3.0-only

package api

// Reading one stored export, and comparing two (#895). Both answer off
// the redacted copy backupvault.Store wrote -- the readable half of a
// generation never carried a secret, so neither of these has anything
// to hide at display time.
//
// They are held to exactly what handleRouterBackupDownload is held to,
// and for the same reason: a configuration read on screen is a
// configuration read. Admin only, the vault passphrase gate ahead of
// the read, and an audit entry naming who read which generation of
// whose router. Reading it in a browser instead of saving it to disk
// does not make it a lesser act.

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/tomlawesome/mikroview/internal/backupvault"
	"github.com/tomlawesome/mikroview/internal/routeros/export"
)

// routerBackupTextResponse is one generation's readable export.
// Redacted reports whether mikroview's ingest pass had to take anything
// out, so the screen can say so without re-reading the marker comment
// out of the text.
type routerBackupTextResponse struct {
	Device     string `json:"device"`
	Generation string `json:"generation"`
	Text       string `json:"text"`
	Lines      int    `json:"lines"`
	Redacted   bool   `json:"redacted"`
}

// routerBackupDiffResponse is the comparison of two generations, oldest
// named by `from`. Lines carries only what the two do not share.
type routerBackupDiffResponse struct {
	Device string            `json:"device"`
	From   string            `json:"from"`
	To     string            `json:"to"`
	Lines  []export.DiffLine `json:"lines"`
	Same   bool              `json:"same"`
}

// handleRouterBackupText returns one generation's redacted export as
// plain text for the screen to show.
func (s *Server) handleRouterBackupText(w http.ResponseWriter, r *http.Request) {
	device := r.PathValue("device")
	generation := r.PathValue("generation")
	text, ok := s.readBackupText(w, r, device, generation)
	if !ok {
		return
	}
	s.Audit.Record(auditActor(r), "router_backup.read", device,
		fmt.Sprintf("generation=%s kind=text", generation))
	writeJSON(w, http.StatusOK, routerBackupTextResponse{
		Device:     device,
		Generation: generation,
		Text:       text,
		Lines:      countLines(text),
		Redacted:   wasRedacted(text),
	})
}

// handleRouterBackupDiff compares two of a router's generations. from
// and to are query parameters rather than path segments because
// neither is more "the" generation than the other -- the pair is the
// subject, and a path would have to pick one to own the other.
func (s *Server) handleRouterBackupDiff(w http.ResponseWriter, r *http.Request) {
	// The role gate comes before the request is even read: a caller who
	// may not be here is told so, rather than being told their query
	// string was wrong -- which would answer a question they were never
	// entitled to ask.
	if !callerIsAdmin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	device := r.PathValue("device")
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	if from == "" || to == "" {
		http.Error(w, "from and to must both name a generation", http.StatusBadRequest)
		return
	}

	fromText, ok := s.readBackupText(w, r, device, from)
	if !ok {
		return
	}
	toText, ok := s.readBackupText(w, r, device, to)
	if !ok {
		return
	}

	// One entry for the pair, not one per half: the act being recorded
	// is "this admin read these two configurations", and splitting it
	// in two would read in the trail as two unrelated reads.
	s.Audit.Record(auditActor(r), "router_backup.diff", device,
		fmt.Sprintf("from=%s to=%s", from, to))

	lines := export.Diff(fromText, toText)
	writeJSON(w, http.StatusOK, routerBackupDiffResponse{
		Device: device,
		From:   from,
		To:     to,
		Lines:  lines,
		Same:   len(lines) == 0,
	})
}

// readBackupText is the gate both handlers share: admin, then the vault
// passphrase, then the read itself. It writes the refusal and reports
// false, so a caller's only job is to stop.
func (s *Server) readBackupText(w http.ResponseWriter, r *http.Request, device, generation string) (string, bool) {
	if !callerIsAdmin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return "", false
	}
	// Same gate, same words as handleRouterBackupDownload: with a
	// passphrase set, only the session that unlocked may read, and the
	// refusal says whose unlock it is not rather than contradicting
	// what the list just showed (#1124).
	if !s.vaultUnlockedFor(r, time.Now()) {
		msg := "the vault is locked -- unlock it with the vault passphrase first"
		if s.vaultUnlock.holder() != "" {
			msg = "another session holds the vault unlock -- unlock it in this session to read"
		}
		http.Error(w, msg, http.StatusForbidden)
		return "", false
	}
	data, err := s.Vault.Open(device, generation, backupvault.KindRsc)
	switch {
	case err == nil:
		return string(data), true
	case errors.Is(err, backupvault.ErrNotFound), errors.Is(err, backupvault.ErrDisabled):
		// No such generation, or no `.rsc` half of one that exists --
		// the download route answers the same way, and for the same
		// reason: which of the two it is is not the caller's business.
		http.Error(w, "not found", http.StatusNotFound)
	default:
		// A seal that will not open names this server's own files.
		// That belongs in the log, not in a reply.
		apiLog.Error(fmt.Sprintf("could not open %s's export %s: %v", device, generation, err))
		http.Error(w, "the stored export could not be opened", http.StatusInternalServerError)
	}
	return "", false
}

// countLines counts what a reader would count: a file ending in a
// newline has not got an extra empty line at the bottom.
func countLines(text string) int {
	if text == "" {
		return 0
	}
	n := 1
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			n++
		}
	}
	if text[len(text)-1] == '\n' {
		n--
	}
	return n
}

// wasRedacted reads the marker export.Redact prepends, which is on the
// first line or not at all.
func wasRedacted(text string) bool {
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			return isRedactionMarker(text[:i])
		}
	}
	return isRedactionMarker(text)
}

func isRedactionMarker(line string) bool {
	const prefix = "# mikroview: "
	return len(line) > len(prefix) && line[:len(prefix)] == prefix
}
