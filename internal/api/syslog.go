// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"fmt"
	"net/http"

	"github.com/tomlawesome/mikroview/internal/syslog"
)

// handleSyslogLossClear is issue #1015's "Clear all" for the
// ingest-loss family: it zeroes the four monotonic counters
// GET /api/stats' "syslog.loss" field is built from (syslog.ClearLoss),
// so a transient loss the operator has already seen stops permanently
// marking the instance. Same access tier and CSRF requirement as
// POST /api/flags/clear-all (see handleFlagsClearAll) -- both are a
// reversible, whole-family clear of something mikroview is currently
// showing, which #653 put at user tier rather than viewer or admin.
//
// One audit entry per call, carrying the totals that were cleared, so
// "what was cleared and when" stays answerable the same way
// flag.clear_all's "cleared N flags" detail does.
func (s *Server) handleSyslogLossClear(w http.ResponseWriter, r *http.Request) {
	if !callerIsUser(r) {
		http.Error(w, "user role required", http.StatusForbidden)
		return
	}
	cleared := syslog.ClearLoss()
	s.Audit.Record(auditActor(r), "ingest_loss.clear_all", "", fmt.Sprintf(
		"dropped=%d rejectedConfigured=%d rejected=%d oversized=%d",
		cleared.Dropped, cleared.RejectedConfigured, cleared.Rejected, cleared.Oversized))
	writeJSON(w, http.StatusOK, map[string]any{
		"dropped":            cleared.Dropped,
		"rejectedConfigured": cleared.RejectedConfigured,
		"rejected":           cleared.Rejected,
		"oversized":          cleared.Oversized,
	})
}
