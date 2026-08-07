// SPDX-License-Identifier: AGPL-3.0-only

package persist

import (
	"context"
	"path/filepath"
	"testing"
)

// The point of this package is that a store can be moved from a file to
// Postgres without its behaviour changing. That only holds if both
// backends implement the same contract, so the contract is tested once
// and run against each -- rather than two test files that drift.
//
// The Postgres case skips itself when MIKROVIEW_TEST_POSTGRES is unset;
// the file case always runs.
func eachBackend(t *testing.T, run func(t *testing.T, b Backend)) {
	t.Helper()

	t.Run("file", func(t *testing.T) {
		run(t, NewFileBackend(filepath.Join(t.TempDir(), "store.json")))
	})

	t.Run("postgres", func(t *testing.T) {
		p := newTestPool(t)
		run(t, NewPostgresBackend(p, "contract"))
	})
}

func TestContractLoadOfAnUnwrittenStore(t *testing.T) {
	eachBackend(t, func(t *testing.T, b Backend) {
		snap, err := b.Load(context.Background())
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if snap.Exists {
			t.Error("Exists is true for a store that was never written")
		}
		if snap.Version != 0 {
			t.Errorf("Version = %d, want 0 for a store that was never written", snap.Version)
		}
		if len(snap.Payload) != 0 {
			t.Errorf("Payload = %q, want empty", snap.Payload)
		}
	})
}

func TestContractCreateThenRead(t *testing.T) {
	eachBackend(t, func(t *testing.T, b Backend) {
		ctx := context.Background()
		v, err := b.Save(ctx, []byte(`{"hello":"world"}`), 0)
		if err != nil {
			t.Fatalf("Save: %v", err)
		}
		snap, err := b.Load(ctx)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if string(snap.Payload) != `{"hello":"world"}` {
			t.Errorf("Payload = %q", snap.Payload)
		}
		if !snap.Exists {
			t.Error("Exists is false after a successful create")
		}
		if snap.Version != v {
			t.Errorf("Load version %d != Save version %d", snap.Version, v)
		}
	})
}

// Creating twice must fail on both backends. Otherwise two processes
// starting together could each believe they initialised the store.
func TestContractDoubleCreateIsAConflict(t *testing.T) {
	eachBackend(t, func(t *testing.T, b Backend) {
		ctx := context.Background()
		if _, err := b.Save(ctx, []byte(`{"n":1}`), 0); err != nil {
			t.Fatalf("first create: %v", err)
		}
		if _, err := b.Save(ctx, []byte(`{"n":2}`), 0); err != ErrConflict {
			t.Errorf("second create: got %v, want ErrConflict", err)
		}
	})
}

func TestContractStaleWriteIsRefused(t *testing.T) {
	eachBackend(t, func(t *testing.T, b Backend) {
		ctx := context.Background()
		v1, err := b.Save(ctx, []byte(`{"n":1}`), 0)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		v2, err := b.Save(ctx, []byte(`{"n":2}`), v1)
		if err != nil {
			t.Fatalf("update: %v", err)
		}
		if v2 == v1 {
			t.Error("version did not advance on write")
		}
		if _, err := b.Save(ctx, []byte(`{"n":3}`), v1); err != ErrConflict {
			t.Errorf("stale write: got %v, want ErrConflict", err)
		}

		snap, _ := b.Load(ctx)
		if string(snap.Payload) != `{"n":2}` {
			t.Errorf("stale write landed: %q", snap.Payload)
		}
	})
}

func TestContractSuccessiveWritesChain(t *testing.T) {
	eachBackend(t, func(t *testing.T, b Backend) {
		ctx := context.Background()
		v, err := b.Save(ctx, []byte(`{"n":0}`), 0)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		for i := 1; i <= 5; i++ {
			v, err = b.Save(ctx, []byte(`{"n":`+string(rune('0'+i))+`}`), v)
			if err != nil {
				t.Fatalf("write %d: %v", i, err)
			}
		}
		snap, _ := b.Load(ctx)
		if string(snap.Payload) != `{"n":5}` {
			t.Errorf("final payload = %q", snap.Payload)
		}
	})
}

// An empty document is a real, storable state -- distinct from "never
// written". Auto-migration depends on the difference: it must not
// overwrite a deliberately-empty store from a stale file.
func TestContractEmptyPayloadIsStillAnExistingStore(t *testing.T) {
	eachBackend(t, func(t *testing.T, b Backend) {
		ctx := context.Background()
		if _, err := b.Save(ctx, []byte(``), 0); err != nil {
			t.Fatalf("Save: %v", err)
		}
		snap, err := b.Load(ctx)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if !snap.Exists {
			t.Error("an empty document reports as never written")
		}
	})
}

func TestContractDescribeNeverLeaksACredential(t *testing.T) {
	eachBackend(t, func(t *testing.T, b Backend) {
		d := b.Describe()
		if d == "" {
			t.Error("Describe returned nothing")
		}
		// The test DSN's password. Describe feeds logs and the
		// config-problem surface, so it must never carry one.
		if contains(d, "devpass") {
			t.Errorf("Describe leaked a credential: %q", d)
		}
	})
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		(func() bool {
			for i := 0; i+len(needle) <= len(haystack); i++ {
				if haystack[i:i+len(needle)] == needle {
					return true
				}
			}
			return false
		})()
}
