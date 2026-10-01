// SPDX-License-Identifier: AGPL-3.0-only

package blcatalogue

import (
	"go/build"
	"slices"
	"strings"
	"testing"
)

// The seven ratified lists, in the drawn order (round 2's three, then
// the owner's 1b and 1c), each key once.
func TestCatalogueIsTheSevenRatifiedListsInOrder(t *testing.T) {
	want := []string{"spamhaus", "et", "cins", "blde", "greensnow", "dshield", "bindef"}
	var got []string
	for _, l := range Lists() {
		got = append(got, l.Key)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("catalogue keys = %v, want %v (order matters: it is the drawn order)", got, want)
	}
	for _, k := range want {
		if _, ok := Lookup(k); !ok {
			t.Errorf("Lookup(%q) found nothing", k)
		}
	}
	if _, ok := Lookup("firehol"); ok {
		t.Error("Lookup found a list the catalogue leaves out")
	}
}

// Keys become RouterOS names (mv-bl-<key>, a script, a scheduler, a rule
// comment) and are matched by regex on the router (^mv-bl-<key>), so they
// stay to lower-case letters and digits.
func TestKeysAreSafeRouterOSNames(t *testing.T) {
	for _, l := range Lists() {
		if l.Key == "" {
			t.Fatal("a list has no key")
		}
		for _, r := range l.Key {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
				t.Errorf("key %q has %q; keys are lower-case letters and digits only", l.Key, r)
			}
		}
	}
}

func TestEveryURLIsHTTPS(t *testing.T) {
	for _, l := range Lists() {
		for _, u := range []string{l.URL, l.URL6} {
			if u == "" {
				continue
			}
			if !strings.HasPrefix(u, "https://") {
				t.Errorf("%s: %q is not https -- the router would load a list anyone on the path could rewrite", l.Key, u)
			}
		}
		if l.URL == "" {
			t.Errorf("%s has no URL", l.Key)
		}
		if (l.URL6 != "") != l.IPv6 {
			t.Errorf("%s: IPv6 = %v but URL6 = %q", l.Key, l.IPv6, l.URL6)
		}
	}
}

// The three that passed the strict bar state their terms and carry no
// caveat; the four the owner admitted carry exactly the label their
// answer named (1b: no licence stated, 1c: not for business use).
func TestTermsAndCaveats(t *testing.T) {
	want := map[string]string{
		"spamhaus":  "",
		"et":        "",
		"cins":      "",
		"blde":      CaveatNoLicence,
		"greensnow": CaveatNoLicence,
		"dshield":   CaveatNotBusiness,
		"bindef":    CaveatNotBusiness,
	}
	for _, l := range Lists() {
		if l.Caveat != want[l.Key] {
			t.Errorf("%s: caveat %q, want %q", l.Key, l.Caveat, want[l.Key])
		}
		if l.Terms == "" {
			t.Errorf("%s has no terms text", l.Key)
		}
	}
	if CaveatNoLicence != "no licence stated" || CaveatNotBusiness != "not for business use" {
		t.Error("the caveat labels are not the words the owner ratified")
	}
}

// Defaults are exactly what was ratified: Spamhaus and ET on, ET both
// ways (17a), Spamhaus with IPv6, and only Spamhaus offering IPv6.
func TestDefaults(t *testing.T) {
	var on []string
	for _, l := range Lists() {
		if l.Default {
			on = append(on, l.Key)
		}
		if !slices.Contains(l.Refresh, l.RefreshDefault) {
			t.Errorf("%s: default refresh %q is not one of its choices %v", l.Key, l.RefreshDefault, l.Refresh)
		}
		wantDir := DirectionFrom
		if l.Key == "et" {
			wantDir = DirectionBoth
		}
		if l.DefaultDirection != wantDir {
			t.Errorf("%s: default direction %q, want %q", l.Key, l.DefaultDirection, wantDir)
		}
		if l.IPv6 != (l.Key == "spamhaus") {
			t.Errorf("%s: IPv6 = %v; only Spamhaus offers an IPv6 list", l.Key, l.IPv6)
		}
		if l.FlaggedByMikroView != (l.Key == "spamhaus" || l.Key == "et") {
			t.Errorf("%s: FlaggedByMikroView = %v; internal/blocklist flags from Spamhaus and ET only", l.Key, l.FlaggedByMikroView)
		}
	}
	if !slices.Equal(on, []string{"spamhaus", "et"}) {
		t.Errorf("on by default: %v, want exactly spamhaus and et", on)
	}
}

// The refresh floors: Spamhaus asks for no fetch under an hour apart and
// the card never offers under 6 h; the BUILD.md choices for the four new
// lists; weekdays only on ET.
func TestRefreshChoices(t *testing.T) {
	want := map[string][]Refresh{
		"spamhaus":  {RefreshSixHours, RefreshDaily, RefreshWeekly},
		"et":        {RefreshDaily, RefreshWeekdays, RefreshWeekly},
		"cins":      {RefreshHourly, RefreshSixHours, RefreshDaily},
		"blde":      {RefreshHourly, RefreshSixHours, RefreshDaily},
		"greensnow": {RefreshSixHours, RefreshDaily, RefreshWeekly},
		"dshield":   {RefreshSixHours, RefreshDaily, RefreshWeekly},
		"bindef":    {RefreshDaily, RefreshWeekly},
	}
	wantDefault := map[string]Refresh{
		"spamhaus": RefreshDaily, "et": RefreshWeekdays, "cins": RefreshSixHours,
		"blde": RefreshSixHours, "greensnow": RefreshDaily, "dshield": RefreshDaily, "bindef": RefreshDaily,
	}
	for _, l := range Lists() {
		if !slices.Equal(l.Refresh, want[l.Key]) {
			t.Errorf("%s: refresh choices %v, want %v", l.Key, l.Refresh, want[l.Key])
		}
		if l.RefreshDefault != wantDefault[l.Key] {
			t.Errorf("%s: refresh default %q, want %q", l.Key, l.RefreshDefault, wantDefault[l.Key])
		}
	}
}

// Without the scheduler's days= (below RouterOS 7.24) weekdays is
// neither offered nor the default: the drawn 7.19.4 card opens ET on
// daily with daily and weekly to choose from.
func TestWeekdaysNeedsSchedulerDays(t *testing.T) {
	et, _ := Lookup("et")
	if got := et.RefreshDefaultFor(true); got != RefreshWeekdays {
		t.Errorf("ET's default with days= = %q, want weekdays", got)
	}
	if got := et.RefreshDefaultFor(false); got != RefreshDaily {
		t.Errorf("ET's default without days= = %q, want daily", got)
	}
	if got := et.RefreshChoicesFor(false); !slices.Equal(got, []Refresh{RefreshDaily, RefreshWeekly}) {
		t.Errorf("ET's choices without days= = %v, want daily, weekly", got)
	}
	if got := et.RefreshChoicesFor(true); !slices.Equal(got, et.Refresh) {
		t.Errorf("ET's choices with days= = %v, want all of %v", got, et.Refresh)
	}
	spamhaus, _ := Lookup("spamhaus")
	if spamhaus.RefreshDefaultFor(false) != spamhaus.RefreshDefault {
		t.Error("a list without weekdays changed its default on a router without days=")
	}
}

func TestFormats(t *testing.T) {
	want := map[string]Format{
		"spamhaus": FormatSpamhausJSON, "et": FormatOnePerLine, "cins": FormatOnePerLine,
		"blde": FormatOnePerLine, "greensnow": FormatOnePerLine, "dshield": FormatDShield, "bindef": FormatOnePerLine,
	}
	for _, l := range Lists() {
		if l.Format != want[l.Key] {
			t.Errorf("%s: format %q, want %q", l.Key, l.Format, want[l.Key])
		}
	}
}

// ListNames is the constant the push counts: one per list, plus
// Spamhaus's IPv6 list right after it.
func TestListNames(t *testing.T) {
	want := []string{
		"mv-bl-spamhaus", "mv-bl-spamhaus6", "mv-bl-et", "mv-bl-cins",
		"mv-bl-blde", "mv-bl-greensnow", "mv-bl-dshield", "mv-bl-bindef",
	}
	if got := ListNames(); !slices.Equal(got, want) {
		t.Errorf("ListNames() = %v, want %v", got, want)
	}
	// No name may be a prefix-match for another's ^mv-bl-<key> undo: an
	// undo of "et" by regex must not also sweep a list whose key begins
	// with "et".
	keys := []string{}
	for _, l := range Lists() {
		keys = append(keys, l.Key)
	}
	for _, a := range keys {
		for _, b := range keys {
			if a != b && strings.HasPrefix(b, a) {
				t.Errorf("key %q is a prefix of %q, so ^mv-bl-%s would match both", a, b, a)
			}
		}
	}
}

// Ten left out, none of them a list now offered, none named twice.
func TestLeftOut(t *testing.T) {
	got := LeftOutLists()
	if len(got) != 10 {
		t.Errorf("%d lists left out, want 10", len(got))
	}
	seen := map[string]bool{}
	for _, l := range got {
		if l.Name == "" || l.Why == "" {
			t.Errorf("left-out entry %+v is missing its name or reason", l)
		}
		if seen[l.Name] {
			t.Errorf("%q is left out twice", l.Name)
		}
		seen[l.Name] = true
		for _, offered := range Lists() {
			if strings.EqualFold(l.Name, offered.Name) || strings.EqualFold(l.Name, offered.Short) {
				t.Errorf("%q is both offered and left out", l.Name)
			}
		}
	}
}

// Every accessor hands out a copy, so a caller cannot rewrite the
// catalogue for everyone else.
func TestAccessorsReturnCopies(t *testing.T) {
	ls := Lists()
	ls[0].Key = "changed"
	ls[0].Refresh[0] = "changed"
	lo := LeftOutLists()
	lo[0].Name = "changed"
	if Lists()[0].Key != "spamhaus" || Lists()[0].Refresh[0] != RefreshSixHours || LeftOutLists()[0].Name == "changed" {
		t.Error("a caller's change reached the catalogue")
	}
}

// Pure data: the standard library only, so anything -- internal/routeros
// included -- can import it without dragging a dependency in.
func TestImportsOnlyTheStandardLibrary(t *testing.T) {
	pkg, err := build.ImportDir(".", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range pkg.Imports {
		if strings.Contains(strings.Split(imp, "/")[0], ".") {
			t.Errorf("imports %q, which is not the standard library", imp)
		}
	}
}
