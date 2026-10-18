package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type SMTP struct {
	Host     string
	Port     int
	User     string
	Pass     string
	From     string
	StartTLS bool
}

func (s SMTP) Enabled() bool { return strings.TrimSpace(s.Host) != "" }

func (s SMTP) Addr() string { return fmt.Sprintf("%s:%d", s.Host, s.Port) }

// Config centraliza la configuración de la aplicación.
// Rutas de datos: cada componente vive en su propia subcarpeta para poder
// aislarlo en migraciones y facilitar su auditoría:
//
//	data/
//	  db/            → base de datos SQLite (cuenta: usuarios, tickets, etc.)
//	  tickets/<id>/  → por ticket: snapshot.json (estado completo) y attachments/
//	  logs/          → logs de la aplicación
type Config struct {
	Addr         string
	BaseURL      string
	DataDir      string
	DBPath       string
	TicketsDir   string
	AllowSignup  bool
	SessionTTL   time.Duration
	CookieSecure bool
	TrustProxy   bool
	MaxUploadMB  int64
	SMTP         SMTP
}

// TicketDir devuelve la subcarpeta de un ticket: <TicketsDir>/<id>.
// Cada ticket se aísla en su propia carpeta para auditoría y migraciones.
func (c Config) TicketDir(id int64) string {
	return filepath.Join(c.TicketsDir, strconv.FormatInt(id, 10))
}

// AttachDir devuelve la carpeta de adjuntos de un ticket.
func (c Config) AttachDir(id int64) string {
	return filepath.Join(c.TicketDir(id), "attachments")
}

// SnapshotPath devuelve la ruta del snapshot JSON de un ticket.
func (c Config) SnapshotPath(id int64) string {
	return filepath.Join(c.TicketDir(id), "snapshot.json")
}

func Load() (Config, error) {
	dataDir := env("SD_DATA_DIR", "./data")
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		return Config{}, err
	}
	ttl, err := time.ParseDuration(env("SD_SESSION_TTL", "168h"))
	if err != nil {
		return Config{}, fmt.Errorf("SD_SESSION_TTL inválido: %w", err)
	}
	port, err := strconv.Atoi(env("SD_SMTP_PORT", "587"))
	if err != nil {
		return Config{}, fmt.Errorf("SD_SMTP_PORT inválido: %w", err)
	}
	maxUpload, err := strconv.ParseInt(env("SD_MAX_UPLOAD_MB", "10"), 10, 64)
	if err != nil {
		return Config{}, fmt.Errorf("SD_MAX_UPLOAD_MB inválido: %w", err)
	}
	addr := env("SD_ADDR", ":8080")
	baseURL := env("SD_BASE_URL", "http://localhost"+addr)
	if strings.HasPrefix(addr, ":") {
		baseURL = "http://localhost" + addr
	}
	ticketsDir := filepath.Join(abs, "tickets")
	return Config{
		Addr:         addr,
		BaseURL:      env("SD_BASE_URL", baseURL),
		DataDir:      abs,
		DBPath:       filepath.Join(abs, "db", "servicedesk.db"),
		TicketsDir:   ticketsDir,
		AllowSignup:  envBool("SD_ALLOW_SIGNUP", true),
		SessionTTL:   ttl,
		CookieSecure: envBool("SD_COOKIE_SECURE", false),
		TrustProxy:   envBool("SD_TRUST_PROXY", false),
		MaxUploadMB:  maxUpload,
		SMTP: SMTP{
			Host:     env("SD_SMTP_HOST", ""),
			Port:     port,
			User:     env("SD_SMTP_USER", ""),
			Pass:     env("SD_SMTP_PASS", ""),
			From:     env("SD_SMTP_FROM", "servicedesk@localhost"),
			StartTLS: envBool("SD_SMTP_STARTTLS", true),
		},
	}, nil
}

func env(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func envBool(k string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}
