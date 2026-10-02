// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/persist"
)

// seedAdmin registers a local admin so the store is past first-run
// setup: FindOrCreateOIDCUser refuses on an empty store (#1415), so a
// test about SSO provisioning starts from a deployment that already has
// its admin, as a real one does.
func seedAdmin(t *testing.T, s *Store, username string) *User {
	t.Helper()
	u, err := s.Register(username, "password12345", time.Now())
	if err != nil {
		t.Fatalf("seeding the admin: %v", err)
	}
	return u
}

// setupCodeRecorder is an Options.OnSetupCode that keeps every code
// announced, in order.
type setupCodeRecorder struct {
	mu    sync.Mutex
	codes []string
}

func (r *setupCodeRecorder) SetupCode(code string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.codes = append(r.codes, code)
}

func (r *setupCodeRecorder) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.codes...)
}

func openRecorded(t *testing.T, path string) (*Store, *setupCodeRecorder) {
	t.Helper()
	rec := &setupCodeRecorder{}
	s, err := OpenStore(persist.NewFileBackend(path), Options{OnSetupCode: rec})
	if err != nil {
		t.Fatal(err)
	}
	return s, rec
}

var setupCodeShape = regexp.MustCompile(`^[A-HJ-NP-Z2-9]{4}-[A-HJ-NP-Z2-9]{4}-[A-HJ-NP-Z2-9]{4}-[A-HJ-NP-Z2-9]{4}$`)

func TestEmptyStoreAnnouncesOneSetupCode(t *testing.T) {
	s, rec := openRecorded(t, filepath.Join(t.TempDir(), "users.json"))
	codes := rec.all()
	if len(codes) != 1 {
		t.Fatalf("announced %d codes on open, want 1", len(codes))
	}
	if !setupCodeShape.MatchString(codes[0]) {
		t.Errorf("code %q is not xxxx-xxxx-xxxx-xxxx over the reset-code alphabet", codes[0])
	}
	if err := s.CheckSetupCode(codes[0]); err != nil {
		t.Errorf("CheckSetupCode(announced) = %v, want nil", err)
	}
	// Typed back without dashes, in lower case, with spaces.
	loose := strings.ToLower(strings.ReplaceAll(codes[0], "-", " "))
	if err := s.CheckSetupCode(loose); err != nil {
		t.Errorf("CheckSetupCode(%q) = %v, want nil", loose, err)
	}
	for _, wrong := range []string{"", "AAAA-AAAA-AAAA-AAAA", codes[0] + "A", codes[0][:len(codes[0])-1]} {
		if err := s.CheckSetupCode(wrong); !errors.Is(err, ErrSetupCodeInvalid) {
			t.Errorf("CheckSetupCode(%q) = %v, want ErrSetupCodeInvalid", wrong, err)
		}
	}
	// Only the hash is held: nothing in the store's memory is the code.
	if string(s.setupCodeHash) == NormaliseResetCode(codes[0]) {
		t.Error("the store kept the code itself rather than its hash")
	}
}

func TestEachOpenOfAnEmptyStoreIssuesANewCode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	first, firstRec := openRecorded(t, path)
	second, secondRec := openRecorded(t, path) // a restart
	a, b := firstRec.all()[0], secondRec.all()[0]
	if a == b {
		t.Fatal("a restart announced the same code again")
	}
	if err := second.CheckSetupCode(a); !errors.Is(err, ErrSetupCodeInvalid) {
		t.Errorf("the previous process's code still works after a restart: %v", err)
	}
	_ = first
}

func TestSetupCodeIsDeadOnceAnAccountExists(t *testing.T) {
	s, rec := openRecorded(t, filepath.Join(t.TempDir(), "users.json"))
	code := rec.all()[0]
	if _, err := s.Register("admin", "password12345", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckSetupCode(code); !errors.Is(err, ErrRegistrationClosed) {
		t.Errorf("CheckSetupCode after the first account = %v, want ErrRegistrationClosed", err)
	}
	if s.setupCodeHash != nil {
		t.Error("the setup code's hash outlived the first account")
	}
}

func TestStoreWithAccountsAnnouncesNoCode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	s, _ := openRecorded(t, path)
	seedAdmin(t, s, "admin")

	_, rec := openRecorded(t, path)
	if got := rec.all(); len(got) != 0 {
		t.Errorf("a store holding an account announced %d setup codes, want none", len(got))
	}
}

func TestUnpersistedStoreHasNoSetupCode(t *testing.T) {
	rec := &setupCodeRecorder{}
	s, err := OpenStore(nil, Options{OnSetupCode: rec})
	if err != nil {
		t.Fatal(err)
	}
	if got := rec.all(); len(got) != 0 {
		t.Errorf("an unpersisted store announced %d codes, want none", len(got))
	}
	if err := s.CheckSetupCode("AAAA-AAAA-AAAA-AAAA"); !errors.Is(err, ErrNotPersisted) {
		t.Errorf("CheckSetupCode on an unpersisted store = %v, want ErrNotPersisted", err)
	}
}

// A document emptied underneath a running server -- a bad restore, say
// -- puts it back into setup with a fresh code, never the old one.
func TestReloadOfAnEmptiedDocumentIssuesAFreshCode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	s, rec := openRecorded(t, path)
	first := rec.all()[0]
	seedAdmin(t, s, "admin")

	emptied, err := json.Marshal(storeFile{Users: []*User{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, emptied, 0o600); err != nil {
		t.Fatal(err)
	}
	// CheckSetupCode reloads first, so the emptied document decides.
	if err := s.CheckSetupCode(first); !errors.Is(err, ErrSetupCodeInvalid) {
		t.Errorf("the first code works again after the document was emptied: %v", err)
	}
	codes := rec.all()
	if len(codes) != 2 {
		t.Fatalf("announced %d codes, want a second one after the reload emptied the store", len(codes))
	}
	if codes[1] == first {
		t.Error("the reload re-announced the old code")
	}
	if err := s.CheckSetupCode(codes[1]); err != nil {
		t.Errorf("CheckSetupCode(fresh code) = %v, want nil", err)
	}
}

// A reload that applies accounts written by another process retires the
// code in this one.
func TestReloadThatAppliesAccountsRetiresTheCode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	s, rec := openRecorded(t, path)
	code := rec.all()[0]

	other, err := OpenStore(persist.NewFileBackend(path), Options{OnSetupCode: DiscardSetupCode})
	if err != nil {
		t.Fatal(err)
	}
	seedAdmin(t, other, "admin")

	if err := s.CheckSetupCode(code); !errors.Is(err, ErrRegistrationClosed) {
		t.Errorf("CheckSetupCode after another process created an account = %v, want ErrRegistrationClosed", err)
	}
}

func TestSetupCodeLogLineCarriesTheCode(t *testing.T) {
	line := setupCodeLogLine("ABCD-EFGH-JKLM-NPQR")
	if !strings.Contains(line, "ABCD-EFGH-JKLM-NPQR") || strings.Contains(line, "\n") {
		t.Errorf("log line = %q, want one line carrying the code", line)
	}
}
