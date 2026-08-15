// SPDX-License-Identifier: AGPL-3.0-only

package tlssniff

import (
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func testLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// serve stands up a real TLS server behind the sniffing listener, so
// every test below exercises the same path a browser or a router does.
func serve(t *testing.T, allow HostAllower) (addr string, stop func()) {
	t.Helper()

	cert, err := selfSigned()
	if err != nil {
		t.Fatalf("generating a certificate: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "served over tls")
		}),
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert}},
	}
	sniffed := Listener(ln, testLog(), allow)
	go srv.ServeTLS(sniffed, "", "")
	return ln.Addr().String(), func() { srv.Close() }
}

func TestTLSClientIsUnaffected(t *testing.T) {
	addr, stop := serve(t, nil)
	defer stop()

	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}}
	res, err := client.Get("https://" + addr + "/anything")
	if err != nil {
		t.Fatalf("a real TLS request failed: %v", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if string(body) != "served over tls" {
		t.Errorf("body = %q -- the peeked byte was not replayed to the TLS server", body)
	}
}

func TestPlainHTTPGetsRedirectedToTheSamePort(t *testing.T) {
	addr, stop := serve(t, func(requested string) string { return requested })
	defer stop()

	// No redirect following: the Location header is the assertion.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	res, err := client.Get("http://" + addr + "/watchlist?x=1")
	if err != nil {
		t.Fatalf("plain HTTP request failed outright: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusPermanentRedirect {
		t.Errorf("status = %d, want %d", res.StatusCode, http.StatusPermanentRedirect)
	}
	want := "https://" + addr + "/watchlist?x=1"
	if got := res.Header.Get("Location"); got != want {
		t.Errorf("Location = %q, want %q -- the port and path must survive", got, want)
	}
}

// The Host header is attacker-controlled, so it must not be echoed into
// a Location unchecked (CWE-601) -- the same rule main.go's port-80
// redirect follows.
func TestHostAllowerCanRefuseAnArbitraryHost(t *testing.T) {
	addr, stop := serve(t, func(string) string { return "known-good:8443" })
	defer stop()

	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	fmt.Fprint(c, "GET /x HTTP/1.1\r\nHost: evil.example.com\r\n\r\n")
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	raw, _ := io.ReadAll(c)

	if strings.Contains(string(raw), "evil.example.com") {
		t.Errorf("an arbitrary Host was reflected into the response:\n%s", raw)
	}
	if !strings.Contains(string(raw), "https://known-good:8443/x") {
		t.Errorf("the allower's host was not used:\n%s", raw)
	}
}

func TestAllowerReturningEmptyDropsTheConnection(t *testing.T) {
	addr, stop := serve(t, func(string) string { return "" })
	defer stop()

	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	fmt.Fprint(c, "GET / HTTP/1.1\r\nHost: whatever\r\n\r\n")
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	raw, _ := io.ReadAll(c)
	if len(raw) != 0 {
		t.Errorf("expected nothing written, got %q", raw)
	}
}

// Garbage that is neither TLS nor HTTP must be closed, not answered and
// not left holding a slot.
func TestGarbageIsClosedWithoutAResponse(t *testing.T) {
	addr, stop := serve(t, func(s string) string { return s })
	defer stop()

	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	c.Write([]byte{0x00, 0x01, 0x02, 0x03})
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	raw, _ := io.ReadAll(c)
	if len(raw) != 0 {
		t.Errorf("garbage got a response: %q", raw)
	}

	// And the listener still works afterwards.
	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}}
	res, err := client.Get("https://" + addr + "/")
	if err != nil {
		t.Fatalf("the listener stopped serving TLS after garbage: %v", err)
	}
	res.Body.Close()
}

// A connection that says nothing must not stall the ones behind it --
// identification happens per connection, not in the accept loop.
func TestASilentClientDoesNotBlockOthers(t *testing.T) {
	addr, stop := serve(t, nil)
	defer stop()

	silent, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer silent.Close()

	done := make(chan error, 1)
	go func() {
		client := &http.Client{
			Timeout:   5 * time.Second,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
		}
		res, err := client.Get("https://" + addr + "/")
		if err == nil {
			res.Body.Close()
		}
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("a TLS client behind a silent one failed: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Error("a silent connection blocked the accept loop")
	}
}
