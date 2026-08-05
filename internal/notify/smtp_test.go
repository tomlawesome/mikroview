package notify

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/tomlawesome/mikroview/internal/flags"
)

// fakeSMTPServer speaks just enough SMTP to accept one message and
// capture it -- net/smtp has no first-party test double, and pulling in
// a real mail server for this is overkill for what's being verified
// here (the client sequence and the composed message), so a minimal
// hand-rolled listener is the standard way to test this.
type fakeSMTPServer struct {
	addr     string
	received chan string
}

func startFakeSMTPServer(t *testing.T) *fakeSMTPServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeSMTPServer{addr: ln.Addr().String(), received: make(chan string, 1)}

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		s.serve(conn)
	}()
	t.Cleanup(func() { ln.Close() })
	return s
}

func (s *fakeSMTPServer) serve(conn net.Conn) {
	r := bufio.NewReader(conn)
	fmt.Fprintf(conn, "220 fake.smtp.test ESMTP\r\n")

	var msg strings.Builder
	inData := false
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		trimmed := strings.TrimRight(line, "\r\n")

		if inData {
			if trimmed == "." {
				inData = false
				s.received <- msg.String()
				fmt.Fprintf(conn, "250 OK\r\n")
				continue
			}
			msg.WriteString(trimmed)
			msg.WriteString("\n")
			continue
		}

		upper := strings.ToUpper(trimmed)
		switch {
		case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
			fmt.Fprintf(conn, "250-fake.smtp.test\r\n250 OK\r\n")
		case strings.HasPrefix(upper, "MAIL FROM"):
			fmt.Fprintf(conn, "250 OK\r\n")
		case strings.HasPrefix(upper, "RCPT TO"):
			fmt.Fprintf(conn, "250 OK\r\n")
		case upper == "DATA":
			inData = true
			fmt.Fprintf(conn, "354 Start mail input\r\n")
		case upper == "QUIT":
			fmt.Fprintf(conn, "221 Bye\r\n")
			return
		default:
			fmt.Fprintf(conn, "500 unrecognized\r\n")
		}
	}
}

func TestSMTPNotifierSendsComposedMessage(t *testing.T) {
	server := startFakeSMTPServer(t)
	host, port := splitHostPort(t, server.addr)

	confidence := 87
	n := NewSMTPNotifier(SMTPConfig{
		Host: host, Port: port,
		From: "mikroview@example.com",
		To:   []string{"ops@example.com"},
		// TLSMode left at TLSNone -- the fake server only speaks
		// plaintext SMTP.
	})

	batch := []flags.Flag{
		{Type: flags.TypePortScan, Target: "203.0.113.9", Detail: "15 distinct ports in 60s", Confidence: &confidence},
	}
	if err := n.Send(batch); err != nil {
		t.Fatalf("Send returned an error: %v", err)
	}

	select {
	case msg := <-server.received:
		if !strings.Contains(msg, "Subject: mikroview: 1 new flag") {
			t.Errorf("expected a subject naming the batch size, got:\n%s", msg)
		}
		if !strings.Contains(msg, "203.0.113.9") || !strings.Contains(msg, "15 distinct ports in 60s") || !strings.Contains(msg, "87%") {
			t.Errorf("expected the message body to describe the flag, got:\n%s", msg)
		}
	default:
		t.Fatal("fake server never received a message")
	}
}

func TestSMTPNotifierSkipsEmptyBatch(t *testing.T) {
	server := startFakeSMTPServer(t)
	host, port := splitHostPort(t, server.addr)
	n := NewSMTPNotifier(SMTPConfig{Host: host, Port: port, From: "a@example.com", To: []string{"b@example.com"}})

	if err := n.Send(nil); err != nil {
		t.Fatalf("expected no error for an empty batch, got %v", err)
	}
	select {
	case msg := <-server.received:
		t.Fatalf("expected no connection attempt for an empty batch, got a message:\n%s", msg)
	default:
	}
}

func splitHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	var port int
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
		t.Fatal(err)
	}
	return host, port
}
