package detect

import (
	"fmt"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/rules"
)

// StaleRuleDetector periodically checks internal/rules' long-lived
// per-rule usage records for any rule that hasn't fired in longer than
// MaxAge, raising a TypeStaleRule flag per such rule (issue #102). A
// rule that hasn't fired in a long time is either dead weight or,
// worse, an unnecessary hole -- flagging it for human review closes
// attack surface at essentially no cost.
//
// Checked periodically (see main.go's staleRuleCheckInterval ticker),
// not per-event -- "hasn't fired in a while" is a property of the
// passage of time, not of any single event, so there's nothing to
// evaluate on ingest itself.
//
// Accepted trade-off (explicit product decision, not an oversight): a
// rule already removed by the operator will still surface as stale
// until the resulting flag is manually cleared. mikroview has no
// visibility into the router's actually-configured rule set (it's
// passive-syslog-only, see internal/rules' doc comment), so "hasn't
// fired in a while" can't be distinguished from "no longer exists."
// Harmless: the implied suggestion ("consider removing this rule") is a
// no-op if it's already gone, and the alternative failure mode -- a
// genuinely forgotten, still-open rule going unflagged -- is worse.
type StaleRuleDetector struct {
	ru     *rules.Store
	fs     *flags.Store
	maxAge time.Duration
}

// NewStaleRuleDetector constructs a detector that flags any rule in ru
// whose LastSeen is older than maxAge as of the time Check is called.
func NewStaleRuleDetector(ru *rules.Store, fs *flags.Store, maxAge time.Duration) *StaleRuleDetector {
	return &StaleRuleDetector{ru: ru, fs: fs, maxAge: maxAge}
}

// Check raises (or, via flags' dedup-by-(Type,Target), re-fires) a
// TypeStaleRule flag for every rule ru currently considers stale as of
// now. Re-firing on every sweep while a rule stays stale is intentional
// and harmless -- flags.Store.Add updates an already-active flag in
// place rather than creating a duplicate, so this keeps LastSeen/Count
// current on the flag without spamming a new episode each time.
func (d *StaleRuleDetector) Check(now time.Time) {
	for _, u := range d.ru.Stale(d.maxAge, now) {
		idleDays := now.Sub(u.LastSeen).Hours() / 24
		d.fs.Add(flags.TypeStaleRule, u.Rule,
			fmt.Sprintf("no traffic in %.1f days (last seen %s, first seen %s, %d hits total)",
				idleDays, u.LastSeen.Format(time.RFC3339), u.FirstSeen.Format(time.RFC3339), u.Count),
			now)
	}
}
