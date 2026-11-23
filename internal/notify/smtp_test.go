// SPDX-License-Identifier: AGPL-3.0-only

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
	// failRcptTo (#1361), lowercased: when non-empty, a RCPT TO for this
	// exact address is refused with 550 instead of accepted -- the one
	// realistic way a real relay fails SendNotice/Send partway through,
	// exercised so that branch is not merely read, only tested.
	failRcptTo string
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
			if s.failRcptTo != "" && strings.Contains(strings.ToLower(trimmed), s.failRcptTo) {
				fmt.Fprintf(conn, "550 no such user\r\n")
				continue
			}
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
		if !strings.Contains(msg, "Subject: MikroView: 1 new flag") {
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

// #1361: the router-backup switch's own email, a one-off notice rather
// than a batch of flags -- same relay, a different composer.
func TestSMTPNotifierSendsNotice(t *testing.T) {
	server := startFakeSMTPServer(t)
	host, port := splitHostPort(t, server.addr)
	n := NewSMTPNotifier(SMTPConfig{
		Host: host, Port: port,
		From: "mikroview@example.com",
		To:   []string{"admins@example.com"},
	})

	if err := n.SendNotice("MikroView: router backups opened", "Opened by alice on port 47022."); err != nil {
		t.Fatalf("SendNotice returned an error: %v", err)
	}

	select {
	case msg := <-server.received:
		if !strings.Contains(msg, "Subject: MikroView: router backups opened") {
			t.Errorf("expected the given subject, got:\n%s", msg)
		}
		if !strings.Contains(msg, "Opened by alice on port 47022.") {
			t.Errorf("expected the given body, got:\n%s", msg)
		}
	default:
		t.Fatal("fake server never received a message")
	}
}

func TestSMTPNotifierSendNoticeSkipsWhenNoRecipients(t *testing.T) {
	server := startFakeSMTPServer(t)
	host, port := splitHostPort(t, server.addr)
	n := NewSMTPNotifier(SMTPConfig{Host: host, Port: port, From: "a@example.com"})

	if err := n.SendNotice("subject", "body"); err != nil {
		t.Fatalf("expected no error with no recipients, got %v", err)
	}
	select {
	case msg := <-server.received:
		t.Fatalf("expected no connection attempt with no recipients, got a message:\n%s", msg)
	default:
	}
}

// A relay that cannot be reached at all: SendNotice's own dial failure,
// wrapped rather than returned bare so the caller's log line names what
// failed.
func TestSMTPNotifierSendNoticeWrapsADialFailure(t *testing.T) {
	// A listener bound then closed immediately: its port is real but
	// nothing answers on it, the cheapest reliable way to force a dial
	// failure without depending on an unroutable address being refused
	// quickly on every CI host.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	host, port := splitHostPort(t, ln.Addr().String())
	ln.Close()

	n := NewSMTPNotifier(SMTPConfig{Host: host, Port: port, From: "a@example.com", To: []string{"b@example.com"}})
	err = n.SendNotice("subject", "body")
	if err == nil {
		t.Fatal("expected an error dialing a closed port, got none")
	}
	if !strings.Contains(err.Error(), "dial") {
		t.Errorf("error %q does not say dial", err)
	}
}

// A username configured but no relay willing to accept AUTH: the fake
// server has no AUTH support at all, so it answers 500 the same way a
// real relay would to a mechanism it doesn't offer -- SendNotice's own
// auth-failure branch, wrapped.
func TestSMTPNotifierSendNoticeWrapsAnAuthFailure(t *testing.T) {
	server := startFakeSMTPServer(t)
	host, port := splitHostPort(t, server.addr)
	n := NewSMTPNotifier(SMTPConfig{
		Host: host, Port: port,
		From: "a@example.com", To: []string{"b@example.com"},
		Username: "someone", Password: "wrong",
	})

	err := n.SendNotice("subject", "body")
	if err == nil {
		t.Fatal("expected an error from a relay that refuses AUTH, got none")
	}
	if !strings.Contains(err.Error(), "auth") {
		t.Errorf("error %q does not say auth", err)
	}
}

// A recipient the relay refuses partway through the transaction:
// SendNotice's own RCPT TO failure branch, wrapped with the address that
// was refused.
func TestSMTPNotifierSendNoticeWrapsARcptFailure(t *testing.T) {
	server := startFakeSMTPServer(t)
	server.failRcptTo = "bad@example.com"
	host, port := splitHostPort(t, server.addr)
	n := NewSMTPNotifier(SMTPConfig{Host: host, Port: port, From: "a@example.com", To: []string{"bad@example.com"}})

	err := n.SendNotice("subject", "body")
	if err == nil {
		t.Fatal("expected an error from a refused recipient, got none")
	}
	if !strings.Contains(err.Error(), "RCPT TO") || !strings.Contains(err.Error(), "bad@example.com") {
		t.Errorf("error %q does not name the refused recipient", err)
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
