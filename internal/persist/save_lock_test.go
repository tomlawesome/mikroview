// SPDX-License-Identifier: AGPL-3.0-only

package persist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/tomlawesome/mikroview/internal/retention"
)

// #1468: Save checks the stored version and then renames, which is not
// atomic across processes, so two processes saving one store can both pass
// the check and one update is lost. EncryptedFileBackend.Load's re-seal of a
// legacy-bound file has the same window. These tests run real processes
// (the test binary re-executed, as internal/store/memory_test.go does) that
// each add unique entries with a Load / Save-with-version / retry-on-
// ErrConflict loop, and require every entry to survive.

const (
	saveLockKindEnv   = "MV_SAVELOCK_KIND"
	saveLockPathEnv   = "MV_SAVELOCK_PATH"
	saveLockIDEnv     = "MV_SAVELOCK_ID"
	saveLockRoundsEnv = "MV_SAVELOCK_ROUNDS"
	saveLockSeed      = 0x2a
)

func saveLockBackend(kind, path string, t *testing.T) Backend {
	switch kind {
	case "file":
		return NewFileBackend(path)
	case "sealed":
		return NewEncryptedFileBackend(path, "locktest", testKey(t, saveLockSeed))
	case "legacy":
		// Reads a file sealed under its path binding (re-sealing it on
		// Load) and writes under the store name.
		return NewEncryptedFileBackendForPath(path, path, "locktest", testKey(t, saveLockSeed))
	}
	t.Fatalf("unknown kind %q", kind)
	return nil
}

// TestSaveLockHelper is the child process body; it does nothing in a normal run.
func TestSaveLockHelper(t *testing.T) {
	kind := os.Getenv(saveLockKindEnv)
	if kind == "" {
		t.Skip("helper for the SaveLock tests")
	}
	path, id := os.Getenv(saveLockPathEnv), os.Getenv(saveLockIDEnv)
	rounds, err := strconv.Atoi(os.Getenv(saveLockRoundsEnv))
	if err != nil {
		t.Fatal(err)
	}
	b := saveLockBackend(kind, path, t)
	ctx := context.Background()
	for r := 0; r < rounds; r++ {
		entry := fmt.Sprintf("p%s-r%d", id, r)
		for {
			snap, err := b.Load(ctx)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			doc := map[string]string{}
			if snap.Exists {
				if err := json.Unmarshal(snap.Payload, &doc); err != nil {
					t.Fatalf("document is not valid JSON: %v", err)
				}
			}
			doc[entry] = "x"
			out, _ := json.Marshal(doc)
			if _, err := b.Save(ctx, out, snap.Version); err == nil {
				break
			} else if !errors.Is(err, ErrConflict) {
				t.Fatalf("save: %v", err)
			}
		}
	}
}

// runSaveLockProcs starts procs children against path and waits for all.
func runSaveLockProcs(t *testing.T, kind, path string, procs, rounds int) {
	t.Helper()
	var wg sync.WaitGroup
	outs := make([][]byte, procs)
	errs := make([]error, procs)
	for i := 0; i < procs; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run=^TestSaveLockHelper$")
			cmd.Env = append(os.Environ(),
				saveLockKindEnv+"="+kind, saveLockPathEnv+"="+path,
				saveLockIDEnv+"="+strconv.Itoa(i), saveLockRoundsEnv+"="+strconv.Itoa(rounds))
			outs[i], errs[i] = cmd.CombinedOutput()
		}(i)
	}
	wg.Wait()
	for i := range errs {
		if errs[i] != nil {
			t.Errorf("process %d failed: %v\n%s", i, errs[i], outs[i])
		}
	}
}

func checkSaveLockDoc(t *testing.T, kind, path string, procs, rounds int) {
	t.Helper()
	snap, err := saveLockBackend(kind, path, t).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	doc := map[string]string{}
	if err := json.Unmarshal(snap.Payload, &doc); err != nil {
		t.Fatalf("final document unreadable: %v", err)
	}
	if len(doc) != procs*rounds {
		t.Errorf("document holds %d entries, want %d: %d saves were lost", len(doc), procs*rounds, procs*rounds-len(doc))
	}
}

func TestSaveLockFileBackendAcrossProcessesLosesNoSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.json")
	const procs, rounds = 8, 50
	runSaveLockProcs(t, "file", path, procs, rounds)
	checkSaveLockDoc(t, "file", path, procs, rounds)
}

func TestSaveLockEncryptedBackendAcrossProcessesLosesNoSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.json")
	const procs, rounds = 8, 50
	runSaveLockProcs(t, "sealed", path, procs, rounds)
	checkSaveLockDoc(t, "sealed", path, procs, rounds)
}

// Many processes open a file still sealed under its old path binding at
// once, so each Load tries to re-seal it while the others save. Repeated on
// fresh files because the legacy window closes after the first re-seal.
func TestSaveLockLegacyRebindAcrossProcessesLosesNoSave(t *testing.T) {
	const procs, rounds, files = 8, 10, 15
	for f := 0; f < files; f++ {
		path := filepath.Join(t.TempDir(), "store.json")
		sealed, err := testKey(t, saveLockSeed).Seal(retention.StateStoreKeyInfo, []byte(path), []byte(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, sealed, 0o600); err != nil {
			t.Fatal(err)
		}
		runSaveLockProcs(t, "legacy", path, procs, rounds)
		checkSaveLockDoc(t, "legacy", path, procs, rounds)
		if t.Failed() {
			t.Logf("failed on file %d of %d", f+1, files)
			return
		}
	}
}
