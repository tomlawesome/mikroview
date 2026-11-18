// SPDX-License-Identifier: AGPL-3.0-only

package routeros

import (
	"strings"
	"testing"
)

// #1451: builder-page helpers no other test reached.

func TestBlocklistRuleCommentPrefixMatchesTheCommentsTheBuilderWrites(t *testing.T) {
	for _, dir := range []string{"from", "to"} {
		c := blocklistRuleComment("spamhaus", dir)
		if !strings.HasPrefix(c, BlocklistRuleCommentPrefix("spamhaus")) {
			t.Errorf("comment %q does not start with the prefix %q", c, BlocklistRuleCommentPrefix("spamhaus"))
		}
	}
	// One list's prefix must not match another list whose key starts the same way.
	if strings.HasPrefix(blocklistRuleComment("et2", "from"), BlocklistRuleCommentPrefix("et")) {
		t.Error("prefix for et matched a comment for et2")
	}
}

func TestBlocklistStartTimeIsHHMMOrEmpty(t *testing.T) {
	if got := BlocklistStartTime("spamhaus"); got != "04:17" {
		t.Errorf("BlocklistStartTime(spamhaus) = %q, want 04:17", got)
	}
	if got := BlocklistStartTime("nope"); got != "" {
		t.Errorf("unknown key = %q, want empty", got)
	}
	seen := map[string]string{}
	for k := range blocklistStartTimes {
		got := BlocklistStartTime(k)
		if len(got) != 5 || got[2] != ':' {
			t.Errorf("%s start time %q is not HH:MM", k, got)
		}
		if other, dup := seen[got]; dup {
			t.Errorf("%s and %s fetch at the same time %s", k, other, got)
		}
		seen[got] = k
	}
}

func TestUpgradeAndDisableCommandsDoWhatTheirNotesSay(t *testing.T) {
	up := RouterOSUpgradeCommands()
	lines := strings.Split(up, "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "check-for-updates") || !strings.HasSuffix(lines[1], "update install") {
		t.Errorf("RouterOSUpgradeCommands = %q, want check then install", up)
	}
	dis := BlocklistDisableAll()
	if !strings.Contains(dis, "disable") || strings.Contains(dis, "remove") || !strings.Contains(dis, "mikroview blocklist") {
		t.Errorf("BlocklistDisableAll = %q, want a disable (never a remove) of the blocklist rules", dis)
	}
}

func TestResetBuiltinLoggingActionRestoresFactoryTargets(t *testing.T) {
	if got := ResetBuiltinLoggingAction("remote"); !strings.Contains(got, "name=remote") || !strings.Contains(got, "remote=0.0.0.0") || !strings.Contains(got, "src-address=0.0.0.0") {
		t.Errorf("remote reset = %q, want the remote and src-address fields cleared", got)
	}
	for _, name := range []string{"memory", "disk", "echo"} {
		want := "/system logging action set [find name=" + name + "] target=" + name
		if got := ResetBuiltinLoggingAction(name); got != want {
			t.Errorf("ResetBuiltinLoggingAction(%s) = %q, want %q", name, got, want)
		}
	}
}
