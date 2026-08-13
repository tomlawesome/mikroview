// SPDX-License-Identifier: AGPL-3.0-only

// Package tlssniff lets one port answer both a TLS client and someone
// who typed the address into a browser.
//
// Mikroview usually gets one published port on a host that already runs
// other things -- 80 and 443 belong to something more important -- so it
// is reached as https://host:8080. A browser given "host:8080" tries
// http:// first, which lands plaintext on the TLS listener, and Go's
// server answers "Client sent an HTTP request to an HTTPS server". That
// is correct, useless, and the first thing a new operator sees.
//
// A TLS record begins 0x16, and no HTTP method does, so one byte
// separates the two. TLS connections are passed through untouched (the
// byte is replayed, so tls.Server sees a complete stream); a plaintext
// request gets a 308 to the same host, port and path over https, and is
// closed. Nothing else is served over plaintext, ever.
package tlssniff

import (
	"bufio"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/tomlawesome/mikroview/internal/logging"
)

// tlsRecordTypeHandshake is the first byte of every TLS ClientHello.
const tlsRecordTypeHandshake = 0x16

// sniffTimeout bounds how long a connection may take to reveal what it
// is. A client that connects and says nothing holds a goroutine and a
// slot until this expires; long enough for a slow link, short enough
// that idle connections are not a resource.
const sniffTimeout = 10 * time.Second

// maxConcurrentSniffs bounds how many not-yet-identified connections may
// be in flight. Past it, a new connection is closed rather than queued:
// the port is unauthenticated, so this is the one place a stranger can
// make us allocate before we know anything about them.
const maxConcurrentSniffs = 256

// maxRequestLineBytes bounds what is read from a plaintext client in
// order to build the redirect. Enough for a long URL and a Host header;
// a client sending more than this is not a browser.
const maxRequestLineBytes = 8 << 10

// HostAllower decides what host to send a redirect to. It receives the
// Host header the client sent and returns the host to use -- which lets
// the caller refuse to echo an arbitrary Host back (see main.go's
// existing redirect listener, which does the same thing against
// tls.hosts). Returning "" drops the redirect.
type HostAllower func(requested string) string

// Listener wraps inner so plaintext HTTP requests are answered with a
// redirect and never reach the TLS server. The returned listener yields
// only connections that began a TLS handshake.
func Listener(inner net.Listener, log *slog.Logger, allow HostAllower) net.Listener {
	l := &sniffingListener{
		inner: inner,
		allow: allow,
		log:   log,
		gate:  logging.NewLimiter(time.Minute),
		conns: make(chan net.Conn),
		errs:  make(chan error, 1),
		done:  make(chan struct{}),
		slots: make(chan struct{}, maxConcurrentSniffs),
	}
	go l.accept()
	return l
}

type sniffingListener struct {
	inner net.Listener
	allow HostAllower
	log   *slog.Logger
	gate  *logging.Limiter

	conns chan net.Conn
	errs  chan error
	done  chan struct{}
	slots chan struct{}

	closeOnce sync.Once
}

// accept runs the real accept loop. Identification happens in a
// goroutine per connection rather than inline: a client that connects
// and sends nothing would otherwise stall every other connection behind
// it for sniffTimeout.
func (l *sniffingListener) accept() {
	for {
		c, err := l.inner.Accept()
		if err != nil {
			select {
			case l.errs <- err:
			case <-l.done:
			}
			return
		}
		select {
		case l.slots <- struct{}{}:
		default:
			// Too many unidentified connections already in flight.
			if total, ok := l.gate.Allow(); ok {
				l.log.Warn(fmt.Sprintf(
					"%d connections have been dropped while waiting to identify themselves -- more than %d were pending at once",
					total, maxConcurrentSniffs))
			}
			c.Close()
			continue
		}
		go func() {
			defer func() { <-l.slots }()
			l.classify(c)
		}()
	}
}

func (l *sniffingListener) classify(c net.Conn) {
	if err := c.SetReadDeadline(time.Now().Add(sniffTimeout)); err != nil {
		c.Close()
		return
	}
	br := bufio.NewReader(c)
	first, err := br.Peek(1)
	if err != nil || len(first) == 0 {
		c.Close()
		return
	}

	if first[0] == tlsRecordTypeHandshake {
		// Hand it on with the peeked byte replayed, and the deadline
		// cleared -- from here the http.Server's own timeouts apply.
		if err := c.SetReadDeadline(time.Time{}); err != nil {
			c.Close()
			return
		}
		select {
		case l.conns <- &replayConn{Conn: c, r: br}:
		case <-l.done:
			c.Close()
		}
		return
	}

	defer c.Close()
	l.redirect(c, br)
}

// redirect answers a plaintext request with a 308 to the same address
// over https. It reads only enough to find the target, and writes the
// response by hand -- running a whole http.Server for this would mean
// serving HTTP on the TLS port, which is the thing being avoided.
func (l *sniffingListener) redirect(c net.Conn, br *bufio.Reader) {
	req, err := http.ReadRequest(bufio.NewReader(&limitedReader{r: br, n: maxRequestLineBytes}))
	if err != nil {
		return
	}

	host := req.Host
	if host == "" {
		// HTTP/1.0 without a Host header: fall back to the address the
		// connection arrived on, which is the one they dialled.
		if addr, ok := c.LocalAddr().(*net.TCPAddr); ok {
			host = addr.String()
		}
	}
	if l.allow != nil {
		host = l.allow(host)
	}
	if host == "" {
		return
	}

	target := url.URL{Scheme: "https", Host: host, Opaque: ""}
	// RequestURI carries the path and query exactly as sent; using it
	// directly (rather than reconstructing from req.URL) keeps an
	// encoded path encoded.
	uri := req.RequestURI
	if uri == "" || !strings.HasPrefix(uri, "/") {
		uri = "/"
	}
	location := target.String() + uri

	if total, ok := l.gate.Allow(); ok {
		l.log.Info(fmt.Sprintf(
			"redirected a plain HTTP request to %s (%d so far) -- mikroview serves HTTPS on this port",
			location, total))
	}

	_ = c.SetWriteDeadline(time.Now().Add(sniffTimeout))
	// 308 rather than 301/302: it preserves the method and body, and is
	// not cached as permanently as 301 by every browser that ever saw it
	// -- which matters on an appliance whose address can change.
	fmt.Fprintf(c, "HTTP/1.1 308 Permanent Redirect\r\n"+
		"Location: %s\r\n"+
		"Content-Type: text/plain; charset=utf-8\r\n"+
		"Content-Length: %d\r\n"+
		"Connection: close\r\n"+
		"\r\n%s",
		location, len(redirectBody(location)), redirectBody(location))
}

func redirectBody(location string) string {
	return "mikroview serves HTTPS on this port. Redirecting to " + location + "\n"
}

func (l *sniffingListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case err := <-l.errs:
		return nil, err
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *sniffingListener) Close() error {
	l.closeOnce.Do(func() { close(l.done) })
	return l.inner.Close()
}

func (l *sniffingListener) Addr() net.Addr { return l.inner.Addr() }

// replayConn hands back the bytes already read from the connection while
// working out what it was, so tls.Server sees the ClientHello whole.
type replayConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *replayConn) Read(p []byte) (int, error) { return c.r.Read(p) }

// limitedReader is io.LimitedReader with a clearer error: a request that
// runs past the cap should look like a malformed request, not EOF
// halfway through a header.
type limitedReader struct {
	r *bufio.Reader
	n int
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.n <= 0 {
		return 0, fmt.Errorf("tlssniff: request exceeded %d bytes", maxRequestLineBytes)
	}
	if len(p) > l.n {
		p = p[:l.n]
	}
	n, err := l.r.Read(p)
	l.n -= n
	return n, err
}
