package store

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/YousefProject/servicedesk/internal/model"
)

// Métodos de restauración usados por internal/backup. Preservan el ID del
// ticket (para mantener ref y carpeta data/tickets/<id>/), pero comentarios,
// adjuntos y auditoría se reinsertan con autoincremento para no colisionar
// con IDs de otra instancia.

// TicketIDs devuelve los IDs de todos los tickets, ordenados.
func (s *Store) TicketIDs() ([]int64, error) {
	rows, err := s.db.Query(`SELECT id FROM tickets ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// RestoreTicket inserta un ticket con ID explícito y adelanta la secuencia
// AUTOINCREMENT para que los tickets nuevos no colisionen.
func (s *Store) RestoreTicket(t *model.Ticket) error {
	_, err := s.db.Exec(
		`INSERT INTO tickets (id, ref, subject, description, status, priority, queue_id,
			requester_id, assignee_id, created_at, updated_at, due_response_at, due_resolution_at,
			first_response, resolved_at, closed_at, sla_breached, sla_notified)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		t.ID, t.Ref, t.Subject, t.Description, string(t.Status), string(t.Priority),
		idVal(t.QueueID), t.RequesterID, idVal(t.AssigneeID),
		dbTime(t.CreatedAt), dbTime(t.UpdatedAt), timeVal(t.DueResponse), timeVal(t.DueResolution),
		timeVal(t.FirstResponse), timeVal(t.ResolvedAt), timeVal(t.ClosedAt), boolInt(t.SLABreached),
	)
	if err != nil {
		return fmt.Errorf("restaurar ticket %d: %w", t.ID, err)
	}
	return s.bumpSequence("tickets", t.ID)
}

// RestoreComment inserta un comentario con autoincremento y devuelve su nuevo
// ID (para re-mapear comment_id de los adjuntos).
func (s *Store) RestoreComment(c *model.Comment) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO comments (ticket_id, author_id, body, internal, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		c.TicketID, c.AuthorID, c.Body, boolInt(c.Internal), dbTime(c.CreatedAt),
	)
	if err != nil {
		return 0, fmt.Errorf("restaurar comentario: %w", err)
	}
	return res.LastInsertId()
}

// RestoreAttachment inserta un adjunto con autoincremento.
func (s *Store) RestoreAttachment(a *model.Attachment) error {
	_, err := s.db.Exec(
		`INSERT INTO attachments (ticket_id, comment_id, uploader_id, filename, size, mime, stored_path, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.TicketID, idVal(a.CommentID), a.UploaderID, a.Filename, a.Size, a.MIME, a.StoredPath, dbTime(a.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("restaurar adjunto: %w", err)
	}
	return nil
}

// RestoreAudit inserta un evento de auditoría con autoincremento.
func (s *Store) RestoreAudit(e *model.AuditEvent) error {
	_, err := s.db.Exec(
		`INSERT INTO audit_events (actor_id, action, entity, entity_id, details, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		idVal(e.ActorID), e.Action, e.Entity, e.EntityID, e.Details, dbTime(e.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("restaurar auditoría: %w", err)
	}
	return nil
}

// DeleteTicket elimina un ticket y todo lo que cuelga de él (FK CASCADE).
func (s *Store) DeleteTicket(id int64) error {
	_, err := s.db.Exec(`DELETE FROM tickets WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("eliminar ticket %d: %w", id, err)
	}
	return nil
}

// QueueByID devuelve la cola con ese ID.
func (s *Store) QueueByID(id int64) (*model.Queue, error) {
	row := s.db.QueryRow(`SELECT id, name, description, created_at FROM queues WHERE id = ?`, id)
	var q model.Queue
	var created string
	if err := row.Scan(&q.ID, &q.Name, &q.Description, &created); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	q.CreatedAt = parseTime(created)
	return &q, nil
}

// QueueByName devuelve la cola con ese nombre (sin distinguir mayúsculas).
func (s *Store) QueueByName(name string) (*model.Queue, error) {
	row := s.db.QueryRow(`SELECT id, name, description, created_at FROM queues WHERE name = ? COLLATE NOCASE`, strings.TrimSpace(name))
	var q model.Queue
	var created string
	if err := row.Scan(&q.ID, &q.Name, &q.Description, &created); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	q.CreatedAt = parseTime(created)
	return &q, nil
}

// CreateUserWithID crea un usuario con ID explícito (para restauraciones).
// El usuario queda inactivo y con hash inutilizable: es un marcador de
// posición para conservar las referencias del backup, no una cuenta real.
func (s *Store) CreateUserWithID(id int64, email, name string, role model.Role, passwordHash string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	_, err := s.db.Exec(
		`INSERT INTO users (id, email, name, role, password_hash, active, created_at)
		 VALUES (?, ?, ?, ?, ?, 0, ?)`,
		id, email, name, string(role), passwordHash, dbTime(now()),
	)
	if err != nil {
		return fmt.Errorf("crear usuario placeholder %d: %w", id, err)
	}
	return s.bumpSequence("users", id)
}

// bumpSequence adelanta la secuencia AUTOINCREMENT de una tabla para que los
// próximos INSERT con ID automático no colisionen con IDs restaurados.
func (s *Store) bumpSequence(table string, id int64) error {
	_, err := s.db.Exec(
		`INSERT INTO sqlite_sequence (name, seq) SELECT ?, ?
		 WHERE NOT EXISTS (SELECT 1 FROM sqlite_sequence WHERE name = ?)`,
		table, id, table,
	)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`UPDATE sqlite_sequence SET seq = MAX(seq, ?) WHERE name = ?`,
		id, table,
	)
	return err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
