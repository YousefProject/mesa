package web

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/YousefProject/servicedesk/internal/auth"
	"github.com/YousefProject/servicedesk/internal/model"
)

func (s *Server) authUser(w http.ResponseWriter, r *http.Request) *model.User {
	u := auth.CurrentUser(r)
	if u == nil {
		s.redirect(w, r, "/login?err="+qr("inicia sesión para continuar"))
		return nil
	}
	return u
}

func (s *Server) requireStaff(w http.ResponseWriter, r *http.Request) *model.User {
	u := s.authUser(w, r)
	if u == nil {
		return nil
	}
	if !u.Role.IsStaff() {
		s.errorPage(w, r, http.StatusForbidden, "Necesitas rol de agente o administrador.")
		return nil
	}
	return u
}

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) *model.User {
	u := s.authUser(w, r)
	if u == nil {
		return nil
	}
	if u.Role != model.RoleAdmin {
		s.errorPage(w, r, http.StatusForbidden, "Solo los administradores pueden acceder a esta sección.")
		return nil
	}
	return u
}

func qr(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), " ", "+")
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func int64ptr(s string) *int64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || v <= 0 {
		return nil
	}
	return &v
}

func (s *Server) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	if auth.CurrentUser(r) != nil {
		s.redirect(w, r, "/")
		return
	}
	s.page(w, r, http.StatusOK, "login.html", struct {
		base
		AllowSignup bool
	}{s.baseView(r, "Entrar"), s.cfg.AllowSignup})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	email := auth.NormalizeEmail(r.PostFormValue("email"))
	password := r.PostFormValue("password")
	user, hash, err := s.store.UserByEmail(email)
	if err != nil {
		s.failLogin(w, r, "Credenciales incorrectas")
		return
	}
	if !user.Active {
		s.failLogin(w, r, "La cuenta está desactivada")
		return
	}
	if err := auth.CheckPassword(hash, password); err != nil {
		s.store.Audit(&user.ID, "login.failed", "user", user.ID, "contraseña incorrecta")
		s.failLogin(w, r, "Credenciales incorrectas")
		return
	}
	if err := s.login(w, r, user.ID); err != nil {
		s.logger.Error("login", "err", err)
		s.failLogin(w, r, "No se pudo iniciar sesión")
		return
	}
	s.store.Audit(&user.ID, "login", "user", user.ID, "sesión iniciada")
	s.redirect(w, r, "/")
}

func (s *Server) failLogin(w http.ResponseWriter, r *http.Request, msg string) {
	s.page(w, r, http.StatusUnauthorized, "login.html", struct {
		base
		AllowSignup bool
	}{base{Title: "Entrar", Error: msg, CSRF: auth.CSRFToken(r.Context()), Now: nowUTC()}, s.cfg.AllowSignup})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(auth.SessionCookie); err == nil {
		_ = s.store.DeleteSession(cookie.Value)
	}
	if u := auth.CurrentUser(r); u != nil {
		s.store.Audit(&u.ID, "logout", "user", u.ID, "sesión cerrada")
	}
	s.clearCookie(w)
	s.redirect(w, r, "/login?ok="+qr("sesión cerrada"))
}

func (s *Server) handleSignupForm(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.AllowSignup {
		s.errorPage(w, r, http.StatusForbidden, "El registro público está desactivado. Pide una cuenta a un administrador.")
		return
	}
	s.page(w, r, http.StatusOK, "signup.html", struct {
		base
	}{base{Title: "Crear cuenta", CSRF: auth.CSRFToken(r.Context()), Now: nowUTC()}})
}

func (s *Server) handleSignup(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.AllowSignup {
		s.errorPage(w, r, http.StatusForbidden, "El registro público está desactivado.")
		return
	}
	email := auth.NormalizeEmail(r.PostFormValue("email"))
	name := strings.TrimSpace(r.PostFormValue("name"))
	password := r.PostFormValue("password")
	view := struct {
		base
	}{base{Title: "Crear cuenta", CSRF: auth.CSRFToken(r.Context()), Now: nowUTC()}}

	if email == "" || !strings.Contains(email, "@") {
		view.Error = "Introduce un correo válido."
		s.page(w, r, http.StatusBadRequest, "signup.html", view)
		return
	}
	if name == "" {
		view.Error = "Introduce tu nombre."
		s.page(w, r, http.StatusBadRequest, "signup.html", view)
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		view.Error = err.Error()
		s.page(w, r, http.StatusBadRequest, "signup.html", view)
		return
	}
	user, err := s.store.CreateUser(email, name, model.RoleRequester, hash)
	if err != nil {
		view.Error = err.Error()
		s.page(w, r, http.StatusConflict, "signup.html", view)
		return
	}
	s.store.Audit(&user.ID, "user.signup", "user", user.ID, "registro público")
	if err := s.login(w, r, user.ID); err != nil {
		s.logger.Error("login tras signup", "err", err)
	}
	s.redirect(w, r, "/?ok="+qr("cuenta creada"))
}

func (s *Server) handleProfile(w http.ResponseWriter, r *http.Request) {
	u := s.authUser(w, r)
	if u == nil {
		return
	}
	events, err := s.store.AuditFor("user", u.ID, 20)
	if err != nil {
		s.logger.Error("audit perfil", "err", err)
	}
	s.page(w, r, http.StatusOK, "profile.html", struct {
		base
		Events []model.AuditEvent
	}{s.baseView(r, "Mi cuenta"), events})
}

func (s *Server) handleProfileSave(w http.ResponseWriter, r *http.Request) {
	u := s.authUser(w, r)
	if u == nil {
		return
	}
	if err := s.store.UpdateUserProfile(u.ID, r.PostFormValue("name")); err != nil {
		s.redirect(w, r, "/me?err="+qr(err.Error()))
		return
	}
	s.store.Audit(&u.ID, "user.profile", "user", u.ID, "nombre actualizado")
	s.redirect(w, r, "/me?ok="+qr("perfil actualizado"))
}

func (s *Server) handlePasswordChange(w http.ResponseWriter, r *http.Request) {
	u := s.authUser(w, r)
	if u == nil {
		return
	}
	current := r.PostFormValue("current")
	next := r.PostFormValue("password")
	hash, err := s.store.PasswordHash(u.ID)
	if err != nil {
		s.redirect(w, r, "/me?err="+qr("no se pudo verificar la contraseña"))
		return
	}
	if err := auth.CheckPassword(hash, current); err != nil {
		s.redirect(w, r, "/me?err="+qr("la contraseña actual no coincide"))
		return
	}
	newHash, err := auth.HashPassword(next)
	if err != nil {
		s.redirect(w, r, "/me?err="+qr(err.Error()))
		return
	}
	if err := s.store.SetPassword(u.ID, newHash); err != nil {
		s.redirect(w, r, "/me?err="+qr("no se pudo guardar"))
		return
	}
	_ = s.store.DeleteUserSessions(u.ID)
	s.store.Audit(&u.ID, "user.password", "user", u.ID, "contraseña cambiada")
	s.clearCookie(w)
	s.redirect(w, r, "/login?ok="+qr("contraseña actualizada, vuelve a entrar"))
}

func nowUTC() time.Time { return time.Now().UTC() }
