// SPDX-License-Identifier: AGPL-3.0-only

package authe2e

// Build notes B8.38 and B8.39 (the password list is a refresher), as far
// as a process test can reach. The list is signed by a key compiled into
// gauntlet, so a test cannot publish a newer list that verifies; what is
// tested here is everything around that: the request order, that a list
// with a bad signature or checksum is never adopted, that a genuine
// signed list is recognised as genuine (it is refused only for not being
// newer), that an unreachable host leaves start-up working with one Warn
// and the built-in list in force, that the directory is
// <data>/password-list, and that Have I Been Pwned is never asked.
//
// Not testable without a change to gauntlet or a compiled-in test key:
// "after it publishes a newer signed list, a password on that list is
// refused" and "a restart reads the newer list kept in the directory".
// See the commit message.

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const listLogMarker = "common-password list refresh failed"

type listHost struct {
	srv    *httptest.Server
	caFile string
	mu     sync.Mutex
	paths  []string
}

func newListHost(t *testing.T, body []byte, sum string, sig []byte) *listHost {
	t.Helper()
	h := &listHost{}
	h.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		h.paths = append(h.paths, r.URL.Path)
		h.mu.Unlock()
		switch r.URL.Path {
		case "/top10k.txt.sha256":
			_, _ = w.Write([]byte(sum + "  top10k.txt\n"))
		case "/top10k.txt":
			_, _ = w.Write(body)
		case "/top10k.txt.sig":
			_, _ = w.Write(sig)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(h.srv.Close)
	h.caFile = writeCert(t, h.srv)
	return h
}

func (h *listHost) url() string { return h.srv.URL + "/top10k.txt" }

func (h *listHost) seen() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.paths...)
}

func (h *listHost) waitFor(t *testing.T, n int) []string {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if p := h.seen(); len(p) >= n {
			return p
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("the list host saw %v, want at least %d requests", h.seen(), n)
	return nil
}

func sha(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// genuinePair is the signed list compiled into the gauntlet release this
// module is built against, read from the module's own files.
func genuinePair(t *testing.T) (list, sig []byte) {
	t.Helper()
	cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/tomlawesome/gauntlet")
	cmd.Dir = moduleRoot()
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("locating the gauntlet module: %v", err)
	}
	dir := strings.TrimSpace(string(out))
	return readBytes(t, filepath.Join(dir, "blocklist", "embedded", "top10k.txt")),
		readBytes(t, filepath.Join(dir, "blocklist", "embedded", "top10k.txt.sig"))
}

func listOpts(h *listHost) Opts {
	return Opts{Env: map[string]string{
		"MV_TEST_PASSWORDLIST_URL": h.url(),
		"SSL_CERT_FILE":            h.caFile,
	}}
}

func refusedAsCommon(t *testing.T, w *world, password string) bool {
	t.Helper()
	r := w.c.post("/api/auth/users", map[string]string{"username": "list-probe-" + randHex(t, 3), "password": password, "role": "user"})
	return r.Status == 400
}

// #1202 B8.38: the first check goes to the list host and sees the checksum
// request first. A list whose signature does not verify is never adopted
// (it is fetched, its signature fetched, and refused with one Warn); one
// whose checksum does not match is not even asked for a signature.
func TestPasswordListRefresherAsksForTheChecksumFirstAndAdoptsNothingUnsigned(t *testing.T) {
	list, sig := genuinePair(t)
	// A list with one more header line: still well-formed, so the signature
	// is the only thing wrong with it.
	tampered := []byte(strings.Replace(string(list), "\n# ", "\n# note: tampered\n# ", 1))
	if string(tampered) == string(list) {
		t.Fatal("test setup: could not alter the list")
	}

	t.Run("signature does not verify", func(t *testing.T) {
		t.Parallel()
		h := newListHost(t, tampered, sha(tampered), sig)
		w := bootstrap(t, listOpts(h))
		paths := h.waitFor(t, 3)
		want := []string{"/top10k.txt.sha256", "/top10k.txt", "/top10k.txt.sig"}
		for i := range want {
			if paths[i] != want[i] {
				t.Fatalf("requests = %v, want the checksum first, then the list, then the signature: %v", paths, want)
			}
		}
		waitLog(t, w.a, listLogMarker)
		if strings.Contains(w.a.log(), "adopted a new common-password list") {
			t.Error("a list with a bad signature was adopted")
		}
		if !refusedAsCommon(t, w, "password123") {
			t.Error("the built-in list stopped refusing password123")
		}
	})

	t.Run("checksum does not match", func(t *testing.T) {
		t.Parallel()
		h := newListHost(t, list, sha([]byte("something else")), sig)
		w := bootstrap(t, listOpts(h))
		paths := h.waitFor(t, 2)
		if paths[0] != "/top10k.txt.sha256" || paths[1] != "/top10k.txt" {
			t.Fatalf("requests = %v, want the checksum, then the list", paths)
		}
		waitLog(t, w.a, listLogMarker)
		time.Sleep(time.Second)
		for _, p := range h.seen() {
			if p == "/top10k.txt.sig" {
				t.Error("the signature was fetched for a list whose checksum did not match")
			}
		}
		if strings.Contains(w.a.log(), "adopted a new common-password list") {
			t.Error("a list with a bad checksum was adopted")
		}
	})
}

// #1202 B8.38: with the host unreachable, start-up still succeeds, one Warn
// is logged, and a password on the built-in list is still refused. No
// request goes to Have I Been Pwned.
func TestPasswordListHostUnreachableStartsWithOneWarnAndNeverAsksHIBP(t *testing.T) {
	t.Parallel()
	proxy := newRecordingProxy(t)
	// Nothing listens on this port.
	dead := freePort(t)
	o := Opts{Env: map[string]string{
		"MV_TEST_PASSWORDLIST_URL": "https://127.0.0.1:" + strconv.Itoa(dead) + "/top10k.txt",
		"HTTPS_PROXY":              proxy.addr, "HTTP_PROXY": proxy.addr, "https_proxy": proxy.addr, "http_proxy": proxy.addr,
	}}
	w := bootstrap(t, o)
	waitLog(t, w.a, listLogMarker)
	time.Sleep(2 * time.Second)

	n := 0
	for _, l := range strings.Split(w.a.log(), "\n") {
		if strings.Contains(l, listLogMarker) {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d refresh-failure warnings after start-up with the host unreachable, want exactly 1\n%s", n, w.a.log())
	}

	// The built-in list works; a different password is fine.
	if !refusedAsCommon(t, w, "password123") {
		t.Error("a password on the built-in list was accepted")
	}
	r := w.c.post("/api/auth/users", map[string]string{"username": "probe-ok", "password": "Zq7!vLm3Rt9x", "role": "user"})
	wantStatus(t, "a password on no list", r, 201)
	wantStatus(t, "changing the admin's password", w.c.post("/api/auth/password", map[string]string{"currentPassword": w.admin.pass, "newPassword": "Hn5!wBe2Kp8d"}), 200)

	for _, host := range proxy.hosts() {
		if strings.Contains(host, "pwnedpasswords") || strings.Contains(host, "haveibeenpwned") {
			t.Errorf("a request went to Have I Been Pwned: %s", host)
		}
	}
}

// #1202 B8.38/B8.39 (what can be reached): the list is kept in
// <data>/password-list, and what is found there at start-up is read before
// any fetch: a stored copy that does not verify is ignored with a Warn
// naming the directory, and the built-in list stays in force.
func TestPasswordListIsKeptInTheDataDirectoryAndReadAtStartUp(t *testing.T) {
	t.Parallel()
	list, _ := genuinePair(t)
	root := t.TempDir()
	dir := filepath.Join(root, "data", "password-list")
	must(t, os.MkdirAll(dir, 0o700))
	// A stored pair whose signature is not one any trusted key made.
	stored := "gauntlet-stored-list/1 12\nnot-a-sig-xx" + string(list)
	must(t, os.WriteFile(filepath.Join(dir, "top10k.signed"), []byte(stored), 0o600))

	dead := freePort(t)
	w := bootstrap(t, Opts{Root: root, Env: map[string]string{"MV_TEST_PASSWORDLIST_URL": "https://127.0.0.1:" + strconv.Itoa(dead) + "/top10k.txt"}})
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(w.a.log(), "ignoring the stored common-password list") {
		time.Sleep(100 * time.Millisecond)
	}
	if !strings.Contains(w.a.log(), "ignoring the stored common-password list") {
		t.Errorf("the stored copy in %s was not read at start-up:\n%s", dir, w.a.log())
	}
	if !strings.Contains(w.a.log(), filepath.Join(root, "data", "password-list")) {
		t.Errorf("the log does not name the directory %s", dir)
	}
	if !refusedAsCommon(t, w, "password123") {
		t.Error("the built-in list is not in force")
	}
}

func waitLog(t *testing.T, a *app, substr string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(a.log(), substr) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("the log never said %q:\n%s", substr, a.log())
}

// recordingProxy answers every CONNECT with 502 and remembers the host.
// Go's HTTP client never sends loopback traffic through a proxy, so what it
// records is everything the process tried to reach outside this machine.
type recordingProxy struct {
	addr string
	mu   sync.Mutex
	seen []string
}

func newRecordingProxy(t *testing.T) *recordingProxy {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &recordingProxy{addr: "http://" + l.Addr().String()}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				line, err := bufio.NewReader(conn).ReadString('\n')
				if err != nil {
					return
				}
				p.mu.Lock()
				p.seen = append(p.seen, strings.TrimSpace(line))
				p.mu.Unlock()
				_, _ = conn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n"))
			}()
		}
	}()
	return p
}

func (p *recordingProxy) hosts() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.seen...)
}
