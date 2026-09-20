// SPDX-License-Identifier: AGPL-3.0-only

package device

import (
	"errors"
	"testing"
	"time"
)

// The operator-requested writes -- Create, Register, Delete -- must
// report a failed save and leave the registry exactly as it was
// (#1303). Each test builds its starting state with no backend, then
// swaps in one that always fails, so only the write under test hits it.

func TestCreateRollsBackWhenPersistFails(t *testing.T) {
	r, err := OpenRegistryWithBackend(&failingSaveBackend{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Create("hap-ax3", "hap-ax3", time.Now())
	if !errors.Is(err, ErrPersistFailed) {
		t.Fatalf("Create() error = %v, want ErrPersistFailed", err)
	}
	if got := r.List(); len(got) != 0 {
		t.Errorf("List() after a failed Create = %+v, want empty", got)
	}
	// The id is free again: a retry once the backend is back must not
	// see ErrDeviceExists.
	r.backend = nil
	if _, err := r.Create("hap-ax3", "hap-ax3", time.Now()); err != nil {
		t.Errorf("Create() retry after rollback = %v, want nil", err)
	}
}

func TestRegisterRollsBackWhenPersistFails(t *testing.T) {
	r, err := OpenRegistryWithBackend(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Create("hap-ax3", "old-name", time.Now()); err != nil {
		t.Fatal(err)
	}
	r.backend = &failingSaveBackend{}
	_, err = r.Register("hap-ax3", "new-name", time.Now())
	if !errors.Is(err, ErrPersistFailed) {
		t.Fatalf("Register() error = %v, want ErrPersistFailed", err)
	}
	got := r.List()
	if len(got) != 1 || got[0].Name != "old-name" || !got[0].RegisteredAt.IsZero() {
		t.Errorf("device after a failed Register = %+v, want old-name and no RegisteredAt", got)
	}
}

func TestDeleteRollsBackWhenPersistFails(t *testing.T) {
	r, err := OpenRegistryWithBackend(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if _, err := r.Create("hap-ax3", "hap-ax3", now); err != nil {
		t.Fatal(err)
	}
	// Enrol it for real, so Delete has an accepted address to clear, then
	// mint a fresh pending token on top, so it has one of those too.
	token, _, err := r.MintEnrolment("hap-ax3", "10.10.0.1", now)
	if err != nil {
		t.Fatal(err)
	}
	if !r.TryEnrol("10.10.0.1", []byte(`<30>Jan  1 00:00:00 router mikroview-enrol `+token)) {
		t.Fatal("TryEnrol() = false with a working backend")
	}
	if _, _, err := r.MintEnrolment("hap-ax3", "10.10.0.1", now); err != nil {
		t.Fatal(err)
	}
	r.backend = &failingSaveBackend{}
	if err := r.Delete("hap-ax3"); !errors.Is(err, ErrPersistFailed) {
		t.Fatalf("Delete() error = %v, want ErrPersistFailed", err)
	}
	got := r.List()
	if len(got) != 1 || got[0].AcceptedIP != "10.10.0.1" {
		t.Errorf("device after a failed Delete = %+v, want it still enrolled at 10.10.0.1", got)
	}
	if !r.Allowed("10.10.0.1") {
		t.Error("Allowed(10.10.0.1) = false after a failed Delete -- the address must still be claimed")
	}
	if p := r.PendingEnrolment("hap-ax3"); !p.Pending {
		t.Error("PendingEnrolment().Pending = false after a failed Delete -- the token must be back")
	}
	// Back in both indexes, not just the per-device one: redemption
	// looks the token up by hash.
	if len(r.pendingByHash) != 1 {
		t.Errorf("pendingByHash has %d entries after a failed Delete, want 1", len(r.pendingByHash))
	}
	r.backend = nil
	if err := r.Delete("hap-ax3"); err != nil {
		t.Errorf("Delete() retry after rollback = %v, want nil", err)
	}
}
