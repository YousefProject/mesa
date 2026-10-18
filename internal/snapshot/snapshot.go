// Package snapshot serializa el estado completo de cada ticket a JSON en
// data/tickets/<id>/snapshot.json. La base de datos sigue siendo la fuente de
// verdad; el snapshot es un espejo auditable y portable: sirve para migrar a
// otra base de datos, auditar sin tocar la BD y respaldar por ticket.
package snapshot

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/YousefProject/servicedesk/internal/model"
)

// TicketSnapshot es el documento JSON por ticket.
type TicketSnapshot struct {
	SnapshotVersion int                `json:"snapshot_version"`
	Ticket          *model.Ticket      `json:"ticket"`
	Comments        []model.Comment    `json:"comments"`
	Attachments     []model.Attachment `json:"attachments"`
	Events          []model.AuditEvent `json:"events"`
	SavedAt         time.Time          `json:"saved_at"`
}

// TicketStore es la parte de store.Repo que necesita el snapshot (si en el
// futuro Repo se divide en interfaces más pequeñas, basta con esto).
type TicketStore interface {
	TicketByID(id int64) (*model.Ticket, error)
	Comments(ticketID int64, includeInternal bool) ([]model.Comment, error)
	Attachments(ticketID int64) ([]model.Attachment, error)
	AuditFor(entity string, entityID int64, limit int) ([]model.AuditEvent, error)
}

type Writer struct {
	store TicketStore
	dir   string
	log   *slog.Logger
}

func New(st TicketStore, ticketsDir string, log *slog.Logger) *Writer {
	return &Writer{store: st, dir: ticketsDir, log: log}
}

// Write materializa el snapshot del ticket de forma atómica (temp + rename)
// para nunca dejar un JSON a medias. Los errores no interrumpen la petición:
// la BD es la fuente de verdad y el snapshot se regenera en la siguiente
// mutación.
func (w *Writer) Write(ticketID int64) {
	if w == nil || w.store == nil {
		return
	}
	t, err := w.store.TicketByID(ticketID)
	if err != nil {
		w.log.Error("snapshot: leer ticket", "ticket", ticketID, "err", err)
		return
	}
	comments, err := w.store.Comments(ticketID, true)
	if err != nil {
		w.log.Error("snapshot: leer comentarios", "ticket", ticketID, "err", err)
		return
	}
	attachments, err := w.store.Attachments(ticketID)
	if err != nil {
		w.log.Error("snapshot: leer adjuntos", "ticket", ticketID, "err", err)
		return
	}
	events, err := w.store.AuditFor("ticket", ticketID, 500)
	if err != nil {
		w.log.Error("snapshot: leer auditoría", "ticket", ticketID, "err", err)
		return
	}
	doc := TicketSnapshot{
		SnapshotVersion: 1,
		Ticket:          t,
		Comments:        comments,
		Attachments:     attachments,
		Events:          events,
		SavedAt:         time.Now().UTC(),
	}
	if doc.Comments == nil {
		doc.Comments = []model.Comment{}
	}
	if doc.Attachments == nil {
		doc.Attachments = []model.Attachment{}
	}
	if doc.Events == nil {
		doc.Events = []model.AuditEvent{}
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		w.log.Error("snapshot: serializar", "ticket", ticketID, "err", err)
		return
	}
	dir := filepath.Join(w.dir, fmt.Sprint(ticketID))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		w.log.Error("snapshot: crear directorio", "ticket", ticketID, "err", err)
		return
	}
	final := filepath.Join(dir, "snapshot.json")
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		w.log.Error("snapshot: escribir", "ticket", ticketID, "err", err)
		return
	}
	if err := os.Rename(tmp, final); err != nil {
		w.log.Error("snapshot: renombrar", "ticket", ticketID, "err", err)
		_ = os.Remove(tmp)
	}
}
