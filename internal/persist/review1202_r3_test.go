// SPDX-License-Identifier: AGPL-3.0-only

package persist

// #1202 review round 3, S7: RefusePlaintextRows, the wrapper under every
// auth store's Postgres backend, refuses a row that holds plaintext JSON
// (an object, an array, `null`) with mikroview's own error -- the one a
// plaintext file gets, ErrPlaintextDocument, in the ruling-19 words --
// passes a sealed row through unchanged, and passes a missing row (a fresh
// store) through as "nothing stored". It runs on gauntlet's in-memory
// backend, so it needs no Postgres.

import (
	"bytes"
	"context"
	"errors"
	"testing"

	gpersist "github.com/tomlawesome/gauntlet/persist"
)

func memoryRow(t *testing.T, payload []byte) *gpersist.Memory {
	t.Helper()
	m := gpersist.NewMemory()
	if payload != nil {
		if _, err := m.Save(context.Background(), payload, 0); err != nil {
			t.Fatalf("planting the row: %v", err)
		}
	}
	return m
}

// #1202 review round 3, S7: a plaintext row is refused, whatever JSON it
// holds, with ErrPlaintextDocument in the ruling-19 wording, and is left as
// it was.
func TestRefusePlaintextRowsRefusesPlaintextJSONRows(t *testing.T) {
	cases := map[string][]byte{
		"object":       []byte(`{"users":[{"id":"aaaa","username":"admin","role":"admin"}]}`),
		"array":        []byte(`[{"id":"t1","name":"grafana","kind":"api"}]`),
		"null":         []byte(`null`),
		"empty object": []byte(`{}`),
	}
	for name, plain := range cases {
		t.Run(name, func(t *testing.T) {
			inner := memoryRow(t, plain)
			snap, err := RefusePlaintextRows(inner).Load(context.Background())
			if err == nil {
				t.Fatalf("a plaintext %s row was passed through: %q", name, snap.Payload)
			}
			if !errors.Is(err, ErrPlaintextDocument) {
				t.Errorf("err = %v, want one wrapping ErrPlaintextDocument", err)
			}
			wantRuling19Wording(t, name, err.Error())
			after, lerr := inner.Load(context.Background())
			if lerr != nil || !bytes.Equal(after.Payload, plain) {
				t.Errorf("the plaintext row was changed: %q, %v", after.Payload, lerr)
			}
		})
	}
}

// #1202 review round 3, S7: a sealed row passes through byte for byte, and
// a missing row is "nothing stored", not an error.
func TestRefusePlaintextRowsPassesASealedRowAndAMissingRow(t *testing.T) {
	ctx := context.Background()

	// A real sealed row, as an auth store writes one.
	holder := gpersist.NewMemory()
	sealed, err := gpersist.Encrypt(holder, pgKey(t), gpersist.EncryptOptions{Label: "auth"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sealed.Save(ctx, []byte(`{"users":[]}`), 0); err != nil {
		t.Fatal(err)
	}
	raw, err := holder.Load(ctx)
	if err != nil || !raw.Exists {
		t.Fatalf("test setup: no sealed row: %v", err)
	}
	if !bytes.Contains(raw.Payload, []byte(`"sealed"`)) {
		t.Fatalf("test setup: the row is not the {\"sealed\": ...} form: %.60s", raw.Payload)
	}

	snap, err := RefusePlaintextRows(holder).Load(ctx)
	if err != nil {
		t.Fatalf("a sealed row was refused: %v", err)
	}
	if !snap.Exists || !bytes.Equal(snap.Payload, raw.Payload) || snap.Version != raw.Version {
		t.Errorf("a sealed row was changed on the way through: exists=%v version %d -> %d", snap.Exists, raw.Version, snap.Version)
	}

	// A missing row: a fresh store.
	snap, err = RefusePlaintextRows(gpersist.NewMemory()).Load(ctx)
	if err != nil {
		t.Fatalf("a missing row was refused: %v", err)
	}
	if snap.Exists {
		t.Errorf("a missing row reads as existing: %+v", snap)
	}
}
