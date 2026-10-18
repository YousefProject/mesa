package mail

import (
	"context"
	"fmt"
	"log/slog"
	"net/smtp"
	"strings"
	"time"

	"github.com/YousefProject/servicedesk/internal/config"
)

type Message struct {
	To      []string
	Subject string
	Body    string
}

type Sender interface {
	Send(ctx context.Context, m Message) error
}

// LogSender es el modo dry-run: registra el correo en lugar de enviarlo.
type LogSender struct{ Logger *slog.Logger }

func (l LogSender) Send(_ context.Context, m Message) error {
	l.Logger.Info("email (dry-run)",
		"to", strings.Join(m.To, ","),
		"subject", m.Subject,
		"body", m.Body,
	)
	return nil
}

type SMTPSender struct {
	cfg config.SMTP
}

func New(cfg config.SMTP, logger *slog.Logger) Sender {
	if !cfg.Enabled() {
		logger.Warn("SMTP no configurado: los correos se registran pero no se envían")
		return LogSender{Logger: logger}
	}
	return &SMTPSender{cfg: cfg}
}

func (s *SMTPSender) Send(ctx context.Context, m Message) error {
	if len(m.To) == 0 {
		return nil
	}
	var auth smtp.Auth
	if s.cfg.User != "" {
		auth = smtp.PlainAuth("", s.cfg.User, s.cfg.Pass, s.cfg.Host)
	}
	headers := []string{
		"From: " + s.cfg.From,
		"To: " + strings.Join(m.To, ", "),
		"Subject: " + m.Subject,
		"Date: " + time.Now().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
	}
	body := strings.Join(headers, "\r\n") + "\r\n\r\n" + m.Body + "\r\n"

	done := make(chan error, 1)
	go func() {
		done <- smtp.SendMail(s.cfg.Addr(), auth, s.cfg.From, m.To, []byte(body))
	}()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("smtp: %w", err)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
