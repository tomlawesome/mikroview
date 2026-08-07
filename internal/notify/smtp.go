// SPDX-License-Identifier: AGPL-3.0-only

package notify

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
)

// TLSMode selects how the connection to Host:Port is secured.
type TLSMode string

const (
	TLSNone     TLSMode = ""         // plaintext -- local relay only
	TLSStartTLS TLSMode = "starttls" // upgrade after connecting (typically port 587)
	TLSImplicit TLSMode = "implicit" // TLS from the first byte (typically port 465)
)

type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	TLSMode  TLSMode
	From     string
	To       []string
}

// SMTPNotifier sends a batch of newly-raised flags as one plain-text
// email through the operator's own external mail relay -- send-only, no
// inbound mailbox, no auth flows beyond the SMTP client credentials
// already in SMTPConfig.
type SMTPNotifier struct {
	cfg SMTPConfig
}

func NewSMTPNotifier(cfg SMTPConfig) *SMTPNotifier {
	return &SMTPNotifier{cfg: cfg}
}

func (n *SMTPNotifier) Send(batch []flags.Flag) error {
	if len(batch) == 0 {
		return nil
	}
	addr := fmt.Sprintf("%s:%d", n.cfg.Host, n.cfg.Port)

	client, err := n.dial(addr)
	if err != nil {
		return fmt.Errorf("notify/smtp: dial %s: %w", addr, err)
	}
	defer client.Close()

	if n.cfg.Username != "" {
		auth := smtp.PlainAuth("", n.cfg.Username, n.cfg.Password, n.cfg.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("notify/smtp: auth: %w", err)
		}
	}

	if err := client.Mail(n.cfg.From); err != nil {
		return fmt.Errorf("notify/smtp: MAIL FROM: %w", err)
	}
	for _, to := range n.cfg.To {
		if err := client.Rcpt(to); err != nil {
			return fmt.Errorf("notify/smtp: RCPT TO %s: %w", to, err)
		}
	}

	wc, err := client.Data()
	if err != nil {
		return fmt.Errorf("notify/smtp: DATA: %w", err)
	}
	if _, err := wc.Write([]byte(n.message(batch))); err != nil {
		wc.Close()
		return fmt.Errorf("notify/smtp: writing message: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("notify/smtp: closing message: %w", err)
	}

	return client.Quit()
}

// dial establishes the client connection for cfg.TLSMode -- StartTLS is
// requested only for TLSStartTLS; TLSImplicit needs a TLS connection
// established before smtp.NewClient ever sees it, since net/smtp itself
// has no concept of connecting over TLS from the first byte.
func (n *SMTPNotifier) dial(addr string) (*smtp.Client, error) {
	if n.cfg.TLSMode == TLSImplicit {
		conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: n.cfg.Host})
		if err != nil {
			return nil, err
		}
		return smtp.NewClient(conn, n.cfg.Host)
	}

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	client, err := smtp.NewClient(conn, n.cfg.Host)
	if err != nil {
		return nil, err
	}
	if n.cfg.TLSMode == TLSStartTLS {
		if err := client.StartTLS(&tls.Config{ServerName: n.cfg.Host}); err != nil {
			client.Close()
			return nil, err
		}
	}
	return client, nil
}

// message builds a minimal RFC 5322 email: one line per flag (type,
// target, detail, confidence, first-seen).
func (n *SMTPNotifier) message(batch []flags.Flag) string {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", n.cfg.From)
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(n.cfg.To, ", "))
	fmt.Fprintf(&b, "Subject: mikroview: %d new flag%s\r\n", len(batch), plural(len(batch)))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	b.WriteString("\r\n")
	for _, f := range batch {
		confidence := "unscored"
		if f.Confidence != nil {
			confidence = fmt.Sprintf("%d%%", *f.Confidence)
		}
		fmt.Fprintf(&b, "[%s] %s -- %s (confidence: %s, first seen %s)\r\n",
			f.Type, f.Target, f.Detail, confidence, f.FirstSeen.Format(time.RFC3339))
	}
	return b.String()
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
