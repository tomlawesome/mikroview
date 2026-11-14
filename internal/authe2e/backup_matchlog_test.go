// SPDX-License-Identifier: AGPL-3.0-only

package authe2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/retention"
	"github.com/tomlawesome/mikroview/internal/store"
)

// #1467: -backup of a stopped, keyed install that has a match log
// succeeds, and -restore brings the log back.
//
// The harness's listener takes no syslog (syslogTls is empty), so there is
// no practical way to make a running server record a match. The log is
// written with internal/matchlog's own file store, which is the writer the
// server uses, opened with the install's key (#1470: a keyed server seals
// the log), so the file is in exactly the format a stopped server leaves.

const matchLogEntryID = "entry-1467"

func writeMatchLog(t *testing.T, a *app, entries int) {
	t.Helper()
	key, err := retention.LoadKey(a.key)
	must(t, err)
	l, err := matchlog.Open(a.file("matchlog.jsonl"), 100, key)
	must(t, err)
	defer l.Close()
	for i := 0; i < entries; i++ {
		tup := matchlog.Tuple{Source: matchlog.Identity{MAC: "52:55:0a:00:02:02"}, DestIP: "203.0.113.7", Port: 443 + i}
		ev := store.Event{ID: uint64(i + 1), SourceIP: "10.0.2.2", Chain: "forward"}
		must(t, l.Append(matchLogEntryID, tup, ev, time.Now().UTC()))
	}
}

func TestBackupOfAStoppedInstallWithMatchLogEntriesRestoresThem(t *testing.T) {
	ri := newRichInstall(t)
	writeMatchLog(t, ri.w.a, 2)

	out := filepath.Join(ri.w.a.root, "out.backup")
	code, text, _ := cli(t, Opts{Root: ri.w.a.root}, "", "-backup", out, "--force")
	if code != 0 {
		t.Fatalf("-backup of a stopped install with match log entries exit %d:\n%s", code, text)
	}

	o := Opts{KeyFile: ri.w.a.key}
	code, text, fresh := cli(t, o, "", "-restore", out, "--force")
	if code != 0 {
		t.Fatalf("-restore exit %d:\n%s", code, text)
	}
	o.Root = fresh.root
	a, err := start(t, o)
	if err != nil {
		t.Fatalf("start on the restored install: %v", err)
	}
	w := &world{a: a, admin: ri.w.admin}
	c, r := w.signIn(t, ri.w.admin, ri.w.admin.recovery(t))
	wantStatus(t, "sign-in on the restored install", r, 200)
	m := c.get("/api/matches?entries=all")
	if m.Status != 200 || strings.Count(string(m.Body), matchLogEntryID) < 2 {
		t.Errorf("the restored install lost its match log entries: %s", m)
	}
}

func TestBackupOfAStoppedInstallWithAnEmptyMatchLogSucceeds(t *testing.T) {
	ri := newRichInstall(t)
	// What a server leaves after a start and stop with no matches.
	if fi, err := os.Stat(ri.w.a.file("matchlog.jsonl")); err != nil || fi.Size() != 0 {
		writeMatchLog(t, ri.w.a, 0)
	}

	out := filepath.Join(ri.w.a.root, "out.backup")
	code, text, _ := cli(t, Opts{Root: ri.w.a.root}, "", "-backup", out, "--force")
	if code != 0 {
		t.Fatalf("-backup of a stopped install with an empty match log exit %d:\n%s", code, text)
	}
}
