// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/persist"
)

// loadCountingBackend is reloadRaceBackend (no VersionReader, like
// persist.FileBackend, so every staleness check is a whole Load) with a
// count of those Loads.
type loadCountingBackend struct {
	reloadRaceBackend
	loads atomic.Int64
}

func (b *loadCountingBackend) Load(ctx context.Context) (persist.Snapshot, error) {
	b.loads.Add(1)
	return b.reloadRaceBackend.Load(ctx)
}

// TestListWithSecondFactorsReadsTheStoreOnce is #1345 E1-F1: the admin
// users list must cost one read of the accounts document however many
// accounts there are, and give exactly the answers List, HasActiveTOTP
// and PasskeyCount give one at a time.
func TestListWithSecondFactorsReadsTheStoreOnce(t *testing.T) {
	b := &loadCountingBackend{}
	s, err := OpenWithBackend(b)
	if err != nil {
		t.Fatal(err)
	}
	// The admin first -- SSO never provisions the first account (#1415).
	// Named to sort after the three below, whose positions the checks
	// at the end rely on.
	seedAdmin(t, s, "zadmin")
	now := time.Now()
	// OIDC accounts, so no password has to be hashed to make them.
	var ids []string
	for _, name := range []string{"bilbo", "frodo", "sam"} {
		u, _, err := s.FindOrCreateOIDCUser("https://idp.example", "subject-"+name, name, now)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, u.ID)
	}
	if err := s.SetPendingTOTPSecret(ids[0], "JBSWY3DPEHPK3PXP"); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfirmTOTP(ids[0], now, 1); err != nil {
		t.Fatal(err)
	}
	for i := byte(1); i <= 2; i++ {
		if _, err := s.AddPasskey(ids[1], testPasskey(i, "")); err != nil {
			t.Fatal(err)
		}
	}

	b.loads.Store(0)
	got := s.ListWithSecondFactors()
	if n := b.loads.Load(); n != 1 {
		t.Errorf("ListWithSecondFactors read the accounts document %d times for %d accounts, want 1", n, len(ids))
	}

	want := s.List()
	if len(got) != len(want) {
		t.Fatalf("ListWithSecondFactors returned %d rows, List %d", len(got), len(want))
	}
	for i, row := range got {
		if row.User.ID != want[i].ID || row.User.Username != want[i].Username {
			t.Errorf("row %d = %s/%s, want List's order and content %s/%s", i, row.User.ID, row.User.Username, want[i].ID, want[i].Username)
		}
		if row.User.TOTPSecret != "" || row.User.Passkeys != nil || row.User.RecoveryCodes != nil {
			t.Errorf("row %d carries credential material List blanks: %+v", i, row.User)
		}
		if w := s.HasActiveTOTP(row.User.ID); row.HasActiveTOTP != w {
			t.Errorf("%s: HasActiveTOTP = %t, want %t", row.User.Username, row.HasActiveTOTP, w)
		}
		if w := s.PasskeyCount(row.User.ID); row.PasskeyCount != w {
			t.Errorf("%s: PasskeyCount = %d, want %d", row.User.Username, row.PasskeyCount, w)
		}
	}
	if !got[0].HasActiveTOTP || got[1].PasskeyCount != 2 || got[2].HasActiveTOTP || got[2].PasskeyCount != 0 {
		t.Errorf("rows = %+v, want bilbo with a factor, frodo with two passkeys, sam with neither", got)
	}
}
