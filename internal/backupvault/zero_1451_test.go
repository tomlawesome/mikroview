// SPDX-License-Identifier: AGPL-3.0-only

package backupvault

import (
	"errors"
	"testing"
	"time"
)

// #1451: paths no other test reached.

func TestLooksLikeBackupAcceptsPlainAndEncryptedHeadersOnly(t *testing.T) {
	plain := append(append([]byte{}, plainMagic...), "rest of the file"...)
	enc := append(append([]byte{}, encryptedMagic...), 0x73, 'x')
	for name, data := range map[string][]byte{"plain": plain, "encrypted": enc} {
		if !LooksLikeBackup(data) {
			t.Errorf("%s header not recognised", name)
		}
	}
	for name, data := range map[string][]byte{
		"empty": nil, "short": plainMagic[:2], "text": []byte("/ip address print"), "zeros": make([]byte, 16),
	} {
		if LooksLikeBackup(data) {
			t.Errorf("%s accepted as a backup", name)
		}
	}
}

func TestGenerationArrivedAtIsTheLaterHalf(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		g    Generation
		want time.Time
	}{
		{"backup only", Generation{BackupArrivedAt: t0}, t0},
		{"rsc only", Generation{RscArrivedAt: t0}, t0},
		{"rsc later", Generation{BackupArrivedAt: t0, RscArrivedAt: t0.Add(time.Minute)}, t0.Add(time.Minute)},
		{"backup later", Generation{BackupArrivedAt: t0.Add(time.Hour), RscArrivedAt: t0}, t0.Add(time.Hour)},
	}
	for _, c := range cases {
		if got := c.g.ArrivedAt(); !got.Equal(c.want) {
			t.Errorf("%s: ArrivedAt() = %v, want %v", c.name, got, c.want)
		}
	}
	if !(Generation{}).ArrivedAt().IsZero() {
		t.Error("an empty generation has an arrival time")
	}
}

func TestRemoveGenerationDropsOnlyThatIDWithoutTouchingTheInput(t *testing.T) {
	gens := []*generationMeta{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	got := removeGeneration(gens, "b")
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "c" {
		t.Fatalf("removeGeneration = %v", got)
	}
	if len(gens) != 3 || gens[1].ID != "b" {
		t.Errorf("input slice was modified: %v", gens)
	}
	if again := removeGeneration(gens, "zzz"); len(again) != 3 {
		t.Errorf("removing an absent id changed the list: %v", again)
	}
	if empty := removeGeneration(nil, "a"); len(empty) != 0 {
		t.Errorf("removeGeneration(nil) = %v", empty)
	}
}

func TestAbandonChangeReleasesTheClaimAndReturnsTheCause(t *testing.T) {
	v := &Vault{}
	st := &lockState{}
	v.lock = st
	cause := errors.New("sealing failed")
	if err := v.abandonChange(st, cause); !errors.Is(err, cause) {
		t.Errorf("abandonChange returned %v, want the cause", err)
	}
	if v.lock != nil {
		t.Error("the claim was not released")
	}
	// A claim somebody else has since taken is left alone.
	other := &lockState{}
	v.lock = other
	_ = v.abandonChange(st, cause)
	if v.lock != other {
		t.Error("abandonChange released a claim it did not hold")
	}
}

func TestSetSpaceProbeForTestInstallsTheProbeAndIsNilSafe(t *testing.T) {
	var nilVault *Vault
	nilVault.SetSpaceProbeForTest(func(string) (int64, int64, error) { return 0, 0, nil }) // must not panic
	v := &Vault{}
	v.SetSpaceProbeForTest(func(string) (int64, int64, error) { return 5, 10, nil })
	free, total, err := v.statfs("/")
	if err != nil || free != 5 || total != 10 {
		t.Errorf("probe = %d %d %v", free, total, err)
	}
}
