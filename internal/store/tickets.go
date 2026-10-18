package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/YousefProject/servicedesk/internal/model"
)

const ticketCols = `t.id, t.ref, t.subject, t.description, t.status, t.priority,
	t.queue_id, t.requester_id, t.assignee_id, t.created_at, t.updated_at,
	t.due_response_at, t.due_resolution_at, t.first_response, t.resolved_at, t.closed_at,
	t.sla_breached, COALESCE(q.name, ''), u.name, u.email, COALESCE(a.name, '')`

const ticketJoins = `FROM tickets t
	LEFT JOIN queues q ON q.id = t.queue_id
	JOIN users u ON u.id = t.requester_id
	LEFT JOIN users a ON a.id = t.assignee_id`

func scanTicket(sc scanner) (*model.Ticket, error) {
	t := &model.Ticket{}
	var status, priority, created, updated string
	var queueID, assigneeID sql.NullInt64
	var dueResp, dueRes, firstResp, resolved, closed sql.NullString
	var breached int
	err := sc.Scan(&t.ID, &t.Ref, &t.Subject, &t.Description, &status, &priority,
		&queueID, &t.RequesterID, &assigneeID, &created, &updated,
		&dueResp, &dueRes, &firstResp, &resolved, &closed,
		&breached, &t.QueueName, &t.RequesterName, &t.RequesterMail, &t.AssigneeName)
	if err != nil {
		return nil, err
	}
	t.Status = model.Status(status)
	t.Priority = model.Priority(priority)
	if queueID.Valid {
		t.QueueID = &queueID.Int64
	}
	if assigneeID.Valid {
		t.AssigneeID = &assigneeID.Int64
	}
	t.CreatedAt = parseTime(created)
	t.UpdatedAt = parseTime(updated)
	t.DueResponse = nullTime(dueResp)
	t.DueResolution = nullTime(dueRes)
	t.FirstResponse = nullTime(firstResp)
	t.ResolvedAt = nullTime(resolved)
	t.ClosedAt = nullTime(closed)
	t.SLABreached = breached == 1
	return t, nil
}

type NewTicket struct {
	Subject       string
	Description   string
	Priority      model.Priority
	QueueID       *int64
	RequesterID   int64
	AssigneeID    *int64
	DueResponse   time.Time
	DueResolution time.Time
}

func (s *Store) CreateTicket(in NewTicket) (*model.Ticket, error) {
	var id int64
	var status = model.StatusNew
	if in.AssigneeID != nil {
		status = model.StatusAssigned
	}
	err := s.tx(func(tx *sql.Tx) error {
		res, err := tx.Exec(
			`INSERT INTO tickets (ref, subject, description, status, priority, queue_id, requester_id, assignee_id,
				created_at, updated_at, due_response_at, due_resolution_at)
			 VALUES ('', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			strings.TrimSpace(in.Subject), strings.TrimSpace(in.Description), string(status), string(in.Priority),
			idVal(in.QueueID), in.RequesterID, idVal(in.AssigneeID),
			dbTime(now()), dbTime(now()), dbTime(in.DueResponse), dbTime(in.DueResolution),
		)
		if err != nil {
			return err
		}
		id, err = res.LastInsertId()
		if err != nil {
			return err
		}
		ref := fmt.Sprintf("SD-%05d", id)
		_, err = tx.Exec(`UPDATE tickets SET ref = ? WHERE id = ?`, ref, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.TicketByID(id)
}

func (s *Store) TicketByID(id int64) (*model.Ticket, error) {
	row := s.db.QueryRow(`SELECT `+ticketCols+` `+ticketJoins+` WHERE t.id = ?`, id)
	t, err := scanTicket(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return t, err
}

type TicketFilter struct {
	Status      model.Status
	Priority    model.Priority
	QueueID     *int64
	AssigneeID  *int64
	RequesterID *int64
	Unassigned  bool
	Breached    bool
	Query       string
	Limit       int
	Offset      int
}

func (f TicketFilter) where() (string, []any) {
	var conds []string
	var args []any
	if f.Status != "" {
		conds = append(conds, "t.status = ?")
		args = append(args, string(f.Status))
	}
	if f.Priority != "" {
		conds = append(conds, "t.priority = ?")
		args = append(args, string(f.Priority))
	}
	if f.QueueID != nil {
		conds = append(conds, "t.queue_id = ?")
		args = append(args, *f.QueueID)
	}
	if f.AssigneeID != nil {
		conds = append(conds, "t.assignee_id = ?")
		args = append(args, *f.AssigneeID)
	}
	if f.RequesterID != nil {
		conds = append(conds, "t.requester_id = ?")
		args = append(args, *f.RequesterID)
	}
	if f.Unassigned {
		conds = append(conds, "t.assignee_id IS NULL")
	}
	if f.Breached {
		conds = append(conds, "(t.sla_breached = 1 OR (t.status NOT IN ('resolved','closed') AND ("+
			"(t.first_response IS NULL AND t.due_response_at IS NOT NULL AND t.due_response_at < ?) OR "+
			"(t.due_resolution_at IS NOT NULL AND t.due_resolution_at < ?))))")
		args = append(args, dbTime(now()), dbTime(now()))
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		like := "%" + strings.ToLower(q) + "%"
		conds = append(conds, "(LOWER(t.subject) LIKE ? OR LOWER(t.description) LIKE ? OR LOWER(t.ref) LIKE ?)")
		args = append(args, like, like, like)
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

func (s *Store) ListTickets(f TicketFilter) ([]model.Ticket, error) {
	where, args := f.where()
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	q := `SELECT ` + ticketCols + ` ` + ticketJoins + where + `
		ORDER BY CASE t.status WHEN 'new' THEN 0 WHEN 'assigned' THEN 1 WHEN 'in_progress' THEN 2
		         WHEN 'waiting' THEN 3 WHEN 'resolved' THEN 4 ELSE 5 END,
		         CASE t.priority WHEN 'urgent' THEN 0 WHEN 'high' THEN 1 WHEN 'normal' THEN 2 ELSE 3 END,
		         t.updated_at DESC LIMIT ? OFFSET ?`
	args = append(args, limit, f.Offset)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Ticket{}
	for rows.Next() {
		t, err := scanTicket(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (s *Store) CountTickets(f TicketFilter) (int, error) {
	where, args := f.where()
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) `+ticketJoins+where, args...).Scan(&n)
	return n, err
}

func (s *Store) touch(id int64) error {
	_, err := s.db.Exec(`UPDATE tickets SET updated_at = ? WHERE id = ?`, dbTime(now()), id)
	return err
}

func (s *Store) SetStatus(id int64, status model.Status) error {
	t, err := s.TicketByID(id)
	if err != nil {
		return err
	}
	resolved, closed := t.ResolvedAt, t.ClosedAt
	switch status {
	case model.StatusResolved:
		if resolved == nil {
			at := now()
			resolved = &at
		}
	case model.StatusClosed:
		if resolved == nil {
			at := now()
			resolved = &at
		}
		if closed == nil {
			at := now()
			closed = &at
		}
	default:
		resolved, closed = nil, nil
	}
	_, err = s.db.Exec(
		`UPDATE tickets SET status = ?, resolved_at = ?, closed_at = ?, updated_at = ? WHERE id = ?`,
		string(status), timeVal(resolved), timeVal(closed), dbTime(now()), id,
	)
	return err
}

func (s *Store) SetPriority(id int64, p model.Priority, dueResponse, dueResolution time.Time) error {
	_, err := s.db.Exec(
		`UPDATE tickets SET priority = ?, due_response_at = ?, due_resolution_at = ?, updated_at = ? WHERE id = ?`,
		string(p), dbTime(dueResponse), dbTime(dueResolution), dbTime(now()), id,
	)
	return err
}

func (s *Store) SetAssignee(id int64, assignee *int64) error {
	_, err := s.db.Exec(
		`UPDATE tickets SET assignee_id = ?,
			status = CASE WHEN ? IS NOT NULL AND status = 'new' THEN 'assigned'
			              WHEN ? IS NULL AND status = 'assigned' THEN 'new' ELSE status END,
			updated_at = ?
		 WHERE id = ?`,
		idVal(assignee), idVal(assignee), idVal(assignee), dbTime(now()), id,
	)
	return err
}

func (s *Store) SetQueue(id int64, queue *int64) error {
	_, err := s.db.Exec(`UPDATE tickets SET queue_id = ?, updated_at = ? WHERE id = ?`,
		idVal(queue), dbTime(now()), id)
	return err
}

func (s *Store) MarkFirstResponse(id int64) error {
	_, err := s.db.Exec(
		`UPDATE tickets SET first_response = COALESCE(first_response, ?) WHERE id = ?`,
		dbTime(now()), id,
	)
	return err
}

// BreachedTickets devuelve los tickets abiertos que han superado su SLA y aún no
// habían sido marcados, para notificar una sola vez.
func (s *Store) BreachedTickets() ([]model.Ticket, error) {
	rows, err := s.db.Query(`SELECT `+ticketCols+` `+ticketJoins+`
		WHERE t.status NOT IN ('resolved','closed')
		  AND (t.sla_breached = 0 OR t.sla_notified = 0)
		  AND ((t.first_response IS NULL AND t.due_response_at IS NOT NULL AND t.due_response_at < ?)
		    OR (t.due_resolution_at IS NOT NULL AND t.due_resolution_at < ?))`,
		dbTime(now()), dbTime(now()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Ticket{}
	for rows.Next() {
		t, err := scanTicket(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (s *Store) MarkSLABreached(id int64) error {
	_, err := s.db.Exec(`UPDATE tickets SET sla_breached = 1, sla_notified = 1 WHERE id = ?`, id)
	return err
}
