package store

import (
	"database/sql"
	"errors"

	"github.com/YousefProject/servicedesk/internal/model"
)

func (s *Store) AddComment(ticketID, authorID int64, body string, internal bool) (*model.Comment, error) {
	internalInt := 0
	if internal {
		internalInt = 1
	}
	res, err := s.db.Exec(
		`INSERT INTO comments (ticket_id, author_id, body, internal, created_at) VALUES (?, ?, ?, ?, ?)`,
		ticketID, authorID, body, internalInt, dbTime(now()),
	)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.CommentByID(id)
}

const commentCols = `c.id, c.ticket_id, c.author_id, c.body, c.internal, c.created_at, u.name, u.role`

func scanComment(sc scanner) (*model.Comment, error) {
	c := &model.Comment{}
	var internal int
	var created, role string
	if err := sc.Scan(&c.ID, &c.TicketID, &c.AuthorID, &c.Body, &internal, &created, &c.AuthorName, &role); err != nil {
		return nil, err
	}
	c.Internal = internal == 1
	c.CreatedAt = parseTime(created)
	c.AuthorRole = model.Role(role)
	return c, nil
}

func (s *Store) CommentByID(id int64) (*model.Comment, error) {
	row := s.db.QueryRow(
		`SELECT `+commentCols+` FROM comments c JOIN users u ON u.id = c.author_id WHERE c.id = ?`, id)
	c, err := scanComment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

// Comments devuelve los comentarios del ticket. Si includeInternal es false se
// omiten las notas internas (visión del solicitante).
func (s *Store) Comments(ticketID int64, includeInternal bool) ([]model.Comment, error) {
	q := `SELECT ` + commentCols + ` FROM comments c JOIN users u ON u.id = c.author_id
	      WHERE c.ticket_id = ?`
	if !includeInternal {
		q += ` AND c.internal = 0`
	}
	q += ` ORDER BY c.created_at ASC`
	rows, err := s.db.Query(q, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Comment{}
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (s *Store) AddAttachment(ticketID int64, commentID *int64, uploaderID int64, filename string, size int64, mime, storedPath string) (*model.Attachment, error) {
	res, err := s.db.Exec(
		`INSERT INTO attachments (ticket_id, comment_id, uploader_id, filename, size, mime, stored_path, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		ticketID, idVal(commentID), uploaderID, filename, size, mime, storedPath, dbTime(now()),
	)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.AttachmentByID(id)
}

func (s *Store) AttachmentByID(id int64) (*model.Attachment, error) {
	row := s.db.QueryRow(
		`SELECT a.id, a.ticket_id, a.comment_id, a.uploader_id, a.filename, a.size, a.mime, a.stored_path,
		        a.created_at, u.name
		   FROM attachments a JOIN users u ON u.id = a.uploader_id WHERE a.id = ?`, id)
	return scanAttachment(row)
}

func scanAttachment(sc scanner) (*model.Attachment, error) {
	a := &model.Attachment{}
	var commentID sql.NullInt64
	var created string
	err := sc.Scan(&a.ID, &a.TicketID, &commentID, &a.UploaderID, &a.Filename, &a.Size, &a.MIME,
		&a.StoredPath, &created, &a.UploaderName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if commentID.Valid {
		a.CommentID = &commentID.Int64
	}
	a.CreatedAt = parseTime(created)
	return a, nil
}

func (s *Store) Attachments(ticketID int64) ([]model.Attachment, error) {
	rows, err := s.db.Query(
		`SELECT a.id, a.ticket_id, a.comment_id, a.uploader_id, a.filename, a.size, a.mime, a.stored_path,
		        a.created_at, u.name
		   FROM attachments a JOIN users u ON u.id = a.uploader_id
		  WHERE a.ticket_id = ? ORDER BY a.created_at ASC`, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Attachment{}
	for rows.Next() {
		a, err := scanAttachment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (s *Store) AuditFor(entity string, entityID int64, limit int) ([]model.AuditEvent, error) {
	return s.auditList(`WHERE a.entity = ? AND a.entity_id = ?`, []any{entity, entityID}, limit)
}

func (s *Store) RecentAudit(limit int) ([]model.AuditEvent, error) {
	return s.auditList(``, nil, limit)
}
