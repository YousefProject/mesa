package web

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/YousefProject/servicedesk/internal/auth"
	"github.com/YousefProject/servicedesk/internal/model"
	"github.com/YousefProject/servicedesk/internal/store"
)

type adminUsersView struct {
	base
	Users []model.User
	Roles []model.Role
}

func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	if s.requireAdmin(w, r) == nil {
		return
	}
	users, err := s.store.Users()
	if err != nil {
		s.logger.Error("listar usuarios", "err", err)
	}
	s.page(w, r, http.StatusOK, "admin_users.html", adminUsersView{
		base:  s.baseView(r, "Usuarios"),
		Users: users,
		Roles: []model.Role{model.RoleAdmin, model.RoleAgent, model.RoleRequester},
	})
}

func (s *Server) handleAdminUserCreate(w http.ResponseWriter, r *http.Request) {
	admin := s.requireAdmin(w, r)
	if admin == nil {
		return
	}
	email := auth.NormalizeEmail(r.PostFormValue("email"))
	name := strings.TrimSpace(r.PostFormValue("name"))
	password := r.PostFormValue("password")
	role := model.Role(strings.TrimSpace(r.PostFormValue("role")))
	if !role.Valid() {
		role = model.RoleRequester
	}
	if email == "" || name == "" {
		s.redirect(w, r, "/admin/users?err="+qr("nombre y correo son obligatorios"))
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		s.redirect(w, r, "/admin/users?err="+qr(err.Error()))
		return
	}
	user, err := s.store.CreateUser(email, name, role, hash)
	if err != nil {
		s.redirect(w, r, "/admin/users?err="+qr(err.Error()))
		return
	}
	s.store.Audit(&admin.ID, "user.create", "user", user.ID, "rol "+role.String())
	s.redirect(w, r, "/admin/users?ok="+qr("usuario creado"))
}

func (s *Server) handleAdminUserUpdate(w http.ResponseWriter, r *http.Request) {
	admin := s.requireAdmin(w, r)
	if admin == nil {
		return
	}
	id, ok := pathID(r)
	if !ok {
		s.errorPage(w, r, http.StatusBadRequest, "Usuario no válido.")
		return
	}
	target, err := s.store.UserByID(id)
	if err != nil {
		s.errorPage(w, r, http.StatusNotFound, "Ese usuario no existe.")
		return
	}
	action := strings.TrimSpace(r.PostFormValue("action"))
	switch action {
	case "role":
		role := model.Role(strings.TrimSpace(r.PostFormValue("role")))
		if !role.Valid() {
			s.redirect(w, r, "/admin/users?err="+qr("rol no válido"))
			return
		}
		if target.ID == admin.ID && role != model.RoleAdmin {
			s.redirect(w, r, "/admin/users?err="+qr("no puedes quitarte tu propio rol de administrador"))
			return
		}
		if err := s.store.UpdateUserRole(id, role); err != nil {
			s.redirect(w, r, "/admin/users?err="+qr(err.Error()))
			return
		}
		s.store.Audit(&admin.ID, "user.role", "user", id, "nuevo rol "+role.String())
		s.redirect(w, r, "/admin/users?ok="+qr("rol actualizado"))
	case "active":
		active := r.PostFormValue("active") == "1"
		if target.ID == admin.ID && !active {
			s.redirect(w, r, "/admin/users?err="+qr("no puedes desactivar tu propia cuenta"))
			return
		}
		if err := s.store.SetUserActive(id, active); err != nil {
			s.redirect(w, r, "/admin/users?err="+qr(err.Error()))
			return
		}
		if !active {
			_ = s.store.DeleteUserSessions(id)
		}
		s.store.Audit(&admin.ID, "user.active", "user", id, map[bool]string{true: "activado", false: "desactivado"}[active])
		s.redirect(w, r, "/admin/users?ok="+qr("estado actualizado"))
	case "password":
		hash, err := auth.HashPassword(r.PostFormValue("password"))
		if err != nil {
			s.redirect(w, r, "/admin/users?err="+qr(err.Error()))
			return
		}
		if err := s.store.SetPassword(id, hash); err != nil {
			s.redirect(w, r, "/admin/users?err="+qr(err.Error()))
			return
		}
		_ = s.store.DeleteUserSessions(id)
		s.store.Audit(&admin.ID, "user.password.reset", "user", id, "contraseña restablecida")
		s.redirect(w, r, "/admin/users?ok="+qr("contraseña restablecida"))
	default:
		s.redirect(w, r, "/admin/users?err="+qr("acción desconocida"))
	}
}

type adminQueuesView struct {
	base
	Queues []model.Queue
}

func (s *Server) handleAdminQueues(w http.ResponseWriter, r *http.Request) {
	if s.requireAdmin(w, r) == nil {
		return
	}
	queues, err := s.store.Queues()
	if err != nil {
		s.logger.Error("listar colas", "err", err)
	}
	s.page(w, r, http.StatusOK, "admin_queues.html", adminQueuesView{s.baseView(r, "Colas"), queues})
}

func (s *Server) handleAdminQueueCreate(w http.ResponseWriter, r *http.Request) {
	admin := s.requireAdmin(w, r)
	if admin == nil {
		return
	}
	q, err := s.store.CreateQueue(r.PostFormValue("name"), r.PostFormValue("description"))
	if err != nil {
		s.redirect(w, r, "/admin/queues?err="+qr(err.Error()))
		return
	}
	s.store.Audit(&admin.ID, "queue.create", "queue", q.ID, "cola "+q.Name)
	s.redirect(w, r, "/admin/queues?ok="+qr("cola creada"))
}

type adminSLAView struct {
	base
	Policies []model.SLAPolicy
}

func (s *Server) handleAdminSLA(w http.ResponseWriter, r *http.Request) {
	if s.requireAdmin(w, r) == nil {
		return
	}
	policies, err := s.store.SLAPolicies()
	if err != nil {
		s.logger.Error("listar SLA", "err", err)
	}
	s.page(w, r, http.StatusOK, "admin_sla.html", adminSLAView{s.baseView(r, "SLA"), policies})
}

func (s *Server) handleAdminSLASave(w http.ResponseWriter, r *http.Request) {
	admin := s.requireAdmin(w, r)
	if admin == nil {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.redirect(w, r, "/admin/sla?err="+qr("formulario no válido"))
		return
	}
	for _, p := range model.Priorities() {
		key := string(p)
		resp := atoiDefault(r.PostFormValue("response_"+key), 0)
		res := atoiDefault(r.PostFormValue("resolution_"+key), 0)
		if resp <= 0 || res <= 0 {
			continue
		}
		if err := s.store.UpdateSLAPolicy(model.SLAPolicy{
			Priority: p, ResponseMinutes: resp, ResolutionMinutes: res,
		}); err != nil {
			s.logger.Error("guardar SLA", "err", err)
			s.redirect(w, r, "/admin/sla?err="+qr("no se pudieron guardar las políticas"))
			return
		}
	}
	s.store.Audit(&admin.ID, "sla.update", "sla", 0, "políticas actualizadas")
	s.redirect(w, r, "/admin/sla?ok="+qr("políticas guardadas (aplican a tickets nuevos o al cambiar prioridad)"))
}

type adminAuditView struct {
	base
	Events []model.AuditEvent
}

func (s *Server) handleAdminAudit(w http.ResponseWriter, r *http.Request) {
	if s.requireAdmin(w, r) == nil {
		return
	}
	events, err := s.store.RecentAudit(200)
	if err != nil {
		s.logger.Error("auditoría", "err", err)
	}
	s.page(w, r, http.StatusOK, "admin_audit.html", adminAuditView{s.baseView(r, "Auditoría"), events})
}

type adminTemplatesView struct {
	base
	Templates  []model.TicketTemplate
	Queues     []model.Queue
	Priorities []model.Priority
}

func (s *Server) handleAdminTemplates(w http.ResponseWriter, r *http.Request) {
	if s.requireAdmin(w, r) == nil {
		return
	}
	templates, err := s.store.Templates(true)
	if err != nil {
		s.logger.Error("listar plantillas", "err", err)
	}
	queues, err := s.store.Queues()
	if err != nil {
		s.logger.Error("listar colas", "err", err)
	}
	s.page(w, r, http.StatusOK, "admin_templates.html", adminTemplatesView{
		base:       s.baseView(r, "Plantillas"),
		Templates:  templates,
		Queues:     queues,
		Priorities: model.Priorities(),
	})
}

// templateFromForm lee los campos de una plantilla desde el formulario.
func templateFromForm(r *http.Request) store.NewTemplate {
	priority := model.Priority(strings.TrimSpace(r.PostFormValue("priority")))
	if !priority.Valid() {
		priority = model.PriorityNormal
	}
	return store.NewTemplate{
		Name:        r.PostFormValue("name"),
		Category:    r.PostFormValue("category"),
		Subject:     r.PostFormValue("subject"),
		Description: r.PostFormValue("description"),
		Priority:    priority,
		QueueID:     int64ptr(r.PostFormValue("queue_id")),
		Active:      r.PostFormValue("active") == "1",
	}
}

func (s *Server) handleAdminTemplateCreate(w http.ResponseWriter, r *http.Request) {
	admin := s.requireAdmin(w, r)
	if admin == nil {
		return
	}
	tpl, err := s.store.CreateTemplate(templateFromForm(r))
	if err != nil {
		s.redirect(w, r, "/admin/templates?err="+qr(err.Error()))
		return
	}
	s.store.Audit(&admin.ID, "template.create", "template", tpl.ID, "plantilla "+tpl.Name)
	s.redirect(w, r, "/admin/templates?ok="+qr("plantilla creada"))
}

type adminTemplateEditView struct {
	base
	Template   *model.TicketTemplate
	Queues     []model.Queue
	Priorities []model.Priority
}

func (s *Server) handleAdminTemplateEditForm(w http.ResponseWriter, r *http.Request) {
	if s.requireAdmin(w, r) == nil {
		return
	}
	id, ok := pathID(r)
	if !ok {
		s.errorPage(w, r, http.StatusBadRequest, "Plantilla no válida.")
		return
	}
	tpl, err := s.store.TemplateByID(id)
	if err != nil {
		s.errorPage(w, r, http.StatusNotFound, "Esa plantilla no existe.")
		return
	}
	queues, _ := s.store.Queues()
	s.page(w, r, http.StatusOK, "admin_template_edit.html", adminTemplateEditView{
		base:       s.baseView(r, "Editar plantilla"),
		Template:   tpl,
		Queues:     queues,
		Priorities: model.Priorities(),
	})
}

func (s *Server) handleAdminTemplateEditSave(w http.ResponseWriter, r *http.Request) {
	admin := s.requireAdmin(w, r)
	if admin == nil {
		return
	}
	id, ok := pathID(r)
	if !ok {
		s.errorPage(w, r, http.StatusBadRequest, "Plantilla no válida.")
		return
	}
	in := templateFromForm(r)
	if err := s.store.UpdateTemplate(id, in); err != nil {
		s.redirect(w, r, "/admin/templates/"+fmt.Sprint(id)+"/edit?err="+qr(err.Error()))
		return
	}
	s.store.Audit(&admin.ID, "template.update", "template", id, "plantilla "+strings.TrimSpace(in.Name))
	s.redirect(w, r, "/admin/templates?ok="+qr("plantilla actualizada"))
}

func (s *Server) handleAdminTemplateDelete(w http.ResponseWriter, r *http.Request) {
	admin := s.requireAdmin(w, r)
	if admin == nil {
		return
	}
	id, ok := pathID(r)
	if !ok {
		s.errorPage(w, r, http.StatusBadRequest, "Plantilla no válida.")
		return
	}
	name := ""
	if tpl, err := s.store.TemplateByID(id); err == nil {
		name = tpl.Name
	}
	if err := s.store.DeleteTemplate(id); err != nil {
		s.logger.Error("eliminar plantilla", "err", err)
		s.redirect(w, r, "/admin/templates?err="+qr("no se pudo eliminar la plantilla"))
		return
	}
	s.store.Audit(&admin.ID, "template.delete", "template", id, "plantilla "+name)
	s.redirect(w, r, "/admin/templates?ok="+qr("plantilla eliminada"))
}
