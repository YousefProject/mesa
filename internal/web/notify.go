package web

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/YousefProject/servicedesk/internal/mail"
	"github.com/YousefProject/servicedesk/internal/model"
)

// send envía un correo a los destinatarios (deduplicados) sin bloquear la petición.
func (s *Server) send(to []string, subject, body string) {
	seen := map[string]bool{}
	recipients := make([]string, 0, len(to))
	for _, addr := range to {
		addr = strings.ToLower(strings.TrimSpace(addr))
		if addr == "" || seen[addr] {
			continue
		}
		seen[addr] = true
		recipients = append(recipients, addr)
	}
	if len(recipients) == 0 {
		return
	}
	msg := mail.Message{To: recipients, Subject: subject, Body: body}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := s.mail.Send(ctx, msg); err != nil {
			s.logger.Warn("envío de correo fallido", "to", strings.Join(recipients, ","), "err", err)
		}
	}()
}

func (s *Server) ticketURL(id int64) string {
	return fmt.Sprintf("%s/tickets/%d", strings.TrimRight(s.cfg.BaseURL, "/"), id)
}

func (s *Server) notifyTicketCreated(t *model.Ticket) {
	staff, err := s.store.Staff()
	if err != nil {
		s.logger.Warn("staff para aviso", "err", err)
		return
	}
	addrs := make([]string, 0, len(staff)+1)
	for _, u := range staff {
		addrs = append(addrs, u.Email)
	}
	if t.RequesterMail != "" {
		addrs = append(addrs, t.RequesterMail)
	}
	body := fmt.Sprintf(`Nuevo ticket %s

Asunto:      %s
Prioridad:   %s
Cola:        %s
Solicitante: %s
Vence (SLA): %s

%s

%s
`, t.Ref, t.Subject, t.Priority, dash(t.QueueName), t.RequesterName,
		dueText(t.DueResolution), t.Description, s.ticketURL(t.ID))
	s.send(addrs, "["+t.Ref+"] nuevo ticket: "+t.Subject, body)
}

func (s *Server) notifyComment(t *model.Ticket, c *model.Comment) {
	if c.Internal {
		return
	}
	addrs := []string{}
	if t.RequesterMail != "" {
		addrs = append(addrs, t.RequesterMail)
	}
	if t.AssigneeID != nil {
		if a, err := s.store.UserByID(*t.AssigneeID); err == nil {
			addrs = append(addrs, a.Email)
		}
	} else {
		staff, _ := s.store.Staff()
		for _, u := range staff {
			addrs = append(addrs, u.Email)
		}
	}
	body := fmt.Sprintf("%s escribió en %s:\n\n%s\n\n%s\n",
		c.AuthorName, t.Ref, c.Body, s.ticketURL(t.ID))
	s.send(addrs, "["+t.Ref+"] nuevo comentario", body)
}

func (s *Server) notifyUsers(users []model.User, subject, body string) {
	addrs := make([]string, 0, len(users))
	for _, u := range users {
		addrs = append(addrs, u.Email)
	}
	s.send(addrs, subject, body)
}

func (s *Server) assigneeEmails(t *model.Ticket) []string {
	out := []string{}
	if t.AssigneeID != nil {
		if a, err := s.store.UserByID(*t.AssigneeID); err == nil {
			out = append(out, a.Email)
		}
		return out
	}
	staff, _ := s.store.Staff()
	for _, u := range staff {
		out = append(out, u.Email)
	}
	return out
}

func (s *Server) notifyBreach(t *model.Ticket) {
	body := fmt.Sprintf(`El ticket %s ha incumplido su SLA.

Asunto:    %s
Prioridad: %s
Asignado:  %s
Vencía:    %s

%s
`, t.Ref, t.Subject, t.Priority, dash(t.AssigneeName), dueText(t.DueResolution), s.ticketURL(t.ID))
	s.send(s.assigneeEmails(t), "["+t.Ref+"] SLA incumplido", body)
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func dueText(t *time.Time) string {
	if t == nil {
		return "—"
	}
	return t.Local().Format("02 Jan 2006 15:04")
}
