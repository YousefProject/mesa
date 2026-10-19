package web

import (
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/YousefProject/servicedesk/internal/auth"
	"github.com/YousefProject/servicedesk/internal/model"
	"github.com/YousefProject/servicedesk/internal/sla"
	"github.com/YousefProject/servicedesk/internal/store"
)

type ticketListView struct {
	base
	Tickets         []model.Ticket
	Queues          []model.Queue
	Staff           []model.User
	Filter          store.TicketFilter
	Total           int
	StatusOptions   []model.Status
	PriorityOptions []model.Priority
}

func (s *Server) handleTicketList(w http.ResponseWriter, r *http.Request) {
	u := s.authUser(w, r)
	if u == nil {
		return
	}
	f := store.TicketFilter{
		Query: r.URL.Query().Get("q"),
	}
	if v := r.URL.Query().Get("status"); model.Status(v).Valid() {
		f.Status = model.Status(v)
	}
	if v := r.URL.Query().Get("priority"); model.Priority(v).Valid() {
		f.Priority = model.Priority(v)
	}
	if q := int64ptr(r.URL.Query().Get("queue")); q != nil {
		f.QueueID = q
	}
	switch r.URL.Query().Get("assignee") {
	case "":
	case "none":
		f.Unassigned = true
	default:
		f.AssigneeID = int64ptr(r.URL.Query().Get("assignee"))
	}
	if r.URL.Query().Get("breached") == "1" {
		f.Breached = true
	}
	if !u.Role.IsStaff() {
		f.RequesterID = &u.ID
	}

	tickets, err := s.store.ListTickets(f)
	if err != nil {
		s.logger.Error("listar tickets", "err", err)
		s.errorPage(w, r, http.StatusInternalServerError, "No se pudieron cargar los tickets.")
		return
	}
	total, err := s.store.CountTickets(f)
	if err != nil {
		s.logger.Error("contar tickets", "err", err)
	}
	queues, err := s.store.Queues()
	if err != nil {
		s.logger.Error("listar colas", "err", err)
	}
	var staff []model.User
	if u.Role.IsStaff() {
		staff, _ = s.store.Staff()
	}
	s.page(w, r, http.StatusOK, "tickets.html", ticketListView{
		base:            s.baseView(r, "Tickets"),
		Tickets:         tickets,
		Queues:          queues,
		Staff:           staff,
		Filter:          f,
		Total:           total,
		StatusOptions:   model.Statuses(),
		PriorityOptions: model.Priorities(),
	})
}

type ticketFormView struct {
	base
	Queues   []model.Queue
	Staff    []model.User
	Priority []model.Priority
	Subject  string
	QueueID  *int64
}

func (s *Server) handleTicketForm(w http.ResponseWriter, r *http.Request) {
	u := s.authUser(w, r)
	if u == nil {
		return
	}
	queues, err := s.store.Queues()
	if err != nil {
		s.logger.Error("listar colas", "err", err)
	}
	var staff []model.User
	if u.Role.IsStaff() {
		staff, _ = s.store.Staff()
	}
	s.page(w, r, http.StatusOK, "ticket_new.html", ticketFormView{
		base:     s.baseView(r, "Nuevo ticket"),
		Queues:   queues,
		Staff:    staff,
		Priority: model.Priorities(),
	})
}

func (s *Server) handleTicketCreate(w http.ResponseWriter, r *http.Request) {
	u := s.authUser(w, r)
	if u == nil {
		return
	}
	subject := strings.TrimSpace(r.PostFormValue("subject"))
	description := strings.TrimSpace(r.PostFormValue("description"))
	priority := model.Priority(strings.TrimSpace(r.PostFormValue("priority")))
	if !priority.Valid() {
		priority = model.PriorityNormal
	}
	queueID := int64ptr(r.PostFormValue("queue_id"))
	assignee := int64ptr(r.PostFormValue("assignee_id"))
	if !u.Role.IsStaff() {
		assignee = nil
	}

	if subject == "" || len([]rune(subject)) < 3 {
		s.redirect(w, r, "/tickets/new?err="+qr("el asunto debe tener al menos 3 caracteres"))
		return
	}

	policy, err := s.store.SLAPolicy(priority)
	if err != nil {
		s.logger.Error("política SLA", "err", err)
		policy = model.SLAPolicy{Priority: priority, ResponseMinutes: 240, ResolutionMinutes: 1440}
	}
	created := time.Now().UTC()
	dueResp, dueRes := sla.Due(created, policy)

	ticket, err := s.store.CreateTicket(store.NewTicket{
		Subject:       subject,
		Description:   description,
		Priority:      priority,
		QueueID:       queueID,
		RequesterID:   u.ID,
		AssigneeID:    assignee,
		DueResponse:   dueResp,
		DueResolution: dueRes,
	})
	if err != nil {
		s.logger.Error("crear ticket", "err", err)
		s.redirect(w, r, "/tickets/new?err="+qr("no se pudo crear el ticket"))
		return
	}
	s.store.Audit(&u.ID, "ticket.create", "ticket", ticket.ID, "asunto: "+subject)
	s.snaps.Write(ticket.ID)
	s.notifyTicketCreated(ticket)
	s.redirect(w, r, "/tickets/"+fmt.Sprint(ticket.ID)+"?ok="+qr(ticket.Ref+" creado"))
}

type ticketView struct {
	base
	Ticket      *model.Ticket
	Comments    []model.Comment
	Attachments []model.Attachment
	Events      []model.AuditEvent
	Queues      []model.Queue
	Staff       []model.User
	SLAState    string
	CanTriage   bool
	Statuses    []model.Status
	Priorities  []model.Priority
}

func (s *Server) ticketFor(w http.ResponseWriter, r *http.Request) *model.Ticket {
	u := auth.CurrentUser(r)
	id, ok := pathID(r)
	if !ok {
		s.errorPage(w, r, http.StatusNotFound, "Ticket no válido.")
		return nil
	}
	t, err := s.store.TicketByID(id)
	if err != nil {
		s.errorPage(w, r, http.StatusNotFound, "Ese ticket no existe.")
		return nil
	}
	if !auth.CanViewTicket(u, t) {
		s.errorPage(w, r, http.StatusForbidden, "No tienes acceso a este ticket.")
		return nil
	}
	return t
}

func (s *Server) handleTicketShow(w http.ResponseWriter, r *http.Request) {
	u := s.authUser(w, r)
	if u == nil {
		return
	}
	t := s.ticketFor(w, r)
	if t == nil {
		return
	}
	s.renderTicket(w, r, t, http.StatusOK)
}

func (s *Server) renderTicket(w http.ResponseWriter, r *http.Request, t *model.Ticket, status int) {
	u := auth.CurrentUser(r)
	staff := u.Role.IsStaff()
	comments, err := s.store.Comments(t.ID, staff)
	if err != nil {
		s.logger.Error("comentarios", "err", err)
	}
	attachments, _ := s.store.Attachments(t.ID)
	events, _ := s.store.AuditFor("ticket", t.ID, 50)
	queues, _ := s.store.Queues()
	var staffUsers []model.User
	if staff {
		staffUsers, _ = s.store.Staff()
	}
	s.page(w, r, status, "ticket.html", ticketView{
		base:        s.baseView(r, t.Ref),
		Ticket:      t,
		Comments:    comments,
		Attachments: attachments,
		Events:      events,
		Queues:      queues,
		Staff:       staffUsers,
		SLAState:    string(sla.Evaluate(*t, time.Now().UTC(), slaThreshold)),
		CanTriage:   staff,
		Statuses:    model.Statuses(),
		Priorities:  model.Priorities(),
	})
}

func (s *Server) handleCommentCreate(w http.ResponseWriter, r *http.Request) {
	u := s.authUser(w, r)
	if u == nil {
		return
	}
	t := s.ticketFor(w, r)
	if t == nil {
		return
	}
	body := strings.TrimSpace(r.PostFormValue("body"))
	internal := r.PostFormValue("internal") == "1" && u.Role.IsStaff()
	if body == "" {
		s.redirect(w, r, "/tickets/"+fmt.Sprint(t.ID)+"?err="+qr("el comentario está vacío"))
		return
	}
	comment, err := s.store.AddComment(t.ID, u.ID, body, internal)
	if err != nil {
		s.logger.Error("crear comentario", "err", err)
		s.redirect(w, r, "/tickets/"+fmt.Sprint(t.ID)+"?err="+qr("no se pudo publicar el comentario"))
		return
	}
	if u.Role.IsStaff() {
		if err := s.store.MarkFirstResponse(t.ID); err != nil {
			s.logger.Error("marcar respuesta", "err", err)
		}
	}
	s.store.Audit(&u.ID, "comment.create", "ticket", t.ID, fmt.Sprintf("comentario #%d interno=%v", comment.ID, internal))
	s.snaps.Write(t.ID)
	s.notifyComment(t, comment)

	if r.Header.Get("HX-Request") == "true" {
		attachments, _ := s.store.Attachments(t.ID)
		s.partial(w, "commentItem", struct {
			Comment     model.Comment
			Attachments []model.Attachment
		}{*comment, attachments})
		return
	}
	s.redirect(w, r, "/tickets/"+fmt.Sprint(t.ID)+"?ok="+qr("comentario publicado")+"#c"+fmt.Sprint(comment.ID))
}

func (s *Server) handleTicketStatus(w http.ResponseWriter, r *http.Request) {
	u := s.requireStaff(w, r)
	if u == nil {
		return
	}
	t := s.ticketFor(w, r)
	if t == nil {
		return
	}
	status := model.Status(strings.TrimSpace(r.PostFormValue("status")))
	if !status.Valid() {
		s.redirect(w, r, "/tickets/"+fmt.Sprint(t.ID)+"?err="+qr("estado no válido"))
		return
	}
	if err := s.store.SetStatus(t.ID, status); err != nil {
		s.logger.Error("cambiar estado", "err", err)
	}
	s.store.Audit(&u.ID, "ticket.status", "ticket", t.ID, fmt.Sprintf("%s → %s", t.Status, status))
	s.snaps.Write(t.ID)
	s.reloadAndRespond(w, r, t.ID)
}

func (s *Server) handleTicketPriority(w http.ResponseWriter, r *http.Request) {
	u := s.requireStaff(w, r)
	if u == nil {
		return
	}
	t := s.ticketFor(w, r)
	if t == nil {
		return
	}
	priority := model.Priority(strings.TrimSpace(r.PostFormValue("priority")))
	if !priority.Valid() {
		s.redirect(w, r, "/tickets/"+fmt.Sprint(t.ID)+"?err="+qr("prioridad no válida"))
		return
	}
	policy, err := s.store.SLAPolicy(priority)
	if err != nil {
		s.logger.Error("política SLA", "err", err)
	}
	dueResp, dueRes := sla.Due(t.CreatedAt, policy)
	if err := s.store.SetPriority(t.ID, priority, dueResp, dueRes); err != nil {
		s.logger.Error("cambiar prioridad", "err", err)
	}
	s.store.Audit(&u.ID, "ticket.priority", "ticket", t.ID, fmt.Sprintf("%s → %s", t.Priority, priority))
	s.snaps.Write(t.ID)
	s.reloadAndRespond(w, r, t.ID)
}

func (s *Server) handleTicketAssign(w http.ResponseWriter, r *http.Request) {
	u := s.requireStaff(w, r)
	if u == nil {
		return
	}
	t := s.ticketFor(w, r)
	if t == nil {
		return
	}
	raw := strings.TrimSpace(r.PostFormValue("assignee_id"))
	var assignee *int64
	if raw == "me" {
		assignee = &u.ID
	} else {
		assignee = int64ptr(raw)
	}
	if err := s.store.SetAssignee(t.ID, assignee); err != nil {
		s.logger.Error("asignar", "err", err)
	}
	target := "nadie"
	if assignee != nil {
		if a, err := s.store.UserByID(*assignee); err == nil {
			target = a.Name
			s.notifyUsers([]model.User{*a}, "["+t.Ref+"] te han asignado un ticket", t.Subject+"\n\n"+s.conf().BaseURL+"/tickets/"+fmt.Sprint(t.ID))
		}
	}
	s.store.Audit(&u.ID, "ticket.assign", "ticket", t.ID, "asignado a "+target)
	s.snaps.Write(t.ID)
	s.reloadAndRespond(w, r, t.ID)
}

func (s *Server) handleTicketQueue(w http.ResponseWriter, r *http.Request) {
	u := s.requireStaff(w, r)
	if u == nil {
		return
	}
	t := s.ticketFor(w, r)
	if t == nil {
		return
	}
	queue := int64ptr(r.PostFormValue("queue_id"))
	if err := s.store.SetQueue(t.ID, queue); err != nil {
		s.logger.Error("cambiar cola", "err", err)
	}
	s.store.Audit(&u.ID, "ticket.queue", "ticket", t.ID, "cola actualizada")
	s.snaps.Write(t.ID)
	s.reloadAndRespond(w, r, t.ID)
}

func (s *Server) reloadAndRespond(w http.ResponseWriter, r *http.Request, id int64) {
	t, err := s.store.TicketByID(id)
	if err != nil {
		s.errorPage(w, r, http.StatusNotFound, "Ese ticket no existe.")
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		queues, _ := s.store.Queues()
		staff, _ := s.store.Staff()
		s.partial(w, "ticketSide", ticketView{
			base:       s.baseView(r, t.Ref),
			Ticket:     t,
			Queues:     queues,
			Staff:      staff,
			SLAState:   string(sla.Evaluate(*t, time.Now().UTC(), slaThreshold)),
			CanTriage:  true,
			Statuses:   model.Statuses(),
			Priorities: model.Priorities(),
		})
		return
	}
	s.redirect(w, r, "/tickets/"+fmt.Sprint(id)+"?ok="+qr("ticket actualizado"))
}

func (s *Server) handleTicketAttach(w http.ResponseWriter, r *http.Request) {
	u := s.authUser(w, r)
	if u == nil {
		return
	}
	t := s.ticketFor(w, r)
	if t == nil {
		return
	}
	maxBytes := s.conf().MaxUploadMB * 1024 * 1024
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+maxBytes/10)
	if err := r.ParseMultipartForm(maxBytes); err != nil {
		s.redirect(w, r, "/tickets/"+fmt.Sprint(t.ID)+"?err="+qr("archivo demasiado grande (máx "+fmt.Sprint(s.conf().MaxUploadMB)+" MB)"))
		return
	}
	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		s.redirect(w, r, "/tickets/"+fmt.Sprint(t.ID)+"?err="+qr("no has adjuntado ningún archivo"))
		return
	}
	var commentID *int64
	if body := strings.TrimSpace(r.PostFormValue("body")); body != "" {
		c, err := s.store.AddComment(t.ID, u.ID, body, false)
		if err != nil {
			s.logger.Error("comentario con adjunto", "err", err)
		} else {
			commentID = &c.ID
			if u.Role.IsStaff() {
				_ = s.store.MarkFirstResponse(t.ID)
			}
			s.notifyComment(t, c)
		}
	}
	saved := 0
	for _, header := range files {
		if header.Size > maxUploadBytes {
			continue
		}
		if err := s.saveAttachment(t.ID, commentID, u.ID, header); err != nil {
			s.logger.Error("guardar adjunto", "err", err, "file", header.Filename)
			continue
		}
		saved++
	}
	s.store.Audit(&u.ID, "attachment.create", "ticket", t.ID, fmt.Sprintf("%d archivo(s)", saved))
	s.snaps.Write(t.ID)
	s.redirect(w, r, "/tickets/"+fmt.Sprint(t.ID)+"?ok="+qr(fmt.Sprintf("%d adjunto(s) subido(s)", saved)))
}

const maxUploadBytes = 25 << 20

func (s *Server) saveAttachment(ticketID int64, commentID *int64, uploaderID int64, fh *multipart.FileHeader) error {
	src, err := fh.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	dir := s.conf().AttachDir(ticketID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	name := filepath.Base(sanitizeFilename(fh.Filename))
	stored := filepath.Join(dir, fmt.Sprintf("%d_%s", time.Now().UnixNano(), name))
	dst, err := os.OpenFile(stored, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	size, err := io.Copy(dst, io.LimitReader(src, maxUploadBytes))
	closeErr := dst.Close()
	if err != nil {
		_ = os.Remove(stored)
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	mime := fh.Header.Get("Content-Type")
	if mime == "" {
		mime = "application/octet-stream"
	}
	_, err = s.store.AddAttachment(ticketID, commentID, uploaderID, name, size, mime, stored)
	return err
}

func sanitizeFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case strings.ContainsRune("._- ()[]áéíóúñÁÉÍÓÚÑ", r):
			return r
		default:
			return '_'
		}
	}, name)
	if name == "" || name == "." || name == ".." {
		return "adjunto"
	}
	return name
}

func (s *Server) handleAttachmentDownload(w http.ResponseWriter, r *http.Request) {
	u := s.authUser(w, r)
	if u == nil {
		return
	}
	id, ok := pathID(r)
	if !ok {
		s.errorPage(w, r, http.StatusNotFound, "Adjunto no válido.")
		return
	}
	a, err := s.store.AttachmentByID(id)
	if err != nil {
		s.errorPage(w, r, http.StatusNotFound, "Ese adjunto no existe.")
		return
	}
	t, err := s.store.TicketByID(a.TicketID)
	if err != nil || !auth.CanViewTicket(u, t) {
		s.errorPage(w, r, http.StatusForbidden, "No tienes acceso a este adjunto.")
		return
	}
	f, err := os.Open(a.StoredPath)
	if err != nil {
		s.errorPage(w, r, http.StatusNotFound, "El archivo ya no está disponible.")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		s.errorPage(w, r, http.StatusInternalServerError, "No se pudo leer el archivo.")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+sanitizeFilename(a.Filename)+"\"")
	http.ServeContent(w, r, a.Filename, info.ModTime(), f)
}
