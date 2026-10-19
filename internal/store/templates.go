package store

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/YousefProject/servicedesk/internal/model"
)

const templateCols = `t.id, t.name, t.category, t.subject, t.description, t.priority,
	t.queue_id, t.active, t.created_at, COALESCE(q.name, '')`

const templateJoins = `FROM ticket_templates t
	LEFT JOIN queues q ON q.id = t.queue_id`

func scanTemplate(sc scanner) (*model.TicketTemplate, error) {
	t := &model.TicketTemplate{}
	var priority, created string
	var queueID sql.NullInt64
	var active int
	err := sc.Scan(&t.ID, &t.Name, &t.Category, &t.Subject, &t.Description, &priority,
		&queueID, &active, &created, &t.QueueName)
	if err != nil {
		return nil, err
	}
	t.Priority = model.Priority(priority)
	if queueID.Valid {
		t.QueueID = &queueID.Int64
	}
	t.Active = active == 1
	t.CreatedAt = parseTime(created)
	return t, nil
}

// NewTemplate son los campos editables de una plantilla.
type NewTemplate struct {
	Name        string
	Category    string
	Subject     string
	Description string
	Priority    model.Priority
	QueueID     *int64
	Active      bool
}

// Templates lista las plantillas. Si includeInactive es false, solo las activas.
func (s *Store) Templates(includeInactive bool) ([]model.TicketTemplate, error) {
	q := `SELECT ` + templateCols + ` ` + templateJoins
	if !includeInactive {
		q += ` WHERE t.active = 1`
	}
	q += ` ORDER BY t.category, t.name`
	rows, err := s.db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.TicketTemplate{}
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (s *Store) TemplateByID(id int64) (*model.TicketTemplate, error) {
	row := s.db.QueryRow(`SELECT `+templateCols+` `+templateJoins+` WHERE t.id = ?`, id)
	t, err := scanTemplate(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return t, err
}

func (s *Store) CreateTemplate(in NewTemplate) (*model.TicketTemplate, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, errors.New("el nombre de la plantilla es obligatorio")
	}
	if !in.Priority.Valid() {
		in.Priority = model.PriorityNormal
	}
	res, err := s.db.Exec(
		`INSERT INTO ticket_templates (name, category, subject, description, priority, queue_id, active, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		name, strings.TrimSpace(in.Category), strings.TrimSpace(in.Subject), strings.TrimSpace(in.Description),
		string(in.Priority), idVal(in.QueueID), boolInt(in.Active), dbTime(now()),
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, errors.New("ya existe una plantilla con ese nombre")
		}
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.TemplateByID(id)
}

func (s *Store) UpdateTemplate(id int64, in NewTemplate) error {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return errors.New("el nombre de la plantilla es obligatorio")
	}
	if !in.Priority.Valid() {
		in.Priority = model.PriorityNormal
	}
	_, err := s.db.Exec(
		`UPDATE ticket_templates
		    SET name = ?, category = ?, subject = ?, description = ?, priority = ?, queue_id = ?, active = ?
		  WHERE id = ?`,
		name, strings.TrimSpace(in.Category), strings.TrimSpace(in.Subject), strings.TrimSpace(in.Description),
		string(in.Priority), idVal(in.QueueID), boolInt(in.Active), id,
	)
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return errors.New("ya existe una plantilla con ese nombre")
	}
	return err
}

func (s *Store) DeleteTemplate(id int64) error {
	_, err := s.db.Exec(`DELETE FROM ticket_templates WHERE id = ?`, id)
	return err
}
