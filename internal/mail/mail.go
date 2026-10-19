package mail

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"
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

// NewDynamic crea un remitente que consulta la configuración SMTP vigente en
// cada envío, de modo que los cambios guardados en la consola web se aplican
// sin reiniciar el servicio.
func NewDynamic(get func() config.SMTP, logger *slog.Logger) Sender {
	return &dynamicSender{get: get, logger: logger}
}

type dynamicSender struct {
	get    func() config.SMTP
	logger *slog.Logger
}

func (d *dynamicSender) Send(ctx context.Context, m Message) error {
	cfg := d.get()
	if !cfg.Enabled() {
		d.logger.Info("email (dry-run: SMTP sin configurar)",
			"to", strings.Join(m.To, ","),
			"subject", m.Subject,
		)
		return nil
	}
	return sendSMTP(ctx, cfg, m)
}

// New crea un remitente fijo con la configuración dada.
func New(cfg config.SMTP, logger *slog.Logger) Sender {
	if !cfg.Enabled() {
		logger.Warn("SMTP no configurado: los correos se registran pero no se envían")
		return LogSender{Logger: logger}
	}
	return &SMTPSender{cfg: cfg}
}

func (s *SMTPSender) Send(ctx context.Context, m Message) error {
	return sendSMTP(ctx, s.cfg, m)
}

// SendWith envía un mensaje con la configuración SMTP indicada, sin pasar por
// el remitente dinámico. Lo usa la consola para probar valores sin guardar.
func SendWith(ctx context.Context, cfg config.SMTP, m Message) error {
	if !cfg.Enabled() {
		return fmt.Errorf("SMTP sin configurar")
	}
	return sendSMTP(ctx, cfg, m)
}

func sendSMTP(ctx context.Context, cfg config.SMTP, m Message) error {
	if len(m.To) == 0 {
		return nil
	}
	headers := []string{
		"From: " + cfg.From,
		"To: " + strings.Join(m.To, ", "),
		"Subject: " + m.Subject,
		"Date: " + time.Now().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
	}
	body := strings.Join(headers, "\r\n") + "\r\n\r\n" + m.Body + "\r\n"

	addr := cfg.Addr()
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(30 * time.Second)
	}

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("smtp: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(deadline)

	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("smtp: %w", err)
	}
	defer c.Close()

	// STARTTLS se usa siempre que el servidor lo ofrezca; si la configuración
	// lo exige y no está disponible, el envío se aborta en lugar de caer a
	// una conexión en claro.
	hasStartTLS, _ := c.Extension("STARTTLS")
	if cfg.StartTLS && !hasStartTLS {
		return fmt.Errorf("smtp: el servidor no ofrece STARTTLS y la configuración lo exige")
	}
	tlsUsed := false
	if hasStartTLS {
		if err := c.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("smtp: STARTTLS: %w", err)
		}
		tlsUsed = true
	}

	if cfg.User != "" {
		if !tlsUsed && !isLoopback(host) {
			return fmt.Errorf("smtp: la autenticación requiere STARTTLS o un servidor local")
		}
		if err := c.Auth(smtp.PlainAuth("", cfg.User, cfg.Pass, host)); err != nil {
			return fmt.Errorf("smtp: autenticación: %w", err)
		}
	}

	if err := c.Mail(cfg.From); err != nil {
		return fmt.Errorf("smtp: remitente: %w", err)
	}
	for _, to := range m.To {
		if err := c.Rcpt(to); err != nil {
			return fmt.Errorf("smtp: destinatario %s: %w", to, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp: inicio del mensaje: %w", err)
	}
	if _, err := io.WriteString(w, body); err != nil {
		_ = w.Close()
		return fmt.Errorf("smtp: escribir mensaje: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp: finalizar mensaje: %w", err)
	}
	if err := c.Quit(); err != nil {
		return fmt.Errorf("smtp: %w", err)
	}
	return nil
}

func isLoopback(host string) bool {
	if host == "localhost" || host == "::1" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}
