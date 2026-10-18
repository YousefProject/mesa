// Package backup exporta e importa los tickets del sistema a partir de sus
// snapshots JSON (data/tickets/<id>/). El formato es un ZIP autocontenido:
//
//	manifest.json                 → versión del formato, fecha, recuento
//	tickets/<id>/snapshot.json    → estado completo del ticket
//	tickets/<id>/attachments/…    → archivos adjuntos
//
// La BD sigue siendo la fuente de verdad: importar reconstruye las filas en la
// BD y los archivos en disco, y al final regenera cada snapshot. Los IDs de
// ticket se preservan; comentarios, adjuntos y auditoría se reinsertan con
// autoincremento. Los usuarios y colas se resuelven por correo/nombre y, si no
// existen, se crean marcadores inactivos para no perder las referencias.
package backup

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/YousefProject/servicedesk/internal/config"
	"github.com/YousefProject/servicedesk/internal/model"
	"github.com/YousefProject/servicedesk/internal/snapshot"
)

// FormatVersion es la versión del formato ZIP; se rechazan versiones futuras.
const FormatVersion = 1

type Manifest struct {
	FormatVersion int       `json:"format_version"`
	CreatedAt     time.Time `json:"created_at"`
	AppVersion    string    `json:"app_version"`
	Tickets       int       `json:"tickets"`
}

// Store es la parte de store.Repo que necesita el backup.
type Store interface {
	TicketIDs() ([]int64, error)
	TicketByID(id int64) (*model.Ticket, error)
	RestoreTicket(t *model.Ticket) error
	RestoreComment(c *model.Comment) (int64, error)
	RestoreAttachment(a *model.Attachment) error
	RestoreAudit(e *model.AuditEvent) error
	DeleteTicket(id int64) error
	UserByID(id int64) (*model.User, error)
	UserByEmail(email string) (*model.User, string, error)
	CreateUserWithID(id int64, email, name string, role model.Role, passwordHash string) error
	QueueByID(id int64) (*model.Queue, error)
	QueueByName(name string) (*model.Queue, error)
	CreateQueue(name, description string) (*model.Queue, error)
	Comments(ticketID int64, includeInternal bool) ([]model.Comment, error)
	Attachments(ticketID int64) ([]model.Attachment, error)
	AuditFor(entity string, entityID int64, limit int) ([]model.AuditEvent, error)
}

// Export escribe en w un ZIP con el snapshot de todos los tickets. Los
// snapshots deben existir en disco (regenerarlos antes con -build-snapshots o
// snapshot.Writer). Devuelve el número de tickets exportados.
func Export(st Store, ticketsDir string, appVersion string, w io.Writer) (int, error) {
	ids, err := st.TicketIDs()
	if err != nil {
		return 0, fmt.Errorf("listar tickets: %w", err)
	}
	zw := zip.NewWriter(w)
	count := 0
	for _, id := range ids {
		dir := path.Join("tickets", strconv.FormatInt(id, 10))
		snapPath := filepath.Join(ticketsDir, strconv.FormatInt(id, 10), "snapshot.json")
		data, err := os.ReadFile(snapPath)
		if err != nil {
			// Un ticket sin snapshot no se exporta; se regenera en la siguiente
			// mutación o con -build-snapshots.
			continue
		}
		fw, err := zw.Create(dir + "/snapshot.json")
		if err != nil {
			return count, err
		}
		if _, err := fw.Write(data); err != nil {
			return count, err
		}
		attDir := filepath.Join(ticketsDir, strconv.FormatInt(id, 10), "attachments")
		entries, err := os.ReadDir(attDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			full := filepath.Join(attDir, e.Name())
			if err := addFileToZip(zw, full, dir+"/attachments/"+e.Name()); err != nil {
				return count, err
			}
		}
		count++
	}
	manifest, err := json.MarshalIndent(Manifest{
		FormatVersion: FormatVersion,
		CreatedAt:     time.Now().UTC(),
		AppVersion:    appVersion,
		Tickets:       count,
	}, "", "  ")
	if err != nil {
		return count, err
	}
	mw, err := zw.Create("manifest.json")
	if err != nil {
		return count, err
	}
	if _, err := mw.Write(manifest); err != nil {
		return count, err
	}
	if err := zw.Close(); err != nil {
		return count, err
	}
	return count, nil
}

func addFileToZip(zw *zip.Writer, src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := zw.Create(dest)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	return err
}

type ImportOptions struct {
	// Overwrite reemplaza los tickets que ya existen con el mismo ID;
	// sin esta opción se conservan los existentes y se omiten del backup.
	Overwrite bool
	Logger    *slog.Logger
}

type Report struct {
	Tickets  int      `json:"tickets"`
	Imported []int64  `json:"imported"`
	Skipped  []int64  `json:"skipped"`
	Replaced []int64  `json:"replaced"`
	Warnings []string `json:"warnings"`
}

// Import lee un ZIP de backup y reconstruye tickets, comentarios, adjuntos y
// auditoría en la BD y en data/tickets/<id>/.
func Import(st Store, cfg config.Config, r io.ReaderAt, size int64, opts ImportOptions) (*Report, error) {
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("abrir ZIP: %w", err)
	}
	if err := checkManifest(zr); err != nil {
		return nil, err
	}
	rep := &Report{}
	snaps := snapshot.New(st, cfg.TicketsDir, log)
	for _, dir := range ticketDirs(zr) {
		doc, attFiles, err := readTicketFromZip(zr, dir)
		if err != nil {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("ticket %s: %v", dir, err))
			continue
		}
		if doc == nil || doc.Ticket == nil || doc.Ticket.ID == 0 {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("%s: snapshot sin ticket válido", dir))
			continue
		}
		rep.Tickets++
		id := doc.Ticket.ID
		if _, err := st.TicketByID(id); err == nil {
			if !opts.Overwrite {
				rep.Skipped = append(rep.Skipped, id)
				continue
			}
			if err := st.DeleteTicket(id); err != nil {
				rep.Warnings = append(rep.Warnings, fmt.Sprintf("ticket %d: %v", id, err))
				continue
			}
			rep.Replaced = append(rep.Replaced, id)
		}
		if err := restoreOne(st, cfg, doc, attFiles, rep, log); err != nil {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("ticket %d: %v", id, err))
			continue
		}
		rep.Imported = append(rep.Imported, id)
		snaps.Write(id)
	}
	return rep, nil
}

func checkManifest(zr *zip.Reader) error {
	for _, f := range zr.File {
		if f.Name != "manifest.json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		var m Manifest
		if err := json.NewDecoder(rc).Decode(&m); err != nil {
			return fmt.Errorf("manifest.json ilegible: %w", err)
		}
		if m.FormatVersion > FormatVersion {
			return fmt.Errorf("formato de backup v%d no soportado (esta versión entiende v%d)", m.FormatVersion, FormatVersion)
		}
		return nil
	}
	return fmt.Errorf("manifest.json no encontrado: no es un backup de mesa")
}

// ticketDirs devuelve los prefijos "tickets/<id>" presentes en el ZIP.
func ticketDirs(zr *zip.Reader) []string {
	seen := map[string]bool{}
	var dirs []string
	for _, f := range zr.File {
		parts := strings.SplitN(f.Name, "/", 3)
		if len(parts) >= 2 && parts[0] == "tickets" && parts[1] != "" {
			d := "tickets/" + parts[1]
			if !seen[d] {
				seen[d] = true
				dirs = append(dirs, d)
			}
		}
	}
	return dirs
}

// zipFile es un fichero del ZIP ya abierto, listo para copiar a disco.
type zipFile struct {
	Name string // nombre del fichero dentro del ZIP (solo base)
	Src  *zip.File
}

func readTicketFromZip(zr *zip.Reader, dir string) (*snapshot.TicketSnapshot, []zipFile, error) {
	var doc snapshot.TicketSnapshot
	var atts []zipFile
	prefix := dir + "/"
	for _, f := range zr.File {
		if !strings.HasPrefix(f.Name, prefix) {
			continue
		}
		rel := strings.TrimPrefix(f.Name, prefix)
		switch {
		case rel == "snapshot.json":
			rc, err := f.Open()
			if err != nil {
				return nil, nil, err
			}
			err = json.NewDecoder(rc).Decode(&doc)
			rc.Close()
			if err != nil {
				return nil, nil, fmt.Errorf("snapshot.json ilegible: %w", err)
			}
		case strings.HasPrefix(rel, "attachments/") && !f.FileInfo().IsDir():
			name := path.Base(rel)
			if name == "." || name == ".." || name == "/" {
				continue
			}
			atts = append(atts, zipFile{Name: name, Src: f})
		}
	}
	return &doc, atts, nil
}

func restoreOne(st Store, cfg config.Config, doc *snapshot.TicketSnapshot, atts []zipFile, rep *Report, log *slog.Logger) error {
	t := doc.Ticket
	t.RequesterID = resolveUser(st, t.RequesterID, t.RequesterMail, t.RequesterName, rep, log)
	if t.AssigneeID != nil {
		id := resolveUser(st, *t.AssigneeID, "", t.AssigneeName, rep, log)
		t.AssigneeID = &id
	}
	t.QueueID = resolveQueue(st, t.QueueID, t.QueueName, rep, log)
	if err := st.RestoreTicket(t); err != nil {
		return err
	}
	// Comentarios: se reinsertan con autoincremento y se mapea el ID antiguo
	// al nuevo para re-apuntar los adjuntos.
	commentIDs := map[int64]int64{}
	for i := range doc.Comments {
		c := doc.Comments[i]
		c.TicketID = t.ID
		c.AuthorID = resolveUser(st, c.AuthorID, "", c.AuthorName, rep, log)
		newID, err := st.RestoreComment(&c)
		if err != nil {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("ticket %d: %v", t.ID, err))
			continue
		}
		commentIDs[c.ID] = newID
	}
	// Adjuntos: copiar ficheros a data/tickets/<id>/attachments/ y reinsertar.
	attDir := cfg.AttachDir(t.ID)
	if err := os.MkdirAll(attDir, 0o700); err != nil {
		return err
	}
	extracted := map[string]bool{}
	for _, zf := range atts {
		if err := extractFile(zf, attDir); err != nil {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("ticket %d: extraer %s: %v", t.ID, zf.Name, err))
			continue
		}
		extracted[zf.Name] = true
	}
	for i := range doc.Attachments {
		a := doc.Attachments[i]
		a.TicketID = t.ID
		a.UploaderID = resolveUser(st, a.UploaderID, "", a.UploaderName, rep, log)
		if a.CommentID != nil {
			if newID, ok := commentIDs[*a.CommentID]; ok {
				a.CommentID = &newID
			} else {
				a.CommentID = nil
			}
		}
		stored := filepath.Base(a.StoredPath)
		if !extracted[stored] {
			rep.Warnings = append(rep.Warnings,
				fmt.Sprintf("ticket %d: el adjunto %q no viene en el ZIP", t.ID, a.Filename))
		}
		a.StoredPath = filepath.Join(attDir, stored)
		if err := st.RestoreAttachment(&a); err != nil {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("ticket %d: %v", t.ID, err))
		}
	}
	for i := range doc.Events {
		e := doc.Events[i]
		e.EntityID = t.ID
		if e.ActorID != nil {
			id := resolveUser(st, *e.ActorID, "", "", rep, log)
			e.ActorID = &id
		}
		if err := st.RestoreAudit(&e); err != nil {
			log.Debug("restaurar auditoría", "ticket", t.ID, "err", err)
		}
	}
	return nil
}

func extractFile(zf zipFile, dir string) error {
	rc, err := zf.Src.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	dst, err := os.OpenFile(filepath.Join(dir, zf.Name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, rc)
	closeErr := dst.Close()
	if err != nil {
		_ = os.Remove(dst.Name())
		return err
	}
	return closeErr
}

// resolveUser mapea un usuario del backup a uno de la BD: por correo si está
// informado, luego por ID; si no existe, crea un marcador inactivo.
func resolveUser(st Store, oldID int64, email, name string, rep *Report, log *slog.Logger) int64 {
	email = strings.TrimSpace(strings.ToLower(email))
	if email != "" {
		if u, _, err := st.UserByEmail(email); err == nil {
			return u.ID
		}
	}
	if u, err := st.UserByID(oldID); err == nil && (email == "" || strings.EqualFold(u.Email, email)) {
		return u.ID
	}
	if name == "" {
		name = "Usuario restaurado"
	}
	if email == "" {
		email = fmt.Sprintf("restored-%d@backup.local", oldID)
	}
	// Hash inutilizable: el marcador no es una cuenta accesible.
	if err := st.CreateUserWithID(oldID, email, name, model.RoleRequester, "!"); err == nil {
		rep.Warnings = append(rep.Warnings,
			fmt.Sprintf("usuario %q (#%d) no existía: creado inactivo", email, oldID))
		return oldID
	}
	// El ID estaba ocupado por otro usuario: se crea con ID nuevo.
	u, _, err := st.UserByEmail(email)
	if err == nil {
		return u.ID
	}
	rep.Warnings = append(rep.Warnings, fmt.Sprintf("usuario %q: no se pudo restaurar, se usará el solicitante por defecto", email))
	log.Warn("backup: usuario irrecuperable", "email", email, "id", oldID)
	return oldID
}

func resolveQueue(st Store, oldID *int64, name string, rep *Report, log *slog.Logger) *int64 {
	if oldID != nil {
		if q, err := st.QueueByID(*oldID); err == nil {
			return &q.ID
		}
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	if q, err := st.QueueByName(name); err == nil {
		return &q.ID
	}
	q, err := st.CreateQueue(name, "Restaurada desde backup")
	if err != nil {
		log.Warn("backup: cola irrecuperable", "cola", name, "err", err)
		return nil
	}
	rep.Warnings = append(rep.Warnings, fmt.Sprintf("cola %q no existía: creada", name))
	return &q.ID
}
