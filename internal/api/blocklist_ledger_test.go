// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/routerstate"
	"github.com/tomlawesome/mikroview/internal/setup"
)

// "Fired N today" counts from the first push after local midnight, and a
// counter that falls (a reboot) restarts the count rather than going
// negative or losing what came before.
func TestFiredSinceAcrossMidnightAndAReboot(t *testing.T) {
	midnight := time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local)
	at := func(h, m int) time.Time {
		return midnight.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute)
	}
	sample := func(t time.Time, p int64) routerstate.RawRuleSample {
		return routerstate.RawRuleSample{At: t, Packets: p}
	}

	for _, tc := range []struct {
		name    string
		samples []routerstate.RawRuleSample
		want    int64
		wantOK  bool
	}{
		{"nothing pushed yet", nil, 0, false},
		{"only yesterday's samples", []routerstate.RawRuleSample{sample(at(-2, 0), 50), sample(at(-1, 0), 90)}, 0, false},
		{"across midnight, yesterday's count is not today's",
			[]routerstate.RawRuleSample{sample(at(-1, 40), 900), sample(at(0, 0), 1000), sample(at(0, 20), 1030), sample(at(9, 0), 1412)}, 412, true},
		{"one sample today has fired nothing yet", []routerstate.RawRuleSample{sample(at(3, 0), 77)}, 0, true},
		{"a reboot restarts the counter",
			[]routerstate.RawRuleSample{sample(at(1, 0), 100), sample(at(2, 0), 160), sample(at(3, 0), 5), sample(at(4, 0), 25)}, 60 + 5 + 20, true},
	} {
		got, ok := firedSince(tc.samples, midnight)
		if got != tc.want || ok != tc.wantOK {
			t.Errorf("%s: %d, %v; want %d, %v", tc.name, got, ok, tc.want, tc.wantOK)
		}
	}
	if m := localMidnight(time.Date(2026, 10, 1, 13, 5, 0, 0, time.Local)); !m.Equal(midnight) {
		t.Errorf("localMidnight = %v", m)
	}
}

func TestFlagsCountedPerFeedLabel(t *testing.T) {
	now := time.Now()
	all := []flags.Flag{
		{Type: flags.TypeKnownBadIP, Detail: "matches Spamhaus DROP (1.10.16.0/20)", LastSeen: now.Add(-time.Hour)},
		{Type: flags.TypeKnownBadIP, Detail: "matches Spamhaus DROP (1.19.0.0/16)", LastSeen: now.Add(-25 * time.Hour)},
		{Type: flags.TypeKnownBadIP, Detail: "matches Emerging Threats compromised IPs (198.51.100.7/32)", LastSeen: now},
		{Type: flags.TypePortScan, Detail: "matches Spamhaus DROP (fake)", LastSeen: now},
	}
	if n := countFeedFlags(all, "Spamhaus DROP", now.Add(-24*time.Hour)); n != 1 {
		t.Errorf("Spamhaus DROP flags in 24 h = %d, want 1", n)
	}
}

// The ledger: fired today from the raw rules' counters, both directions
// summed; flags only for the lists MikroView flags from; and a list the
// router stops holding goes back to off.
func TestBlocklistLedgerFollowsThePushes(t *testing.T) {
	s, ts, admin := blocklistTestServer(t, "7.24.4")
	raw := func(from, to int) string {
		return `{"kind":"raw-rule","page":1,"pages":1,"records":[` +
			`{"ordinal":0,"family":"ip","comment":"mikroview blocklist: et (from)","chain":"prerouting","action":"drop","srcAddressList":"mv-bl-et","dstAddressList":null,"logPrefix":"D|bl-et|","log":true,"disabled":false,"packets":` + strconv.Itoa(from) + `,"bytes":0},` +
			`{"ordinal":1,"family":"ip","comment":"mikroview blocklist: et (to)","chain":"prerouting","action":"drop","srcAddressList":null,"dstAddressList":"mv-bl-et","logPrefix":"D|bl-et|","log":true,"disabled":false,"packets":` + strconv.Itoa(to) + `,"bytes":0}]}`
	}
	held := `{"kind":"address-list-count","page":1,"pages":1,"records":[{"list":"mv-bl-et","family":"ip","count":633,"loadedAt":"2026-10-01 04:31:09"}]}`
	pushBlocklistPage(t, s, "rb5009", held)
	pushBlocklistPage(t, s, "rb5009", raw(10, 1))
	pushBlocklistPage(t, s, "rb5009", raw(13, 4))

	_, got := getBuilder(t, admin, ts.URL+"/api/blocklist/builder?device=rb5009")
	et := got.Lists[1]
	if et.State != "held" || et.Count != 633 || et.FiredToday == nil || *et.FiredToday != 6 {
		t.Fatalf("ET row %+v (fired %v)", et, et.FiredToday)
	}
	if et.Flags24h == nil || got.Lists[2].Flags24h != nil {
		t.Errorf("flags: ET %v, CINS %v -- only lists MikroView flags from carry a count", et.Flags24h, got.Lists[2].Flags24h)
	}
	if got.Lists[2].FiredToday != nil {
		t.Error("CINS has no rules on the router but reports a fired count")
	}

	pushBlocklistPage(t, s, "rb5009", `{"kind":"address-list-count","page":1,"pages":1,"records":[{"list":"mv-bl-et","family":"ip","count":0,"loadedAt":""}]}`)
	_, got = getBuilder(t, admin, ts.URL+"/api/blocklist/builder?device=rb5009")
	if got.Lists[1].State != "off" {
		t.Errorf("after the router emptied mv-bl-et the row reads %q, want off", got.Lists[1].State)
	}
}

// Record 8 is witnessed once, by the first push reporting a blocklist
// list with entries, through the real ingest endpoint; it survives a
// restart; and a push with nothing loaded witnesses nothing.
func TestBlocklistWitnessIsRecordEight(t *testing.T) {
	ts, s, token := ingestTestServer(t, "rb5009")
	path := filepath.Join(t.TempDir(), "setup.json")
	ledger, err := setup.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Setup = ledger

	push := func(body string) {
		t.Helper()
		resp := postIngest(t, ts, token, body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("push: %d", resp.StatusCode)
		}
	}
	witnessed := func(st *setup.Store) (setup.Mark, bool) {
		for _, m := range st.Witnessed() {
			if m.Step == setup.StepBlocklist {
				return m, true
			}
		}
		return setup.Mark{}, false
	}

	push(`{"kind":"address-list-count","page":1,"pages":1,"wizardVersion":6,"records":[{"list":"mv-bl-et","family":"ip","count":0,"loadedAt":""}]}`)
	if _, ok := witnessed(ledger); ok {
		t.Fatal("a push with every list empty witnessed record 8")
	}
	push(`{"kind":"address-list-count","page":1,"pages":1,"wizardVersion":6,"records":[` +
		`{"list":"mv-bl-spamhaus","family":"ip","count":1692,"loadedAt":"2026-10-01 14:07:00"},` +
		`{"list":"mv-bl-et","family":"ip","count":633,"loadedAt":"2026-10-01 14:07:30"}]}`)
	m, ok := witnessed(ledger)
	if !ok || m.Note != "Spamhaus DROP 1,692 held · Emerging Threats 633 held · on rb5009" {
		t.Fatalf("record 8 = %+v, %v", m, ok)
	}
	push(`{"kind":"address-list-count","page":1,"pages":1,"wizardVersion":6,"records":[{"list":"mv-bl-et","family":"ip","count":700,"loadedAt":"x"}]}`)
	if again, _ := witnessed(ledger); again != m {
		t.Errorf("a later push rewrote the witness: %+v", again)
	}
	reopened, err := setup.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if again, ok := witnessed(reopened); !ok || again.Note != m.Note {
		t.Errorf("after a restart record 8 is %+v, %v", again, ok)
	}
	// The tail's "Not now" is an ordinary mark under the same number.
	if _, ok, err := reopened.NoteMark(setup.StepBlocklist, setup.MarkSkipped, "admin", "", time.Now()); !ok || err != nil {
		t.Errorf("NoteMark(8, skipped) = %v, %v", ok, err)
	}
}
