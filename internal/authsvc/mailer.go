package authsvc

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type SMTPMailer struct {
	host     string
	port     string
	user     string
	password string
	from     string
}

func NewSMTPMailer(host, port, user, password, from string) *SMTPMailer {
	if from == "" {
		from = user
	}
	return &SMTPMailer{host: host, port: port, user: user, password: password, from: from}
}

func (m *SMTPMailer) implicitTLS() bool { return m.port == "465" }

func (m *SMTPMailer) connect(ctx context.Context) (*smtp.Client, error) {
	addr := net.JoinHostPort(m.host, m.port)
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	tlsConfig := &tls.Config{ServerName: m.host, MinVersion: tls.VersionTLS12}

	var conn net.Conn
	var err error
	if m.implicitTLS() {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsConfig)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return nil, fmt.Errorf("smtp dial: %w", err)
	}
	deadline := time.Now().Add(30 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)

	client, err := smtp.NewClient(conn, m.host)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("smtp handshake: %w", err)
	}

	if !m.implicitTLS() {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			_ = client.Close()
			return nil, errors.New("smtp: server offers no STARTTLS")
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("smtp starttls: %w", err)
		}
	}
	return client, nil
}

func (m *SMTPMailer) Probe(ctx context.Context) error {
	client, err := m.connect(ctx)
	if err != nil {
		return err
	}
	defer client.Close()
	return client.Quit()
}

func (m *SMTPMailer) Send(ctx context.Context, msg Mail) error {
	client, err := m.connect(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	if m.user != "" {
		if err := client.Auth(smtp.PlainAuth("", m.user, m.password, m.host)); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := client.Mail(m.from); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	if err := client.Rcpt(msg.To); err != nil {
		return fmt.Errorf("smtp rcpt: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := writer.Write(m.message(msg)); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("smtp close data: %w", err)
	}
	return client.Quit()
}

func (m *SMTPMailer) message(msg Mail) []byte {
	var b strings.Builder
	header := func(name, value string) {
		b.WriteString(name + ": " + value + "\r\n")
	}
	domain := m.from[strings.LastIndex(m.from, "@")+1:]
	header("From", m.from)
	header("To", msg.To)
	header("Subject", mime.QEncoding.Encode("utf-8", msg.Subject))
	header("Date", time.Now().Format(time.RFC1123Z))
	header("Message-ID", fmt.Sprintf("<%s@%s>", uuid.NewString(), domain))
	header("MIME-Version", "1.0")
	header("Content-Type", `text/plain; charset="utf-8"`)
	header("Content-Transfer-Encoding", "base64")
	b.WriteString("\r\n")

	encoded := base64.StdEncoding.EncodeToString([]byte(msg.Body))
	for len(encoded) > 76 {
		b.WriteString(encoded[:76] + "\r\n")
		encoded = encoded[76:]
	}
	b.WriteString(encoded + "\r\n")
	return []byte(b.String())
}

type LogMailer struct {
	logger *slog.Logger
}

func NewLogMailer(logger *slog.Logger) *LogMailer {
	if logger == nil {
		logger = slog.Default()
	}
	return &LogMailer{logger: logger}
}

func (m *LogMailer) Send(_ context.Context, msg Mail) error {
	m.logger.Warn("mail not sent: SMTP is not configured", "to", msg.To, "subject", msg.Subject, "code", msg.Code)
	return nil
}

type CapturedMail struct {
	To      string    `json:"to"`
	Subject string    `json:"subject"`
	Code    string    `json:"code"`
	SentAt  time.Time `json:"sent_at"`
}

type CaptureMailer struct {
	next Mailer
	mu   sync.Mutex
	last map[string]CapturedMail
}

func NewCaptureMailer(next Mailer) *CaptureMailer {
	return &CaptureMailer{next: next, last: map[string]CapturedMail{}}
}

func (m *CaptureMailer) Send(ctx context.Context, msg Mail) error {
	m.mu.Lock()
	m.last[strings.ToLower(msg.To)] = CapturedMail{To: msg.To, Subject: msg.Subject, Code: msg.Code, SentAt: time.Now()}
	m.mu.Unlock()
	if m.next == nil {
		return nil
	}
	return m.next.Send(ctx, msg)
}

func (m *CaptureMailer) Last(to string) (CapturedMail, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	captured, ok := m.last[strings.ToLower(strings.TrimSpace(to))]
	return captured, ok
}
