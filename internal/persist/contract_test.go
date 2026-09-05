// SPDX-License-Identifier: AGPL-3.0-only

package persist

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/tomlawesome/mikroview/internal/retention"
)

// The point of this package is that a store can be moved from a file to
// Postgres without its behaviour changing. That only holds if both
// backends implement the same contract, so the contract is tested once
// and run against each -- rather than two test files that drift.
//
// The Postgres case skips itself when MIKROVIEW_TEST_POSTGRES is unset;
// the file and encrypted-file cases always run. encrypted-file (#853)
// pins that wrapping a key around FileBackend does not change the
// contract it presents to a store -- same compare-and-swap behaviour,
// same conflict handling -- only what ends up on disk.
func eachBackend(t *testing.T, run func(t *testing.T, b Backend)) {
	t.Helper()

	t.Run("file", func(t *testing.T) {
		run(t, NewFileBackend(filepath.Join(t.TempDir(), "store.json")))
	})

	t.Run("encrypted-file", func(t *testing.T) {
		key, err := retention.NewKeyFromMaterial([]byte(strings.Repeat("k", retention.MinKeyBytes)))
		if err != nil {
			t.Fatalf("test key: %v", err)
		}
		run(t, NewEncryptedFileBackend(filepath.Join(t.TempDir(), "store.json"), key))
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

// Concurrent writers may lose the compare-and-swap -- that is the
// contract, and ErrConflict says so. What they must never do is leave
// the *stored document* holding bytes that were never a whole payload.
//
// The file backend used to do exactly that: every writer shared one
// `path + ".tmp"` filename, written with os.WriteFile's O_TRUNC, so two
// overlapping saves interleaved inside the same temp file and the rename
// published the mixture. On the accounts store that is a fail-closed
// lockout, since internal/auth refuses to boot on a document it cannot
// parse. Two writers is the documented recovery workflow (a CLI command
// run against a live server), not a contrived case.
//
// The assertion is deliberately about *settled* state after both writers
// finish, not about what a reader sees mid-write -- a torn read during a
// write would be a different (and much weaker) claim.
func TestContractConcurrentWritersNeverPublishAMixedDocument(t *testing.T) {
	eachBackend(t, func(t *testing.T, b Backend) {
		// Different lengths so an interleaving leaves a recognisable
		// tail rather than a same-size overwrite that looks intact.
		big := []byte(`{"v":"` + strings.Repeat("A", 150_000) + `"}`)
		small := []byte(`{"v":"B"}`)

		if _, err := b.Save(context.Background(), small, 0); err != nil {
			t.Fatalf("seeding: %v", err)
		}

		for round := 0; round < 200; round++ {
			var wg sync.WaitGroup
			start := make(chan struct{})
			for _, payload := range [][]byte{big, small} {
				wg.Add(1)
				go func(payload []byte) {
					defer wg.Done()
					<-start
					snap, err := b.Load(context.Background())
					if err != nil {
						return
					}
					// ErrConflict is a correct outcome here and is
					// deliberately not asserted on.
					_, _ = b.Save(context.Background(), payload, snap.Version)
				}(payload)
			}
			close(start)
			wg.Wait()

			snap, err := b.Load(context.Background())
			if err != nil {
				t.Fatalf("round %d: settled Load: %v", round, err)
			}
			got := string(snap.Payload)
			if got != string(big) && got != string(small) {
				t.Fatalf("round %d: stored document is neither payload -- %d bytes, prefix %.80q",
					round, len(got), got)
			}
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
