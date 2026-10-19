package web

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/YousefProject/servicedesk/internal/config"
	"github.com/YousefProject/servicedesk/internal/mail"
)

type settingsView struct {
	base
	Cfg config.Config
}

func (s *Server) handleAdminSettings(w http.ResponseWriter, r *http.Request) {
	if s.requireAdmin(w, r) == nil {
		return
	}
	s.page(w, r, http.StatusOK, "admin_settings.html", settingsView{
		base: s.baseView(r, "Ajustes"),
		Cfg:  s.conf(),
	})
}

// settingsFromForm normaliza el formulario en un mapa de ajustes. Las claves
// booleanas usan el nombre del campo igual que la clave del ajuste.
func settingsFromForm(r *http.Request) map[string]string {
	kv := map[string]string{
		config.KeyAddr:        strings.TrimSpace(r.PostFormValue("addr")),
		config.KeyBaseURL:     strings.TrimSpace(r.PostFormValue("base_url")),
		config.KeyMaxUploadMB: strings.TrimSpace(r.PostFormValue("max_upload_mb")),
		config.KeySessionTTL:  strings.TrimSpace(r.PostFormValue("session_ttl")),
		config.KeySMTPHost:    strings.TrimSpace(r.PostFormValue("smtp_host")),
		config.KeySMTPPort:    strings.TrimSpace(r.PostFormValue("smtp_port")),
		config.KeySMTPUser:    strings.TrimSpace(r.PostFormValue("smtp_user")),
		config.KeySMTPPass:    r.PostFormValue("smtp_pass"),
		config.KeySMTPFrom:    strings.TrimSpace(r.PostFormValue("smtp_from")),
	}
	for _, k := range []string{
		config.KeyAllowSignup, config.KeyCookieSecure,
		config.KeyTrustProxy, config.KeySMTPStartTLS,
	} {
		kv[k] = strconv.FormatBool(r.PostFormValue(k) != "")
	}
	return kv
}

// settingsSnapshot devuelve los ajustes vigentes como mapa comparable.
func settingsSnapshot(c config.Config) map[string]string {
	return map[string]string{
		config.KeyAddr:         c.Addr,
		config.KeyBaseURL:      c.BaseURL,
		config.KeyMaxUploadMB:  strconv.FormatInt(c.MaxUploadMB, 10),
		config.KeySessionTTL:   c.SessionTTL.String(),
		config.KeySMTPHost:     c.SMTP.Host,
		config.KeySMTPPort:     strconv.Itoa(c.SMTP.Port),
		config.KeySMTPUser:     c.SMTP.User,
		config.KeySMTPPass:     c.SMTP.Pass,
		config.KeySMTPFrom:     c.SMTP.From,
		config.KeyAllowSignup:  strconv.FormatBool(c.AllowSignup),
		config.KeyCookieSecure: strconv.FormatBool(c.CookieSecure),
		config.KeyTrustProxy:   strconv.FormatBool(c.TrustProxy),
		config.KeySMTPStartTLS: strconv.FormatBool(c.SMTP.StartTLS),
	}
}

func (s *Server) handleAdminSettingsSave(w http.ResponseWriter, r *http.Request) {
	admin := s.requireAdmin(w, r)
	if admin == nil {
		return
	}
	kv := settingsFromForm(r)
	if err := config.ValidateSettings(kv); err != nil {
		s.redirect(w, r, "/admin/settings?err="+qr(err.Error()))
		return
	}
	cur := s.conf()
	newAddr := kv[config.KeyAddr]
	addrChanged := newAddr != "" && newAddr != cur.Addr

	// El rebind se intenta antes de persistir: si el puerto nuevo no se
	// puede abrir, no se guarda nada y el servicio sigue en el actual.
	if addrChanged && s.rebind != nil {
		if err := s.rebind(newAddr); err != nil {
			s.redirect(w, r, "/admin/settings?err="+qr(err.Error()))
			return
		}
	}
	if err := s.store.SetSettings(kv); err != nil {
		if addrChanged && s.rebind != nil {
			_ = s.rebind(cur.Addr)
		}
		s.redirect(w, r, "/admin/settings?err="+qr(err.Error()))
		return
	}
	s.cfg.Update(func(c *config.Config) { _ = config.ApplySettings(c, kv) })

	snap := settingsSnapshot(cur)
	var changed []string
	for k, v := range kv {
		if snap[k] != v {
			changed = append(changed, k)
		}
	}
	// Solo se registran las claves, nunca los valores (la contraseña SMTP
	// no debe acabar en la auditoría).
	s.store.Audit(&admin.ID, "settings.update", "settings", 0,
		"ajustes modificados: "+strings.Join(sortedStrings(changed), ", "))
	s.redirect(w, r, "/admin/settings?ok="+qr("ajustes guardados"))
}

func (s *Server) handleAdminSettingsTest(w http.ResponseWriter, r *http.Request) {
	admin := s.requireAdmin(w, r)
	if admin == nil {
		return
	}
	kv := settingsFromForm(r)
	cfg := s.conf()
	if err := config.ApplySettings(&cfg, kv); err != nil {
		s.redirect(w, r, "/admin/settings?err="+qr(err.Error()))
		return
	}
	if !cfg.SMTP.Enabled() {
		s.redirect(w, r, "/admin/settings?err="+qr("configura el host SMTP antes de enviar la prueba"))
		return
	}
	to := strings.TrimSpace(r.PostFormValue("test_to"))
	if to == "" {
		to = admin.Email
	}
	body := fmt.Sprintf("Hola,\n\nEste es un correo de prueba enviado desde la mesa de servicios.\n\nFecha:    %s\nVersión:  %s\nServidor: %s\n",
		time.Now().Format(time.RFC1123Z), s.version, cfg.SMTP.Addr())
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := mail.SendWith(ctx, cfg.SMTP, mail.Message{
		To:      []string{to},
		Subject: "Prueba de correo · Mesa de servicios",
		Body:    body,
	}); err != nil {
		s.logger.Warn("prueba de correo", "err", err)
		s.redirect(w, r, "/admin/settings?err="+qr("fallo al enviar: "+err.Error()))
		return
	}
	s.redirect(w, r, "/admin/settings?ok="+qr("correo de prueba enviado a "+to))
}

func sortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
