package web

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/YousefProject/servicedesk/internal/auth"
	"github.com/YousefProject/servicedesk/internal/config"
	"github.com/YousefProject/servicedesk/internal/mail"
	"github.com/YousefProject/servicedesk/internal/snapshot"
	"github.com/YousefProject/servicedesk/internal/store"
)

type Server struct {
	cfg     *config.Runtime
	store   store.Repo
	mail    mail.Sender
	render  *Renderer
	logger  *slog.Logger
	version string
	mux     *http.ServeMux
	snaps   *snapshot.Writer
	// rebind reabre el listener HTTP en una nueva dirección. Lo instala
	// main.go al crear el servidor; si es nil, el cambio de puerto se
	// aplica en el próximo reinicio.
	rebind func(addr string) error
}

func New(rt *config.Runtime, st store.Repo, sender mail.Sender, logger *slog.Logger, version string) (*Server, error) {
	r, err := NewRenderer()
	if err != nil {
		return nil, err
	}
	s := &Server{
		cfg: rt, store: st, mail: sender, render: r, logger: logger, version: version,
		mux: http.NewServeMux(), snaps: snapshot.New(st, rt.Get().TicketsDir, logger),
	}
	s.routes()
	return s, nil
}

// conf devuelve la configuración vigente (snapshot en el momento de la
// llamada). Los ajustes guardados en la consola web se reflejan al instante.
func (s *Server) conf() config.Config { return s.cfg.Get() }

func (s *Server) Handler() http.Handler {
	return s.recover(s.logRequests(s.securityHeaders(s.loadSession(s.csrfProtect(s.mux)))))
}

func (s *Server) routes() {
	m := s.mux
	m.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(StaticFS())))
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok\n"))
	})

	m.HandleFunc("GET /{$}", s.handleHome)
	m.HandleFunc("GET /login", s.handleLoginForm)
	m.HandleFunc("POST /login", s.handleLogin)
	m.HandleFunc("POST /logout", s.handleLogout)
	m.HandleFunc("GET /signup", s.handleSignupForm)
	m.HandleFunc("POST /signup", s.handleSignup)

	m.HandleFunc("GET /tickets", s.handleTicketList)
	m.HandleFunc("GET /tickets/new", s.handleTicketForm)
	m.HandleFunc("POST /tickets", s.handleTicketCreate)
	m.HandleFunc("GET /tickets/{id}", s.handleTicketShow)
	m.HandleFunc("POST /tickets/{id}/comments", s.handleCommentCreate)
	m.HandleFunc("POST /tickets/{id}/status", s.handleTicketStatus)
	m.HandleFunc("POST /tickets/{id}/priority", s.handleTicketPriority)
	m.HandleFunc("POST /tickets/{id}/assign", s.handleTicketAssign)
	m.HandleFunc("POST /tickets/{id}/queue", s.handleTicketQueue)
	m.HandleFunc("POST /tickets/{id}/attachments", s.handleTicketAttach)
	m.HandleFunc("GET /attachments/{id}", s.handleAttachmentDownload)

	m.HandleFunc("GET /dashboard", s.handleDashboard)

	m.HandleFunc("GET /me", s.handleProfile)
	m.HandleFunc("POST /me", s.handleProfileSave)
	m.HandleFunc("POST /me/password", s.handlePasswordChange)

	m.HandleFunc("GET /admin/users", s.handleAdminUsers)
	m.HandleFunc("POST /admin/users", s.handleAdminUserCreate)
	m.HandleFunc("POST /admin/users/{id}", s.handleAdminUserUpdate)
	m.HandleFunc("GET /admin/queues", s.handleAdminQueues)
	m.HandleFunc("POST /admin/queues", s.handleAdminQueueCreate)
	m.HandleFunc("GET /admin/sla", s.handleAdminSLA)
	m.HandleFunc("POST /admin/sla", s.handleAdminSLASave)
	m.HandleFunc("GET /admin/audit", s.handleAdminAudit)
	m.HandleFunc("GET /admin/backup", s.handleAdminBackupPage)
	m.HandleFunc("GET /admin/backup/export", s.handleAdminBackupExport)
	m.HandleFunc("POST /admin/backup/import", s.handleAdminBackupImport)
	m.HandleFunc("GET /admin/settings", s.handleAdminSettings)
	m.HandleFunc("POST /admin/settings", s.handleAdminSettingsSave)
	m.HandleFunc("POST /admin/settings/test", s.handleAdminSettingsTest)
}

// SetRebinder instala la función que reabre el listener HTTP en otra
// dirección (la llama main.go). Si no se instala, cambiar el puerto solo
// tiene efecto al reiniciar.
func (s *Server) SetRebinder(fn func(addr string) error) { s.rebind = fn }

func (s *Server) baseView(r *http.Request, title string) base {
	u := auth.CurrentUser(r)
	return base{
		Title:   title,
		User:    u,
		CSRF:    auth.CSRFToken(r.Context()),
		Now:     time.Now().UTC(),
		Flash:   r.URL.Query().Get("ok"),
		Error:   r.URL.Query().Get("err"),
		Path:    r.URL.Path,
		Version: s.version,
	}
}

func (s *Server) page(w http.ResponseWriter, r *http.Request, status int, page string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := s.render.Page(w, page, data); err != nil {
		s.logger.Error("render", "page", page, "err", err)
	}
}

func (s *Server) partial(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.render.Partial(w, name, data); err != nil {
		s.logger.Error("render partial", "partial", name, "err", err)
	}
}

func (s *Server) redirect(w http.ResponseWriter, r *http.Request, to string) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", to)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

func (s *Server) errorPage(w http.ResponseWriter, r *http.Request, status int, msg string) {
	s.page(w, r, status, "error.html", struct {
		base
		Message string
	}{s.baseView(r, "Error"), msg})
}
