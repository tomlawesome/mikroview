// SPDX-License-Identifier: AGPL-3.0-only

package seen

import (
	"context"
	"testing"
	"time"
)

// #1451: Flush on a nil register and on one with no backend are safe no-ops.
func TestFlushIsASafeNoOpWithNothingToWriteTo(t *testing.T) {
	var nilRegister *Register
	if err := nilRegister.Flush(context.Background()); err != nil {
		t.Errorf("nil register Flush = %v", err)
	}
	r, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Flush(context.Background()); err != nil {
		t.Errorf("Flush with no backend = %v", err)
	}
}

func TestFlushWritesToTheBackendNow(t *testing.T) {
	path := t.TempDir() + "/seen.json"
	r, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	r.Observe("tcp", "ether1", "ether2", time.Now())
	if err := r.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	// Read the file back without closing the first register: Flush alone must have written it.
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, v := range again.List(FieldProto) {
		found = found || v.Value == "tcp"
	}
	if !found {
		t.Errorf("tcp not on disk after Flush: %+v", again.List(FieldProto))
	}
	_ = again.Close(context.Background())
	_ = r.Close(context.Background())
}
