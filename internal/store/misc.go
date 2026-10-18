package store

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/YousefProject/servicedesk/internal/model"
)

func (s *Store) auditList(where string, args []any, limit int) ([]model.AuditEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	var q strings.Builder
	q.WriteString(`SELECT a.id, a.actor_id, COALESCE(u.name, 'sistema'), a.action, a.entity, a.entity_id,
	                       a.details, a.created_at
	                  FROM audit_events a LEFT JOIN users u ON u.id = a.actor_id `)
	q.WriteString(where)
	q.WriteString(` ORDER BY a.created_at DESC, a.id DESC LIMIT ?`)
	args = append(args, limit)
	rows, err := s.db.Query(q.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.AuditEvent{}
	for rows.Next() {
		e := model.AuditEvent{}
		var actorID sql.NullInt64
		var created string
		if err := rows.Scan(&e.ID, &actorID, &e.ActorName, &e.Action, &e.Entity, &e.EntityID, &e.Details, &created); err != nil {
			return nil, err
		}
		if actorID.Valid {
			e.ActorID = &actorID.Int64
		}
		e.CreatedAt = parseTime(created)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) Queues() ([]model.Queue, error) {
	rows, err := s.db.Query(
		`SELECT q.id, q.name, q.description, q.created_at,
		        (SELECT COUNT(*) FROM tickets t WHERE t.queue_id = q.id AND t.status NOT IN ('resolved','closed'))
		   FROM queues q ORDER BY q.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Queue{}
	for rows.Next() {
		var q model.Queue
		var created string
		if err := rows.Scan(&q.ID, &q.Name, &q.Description, &created, &q.TicketCount); err != nil {
			return nil, err
		}
		q.CreatedAt = parseTime(created)
		out = append(out, q)
	}
	return out, rows.Err()
}

func (s *Store) CreateQueue(name, description string) (*model.Queue, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("el nombre de la cola es obligatorio")
	}
	res, err := s.db.Exec(`INSERT INTO queues (name, description, created_at) VALUES (?, ?, ?)`,
		name, strings.TrimSpace(description), dbTime(now()))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, errors.New("esa cola ya existe")
		}
		return nil, err
	}
	id, _ := res.LastInsertId()
	queues, err := s.Queues()
	if err != nil {
		return nil, err
	}
	for _, q := range queues {
		if q.ID == id {
			return &q, nil
		}
	}
	return nil, ErrNotFound
}

func (s *Store) SLAPolicies() ([]model.SLAPolicy, error) {
	rows, err := s.db.Query(`SELECT priority, response_minutes, resolution_minutes FROM sla_policies`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.SLAPolicy{}
	for rows.Next() {
		var p model.SLAPolicy
		var prio string
		if err := rows.Scan(&prio, &p.ResponseMinutes, &p.ResolutionMinutes); err != nil {
			return nil, err
		}
		p.Priority = model.Priority(prio)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) SLAPolicy(p model.Priority) (model.SLAPolicy, error) {
	var out model.SLAPolicy
	var prio string
	err := s.db.QueryRow(
		`SELECT priority, response_minutes, resolution_minutes FROM sla_policies WHERE priority = ?`,
		string(p),
	).Scan(&prio, &out.ResponseMinutes, &out.ResolutionMinutes)
	if errors.Is(err, sql.ErrNoRows) {
		return model.SLAPolicy{Priority: p, ResponseMinutes: 240, ResolutionMinutes: 1440}, nil
	}
	if err != nil {
		return model.SLAPolicy{}, err
	}
	out.Priority = model.Priority(prio)
	return out, nil
}

func (s *Store) UpdateSLAPolicy(p model.SLAPolicy) error {
	_, err := s.db.Exec(
		`INSERT INTO sla_policies (priority, response_minutes, resolution_minutes) VALUES (?, ?, ?)
		 ON CONFLICT(priority) DO UPDATE SET response_minutes = excluded.response_minutes,
		                                     resolution_minutes = excluded.resolution_minutes`,
		string(p.Priority), p.ResponseMinutes, p.ResolutionMinutes,
	)
	return err
}

func (s *Store) Dashboard() (*model.Dashboard, error) {
	d := &model.Dashboard{
		ByStatus:   map[model.Status]int{},
		ByPriority: map[model.Priority]int{},
	}
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM tickets WHERE status NOT IN ('resolved','closed')`).Scan(&d.OpenTotal); err != nil {
		return nil, err
	}
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM tickets WHERE status NOT IN ('resolved','closed') AND assignee_id IS NULL`).Scan(&d.Unassigned); err != nil {
		return nil, err
	}
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM tickets WHERE status NOT IN ('resolved','closed')
		   AND ((first_response IS NULL AND due_response_at IS NOT NULL AND due_response_at < ?)
		     OR (due_resolution_at IS NOT NULL AND due_resolution_at < ?))`,
		dbTime(now()), dbTime(now())).Scan(&d.Breached); err != nil {
		return nil, err
	}
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM tickets WHERE resolved_at IS NOT NULL AND resolved_at >= ?`,
		dbTime(now().Truncate(24*3600*1000000000))).Scan(&d.ResolvedToday); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT status, COUNT(*) FROM tickets GROUP BY status`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			rows.Close()
			return nil, err
		}
		d.ByStatus[model.Status(st)] = n
	}
	rows.Close()

	rows, err = s.db.Query(`SELECT priority, COUNT(*) FROM tickets WHERE status NOT IN ('resolved','closed') GROUP BY priority`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p string
		var n int
		if err := rows.Scan(&p, &n); err != nil {
			rows.Close()
			return nil, err
		}
		d.ByPriority[model.Priority(p)] = n
	}
	rows.Close()

	rows, err = s.db.Query(
		`SELECT COALESCE(q.name, 'sin cola'), COUNT(t.id)
		   FROM tickets t LEFT JOIN queues q ON q.id = t.queue_id
		  WHERE t.status NOT IN ('resolved','closed') GROUP BY q.name ORDER BY COUNT(*) DESC`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var qc model.QueueCount
		if err := rows.Scan(&qc.Name, &qc.Count); err != nil {
			rows.Close()
			return nil, err
		}
		d.ByQueue = append(d.ByQueue, qc)
	}
	rows.Close()

	rows, err = s.db.Query(`
		SELECT u.name,
		       COALESCE(SUM(CASE WHEN t.status NOT IN ('resolved','closed') THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN t.status NOT IN ('resolved','closed')
		                         AND ((t.first_response IS NULL AND t.due_response_at IS NOT NULL AND t.due_response_at < ?)
		                           OR (t.due_resolution_at IS NOT NULL AND t.due_resolution_at < ?)) THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN t.resolved_at IS NOT NULL AND t.resolved_at >= ? THEN 1 ELSE 0 END), 0)
		  FROM users u
		  LEFT JOIN tickets t ON t.assignee_id = u.id
		 WHERE u.role IN ('admin','agent')
		 GROUP BY u.id ORDER BY 2 DESC, u.name`,
		dbTime(now()), dbTime(now()), dbTime(now().AddDate(0, 0, -7)))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var a model.AgentLoad
		if err := rows.Scan(&a.Name, &a.Open, &a.Breached, &a.Resolved7d); err != nil {
			rows.Close()
			return nil, err
		}
		d.ByAgent = append(d.ByAgent, a)
	}
	rows.Close()

	var avg sql.NullFloat64
	if err := s.db.QueryRow(
		`SELECT AVG((julianday(resolved_at) - julianday(created_at)) * 1440)
		   FROM tickets WHERE resolved_at IS NOT NULL`).Scan(&avg); err != nil {
		return nil, err
	}
	d.AvgResolveMin = avg.Float64
	return d, nil
}
