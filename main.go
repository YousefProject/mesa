package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/YousefProject/servicedesk/internal/auth"
	"github.com/YousefProject/servicedesk/internal/config"
	"github.com/YousefProject/servicedesk/internal/db"
	"github.com/YousefProject/servicedesk/internal/mail"
	"github.com/YousefProject/servicedesk/internal/model"
	"github.com/YousefProject/servicedesk/internal/store"
	"github.com/YousefProject/servicedesk/internal/web"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

type options struct {
	addr        string
	adminEmail  string
	adminName   string
	adminPass   string
	createAdmin bool
	showVersion bool
}

func parseFlags() options {
	var o options
	flag.StringVar(&o.addr, "addr", "", "dirección de escucha (por defecto SD_ADDR o :8080)")
	flag.StringVar(&o.adminEmail, "admin-email", "", "correo del administrador a crear/actualizar")
	flag.StringVar(&o.adminName, "admin-name", "Administrador", "nombre del administrador")
	flag.StringVar(&o.adminPass, "admin-password", "", "contraseña del administrador (vacío = aleatoria)")
	flag.BoolVar(&o.createAdmin, "create-admin", false, "crea o actualiza el administrador y sale")
	flag.BoolVar(&o.showVersion, "version", false, "muestra la versión y sale")
	flag.Parse()
	return o
}

func main() {
	opts := parseFlags()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if opts.showVersion {
		fmt.Printf("servicedesk %s (commit %s, build %s)\n", version, commit, date)
		return
	}

	cfg, err := config.Load()
	if err != nil {
		fatal(logger, "configuración", err)
	}
	if opts.addr != "" {
		cfg.Addr = opts.addr
	}
	if err := os.MkdirAll(cfg.AttachDir, 0o700); err != nil {
		fatal(logger, "crear directorio de adjuntos", err)
	}

	conn, err := db.Open(cfg.DBPath)
	if err != nil {
		fatal(logger, "abrir base de datos", err)
	}
	defer conn.Close()
	if err := db.Migrate(conn); err != nil {
		fatal(logger, "migrar", err)
	}
	st := store.New(conn)

	if err := bootstrapAdmin(logger, st, opts); err != nil {
		fatal(logger, "administrador", err)
	}
	if opts.createAdmin {
		return
	}

	sender := mail.New(cfg.SMTP, logger)
	srv, err := web.New(cfg, st, sender, logger, version)
	if err != nil {
		fatal(logger, "servidor web", err)
	}

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go srv.RunWorkers(ctx)

	go func() {
		logger.Info("mesa de servicios escuchando",
			"addr", cfg.Addr, "db", cfg.DBPath, "smtp", cfg.SMTP.Enabled(), "signup", cfg.AllowSignup)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fatal(logger, "servidor", err)
		}
	}()

	<-ctx.Done()
	logger.Info("apagando…")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("apagado", "err", err)
	}
}

// bootstrapAdmin crea el primer administrador si no hay usuarios, o actualiza el
// administrador indicado con -admin-email/-admin-password.
func bootstrapAdmin(logger *slog.Logger, st *store.Store, opts options) error {
	n, err := st.CountUsers()
	if err != nil {
		return err
	}
	email := auth.NormalizeEmail(firstNonEmpty(opts.adminEmail, os.Getenv("SD_ADMIN_EMAIL")))
	password := strings.TrimSpace(firstNonEmpty(opts.adminPass, os.Getenv("SD_ADMIN_PASSWORD")))

	if email != "" {
		hash, err := auth.HashPassword(password)
		if err != nil {
			return err
		}
		if existing, _, err := st.UserByEmail(email); err == nil {
			if err := st.SetPassword(existing.ID, hash); err != nil {
				return err
			}
			if err := st.UpdateUserRole(existing.ID, model.RoleAdmin); err != nil {
				return err
			}
			st.Audit(nil, "user.bootstrap", "user", existing.ID, "administrador actualizado")
			fmt.Printf("Administrador %s actualizado.\n", email)
			return nil
		}
		user, err := st.CreateUser(email, firstNonEmpty(strings.TrimSpace(opts.adminName), "Administrador"), model.RoleAdmin, hash)
		if err != nil {
			return err
		}
		st.Audit(nil, "user.bootstrap", "user", user.ID, "administrador creado")
		fmt.Printf("Administrador %s creado.\n", email)
		return nil
	}

	if n > 0 {
		return nil
	}
	generated := auth.NewToken()[:16]
	hash, err := auth.HashPassword(generated)
	if err != nil {
		return err
	}
	user, err := st.CreateUser("admin@localhost", "Administrador", model.RoleAdmin, hash)
	if err != nil {
		return err
	}
	st.Audit(nil, "user.bootstrap", "user", user.ID, "administrador inicial")
	logger.Warn("administrador inicial creado con contraseña aleatoria")
	fmt.Printf("\n  Administrador creado:\n    usuario:    admin@localhost\n    contraseña: %s\n  Cámbiala en /me o crea otro con -create-admin.\n\n", generated)
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func fatal(logger *slog.Logger, msg string, err error) {
	logger.Error(msg, "err", err)
	os.Exit(1)
}
