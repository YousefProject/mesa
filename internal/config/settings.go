package config

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Claves de la tabla settings (consola web). La BD es la fuente de verdad;
// las variables de entorno solo aportan los valores por defecto iniciales.
const (
	KeyAddr         = "addr"
	KeyBaseURL      = "base_url"
	KeyAllowSignup  = "allow_signup"
	KeyCookieSecure = "cookie_secure"
	KeyTrustProxy   = "trust_proxy"
	KeyMaxUploadMB  = "max_upload_mb"
	KeySessionTTL   = "session_ttl"
	KeySMTPHost     = "smtp_host"
	KeySMTPPort     = "smtp_port"
	KeySMTPUser     = "smtp_user"
	KeySMTPPass     = "smtp_pass"
	KeySMTPFrom     = "smtp_from"
	KeySMTPStartTLS = "smtp_starttls"
)

// ValidateSettings comprueba un mapa de ajustes proveniente de la consola web
// antes de persistirlo. Devuelve un error con el primer problema encontrado.
func ValidateSettings(kv map[string]string) error {
	if v := kv[KeyAddr]; v != "" {
		if _, err := net.ResolveTCPAddr("tcp", v); err != nil {
			return fmt.Errorf("dirección de escucha inválida (%q): usa algo como :8080", v)
		}
	}
	if v := kv[KeyBaseURL]; v != "" {
		if !strings.HasPrefix(v, "http://") && !strings.HasPrefix(v, "https://") {
			return fmt.Errorf("la URL pública debe empezar por http:// o https://")
		}
	}
	if v := kv[KeyMaxUploadMB]; v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 1024 {
			return fmt.Errorf("el tamaño máximo de subida debe ser un número entre 1 y 1024")
		}
	}
	if v := kv[KeySessionTTL]; v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < time.Minute {
			return fmt.Errorf("la duración de sesión debe ser un periodo como 168h o 30m")
		}
	}
	if v := kv[KeySMTPPort]; v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("el puerto SMTP debe estar entre 1 y 65535")
		}
	}
	// Con servidor configurado, el remitente es obligatorio: sin él los
	// mensajes saldrían sin cabecera From.
	if kv[KeySMTPHost] != "" && strings.TrimSpace(kv[KeySMTPFrom]) == "" {
		return fmt.Errorf("configura el remitente (From) del correo")
	}
	for _, k := range []string{KeyAllowSignup, KeyCookieSecure, KeyTrustProxy, KeySMTPStartTLS} {
		if v := kv[k]; v != "" && v != "true" && v != "false" {
			return fmt.Errorf("valor booleano inválido para %s", k)
		}
	}
	return nil
}

// ApplySettings sobrescribe en c los ajustes presentes en kv. Los valores
// inválidos se omiten y se acumulan en el error devuelto.
func ApplySettings(c *Config, kv map[string]string) error {
	var errs []string
	set := func(k, v string, fn func()) {
		if err := ValidateSettings(map[string]string{k: v}); err != nil {
			errs = append(errs, err.Error())
			return
		}
		fn()
	}
	// El campo addr es obligatorio: un valor vacío no debe poder dejar al
	// servicio sin dirección de escucha.
	if v, ok := kv[KeyAddr]; ok && v != "" {
		set(KeyAddr, v, func() { c.Addr = v })
	}
	if v, ok := kv[KeyBaseURL]; ok {
		set(KeyBaseURL, v, func() { c.BaseURL = strings.TrimRight(v, "/") })
	}
	if v, ok := kv[KeyAllowSignup]; ok {
		set(KeyAllowSignup, v, func() { c.AllowSignup = v == "true" })
	}
	if v, ok := kv[KeyCookieSecure]; ok {
		set(KeyCookieSecure, v, func() { c.CookieSecure = v == "true" })
	}
	if v, ok := kv[KeyTrustProxy]; ok {
		set(KeyTrustProxy, v, func() { c.TrustProxy = v == "true" })
	}
	if v, ok := kv[KeyMaxUploadMB]; ok {
		set(KeyMaxUploadMB, v, func() {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				c.MaxUploadMB = n
			}
		})
	}
	if v, ok := kv[KeySessionTTL]; ok {
		set(KeySessionTTL, v, func() {
			if d, err := time.ParseDuration(v); err == nil {
				c.SessionTTL = d
			}
		})
	}
	if v, ok := kv[KeySMTPHost]; ok {
		c.SMTP.Host = v
	}
	if v, ok := kv[KeySMTPPort]; ok {
		set(KeySMTPPort, v, func() {
			if n, err := strconv.Atoi(v); err == nil {
				c.SMTP.Port = n
			}
		})
	}
	if v, ok := kv[KeySMTPUser]; ok {
		c.SMTP.User = v
	}
	if v, ok := kv[KeySMTPPass]; ok {
		c.SMTP.Pass = v
	}
	if v, ok := kv[KeySMTPFrom]; ok {
		c.SMTP.From = v
	}
	if v, ok := kv[KeySMTPStartTLS]; ok {
		set(KeySMTPStartTLS, v, func() { c.SMTP.StartTLS = v == "true" })
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

// Runtime es la configuración viva de la aplicación: la capa web la lee en
// cada petición y la consola de administración la actualiza en caliente
// (sin reiniciar) cuando se guardan los ajustes.
type Runtime struct {
	mu  sync.RWMutex
	cfg Config
}

func NewRuntime(c Config) *Runtime { return &Runtime{cfg: c} }

// Get devuelve una copia del snapshot actual de configuración.
func (r *Runtime) Get() Config {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cfg
}

// Update aplica fn al snapshot bajo escritura y devuelve el resultado.
func (r *Runtime) Update(fn func(*Config)) Config {
	r.mu.Lock()
	defer r.mu.Unlock()
	fn(&r.cfg)
	return r.cfg
}
