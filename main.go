package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/YousefProject/servicedesk/internal/auth"
	"github.com/YousefProject/servicedesk/internal/backup"
	"github.com/YousefProject/servicedesk/internal/config"
	"github.com/YousefProject/servicedesk/internal/db"
	"github.com/YousefProject/servicedesk/internal/mail"
	"github.com/YousefProject/servicedesk/internal/model"
	"github.com/YousefProject/servicedesk/internal/snapshot"
	"github.com/YousefProject/servicedesk/internal/store"
	"github.com/YousefProject/servicedesk/internal/web"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

type options struct {
	addr         string
	adminEmail   string
	adminName    string
	adminPass    string
	createAdmin  bool
	showVersion  bool
	buildSnaps   bool
	exportBackup string
	importBackup string
	overwrite    bool
}

func parseFlags() options {
	var o options
	flag.StringVar(&o.addr, "addr", "", "dirección de escucha (por defecto SD_ADDR o :8080)")
	flag.StringVar(&o.adminEmail, "admin-email", "", "correo del administrador a crear/actualizar")
	flag.StringVar(&o.adminName, "admin-name", "Administrador", "nombre del administrador")
	flag.StringVar(&o.adminPass, "admin-password", "", "contraseña del administrador (vacío = aleatoria)")
	flag.BoolVar(&o.createAdmin, "create-admin", false, "crea o actualiza el administrador y sale")
	flag.BoolVar(&o.showVersion, "version", false, "muestra la versión y sale")
	flag.BoolVar(&o.buildSnaps, "build-snapshots", false, "regenera data/tickets/<id>/snapshot.json para todos los tickets y sale")
	flag.StringVar(&o.exportBackup, "export-backup", "", "exporta todos los tickets a un ZIP (ruta de salida) y sale")
	flag.StringVar(&o.importBackup, "import-backup", "", "importa tickets desde un ZIP de backup y sale")
	flag.BoolVar(&o.overwrite, "overwrite", false, "con -import-backup: reemplaza los tickets existentes con el mismo ID")
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
	if err := os.MkdirAll(cfg.TicketsDir, 0o700); err != nil {
		fatal(logger, "crear directorio de tickets", err)
	}
	if err := os.MkdirAll(filepath.Join(cfg.DataDir, "logs"), 0o700); err != nil {
		fatal(logger, "crear directorio de logs", err)
	}
	if err := migrateDataLayout(logger, cfg); err != nil {
		fatal(logger, "migrar estructura de datos", err)
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

	// Ajustes guardados en la consola web: la BD es la fuente de verdad y
	// las variables de entorno solo aportan los valores iniciales. La flag
	// -addr es un override de una sola ejecución y gana sobre ambos.
	kv, err := st.Settings()
	if err != nil {
		fatal(logger, "leer ajustes", err)
	}
	if err := config.ApplySettings(&cfg, kv); err != nil {
		logger.Warn("ajustes guardados con valores inválidos", "err", err)
	}
	if opts.addr != "" {
		cfg.Addr = opts.addr
	}

	if err := bootstrapAdmin(logger, st, opts); err != nil {
		fatal(logger, "administrador", err)
	}
	if opts.createAdmin {
		return
	}

	if opts.buildSnaps {
		snaps := snapshot.New(st, cfg.TicketsDir, logger)
		rows, err := conn.Query(`SELECT id FROM tickets`)
		if err != nil {
			fatal(logger, "listar tickets", err)
		}
		var ids []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				fatal(logger, "leer ticket", err)
			}
			ids = append(ids, id)
		}
		rows.Close()
		for _, id := range ids {
			snaps.Write(id)
		}
		logger.Info("snapshots regenerados", "tickets", len(ids), "dir", cfg.TicketsDir)
		return
	}

	if opts.exportBackup != "" {
		snaps := snapshot.New(st, cfg.TicketsDir, logger)
		ids, err := st.TicketIDs()
		if err != nil {
			fatal(logger, "listar tickets", err)
		}
		for _, id := range ids {
			snaps.Write(id)
		}
		f, err := os.Create(opts.exportBackup)
		if err != nil {
			fatal(logger, "crear fichero de backup", err)
		}
		count, err := backup.Export(st, cfg.TicketsDir, version, f)
		closeErr := f.Close()
		if err != nil {
			fatal(logger, "exportar backup", err)
		}
		if closeErr != nil {
			fatal(logger, "cerrar fichero de backup", closeErr)
		}
		logger.Info("backup exportado", "fichero", opts.exportBackup, "tickets", count)
		return
	}

	if opts.importBackup != "" {
		f, err := os.Open(opts.importBackup)
		if err != nil {
			fatal(logger, "abrir backup", err)
		}
		info, err := f.Stat()
		if err != nil {
			f.Close()
			fatal(logger, "stat backup", err)
		}
		rep, err := backup.Import(st, cfg, f, info.Size(), backup.ImportOptions{
			Overwrite: opts.overwrite,
			Logger:    logger,
		})
		f.Close()
		if err != nil {
			fatal(logger, "importar backup", err)
		}
		for _, w := range rep.Warnings {
			logger.Warn("backup", "aviso", w)
		}
		logger.Info("backup importado",
			"encontrados", rep.Tickets, "importados", len(rep.Imported),
			"omitidos", len(rep.Skipped), "reemplazados", len(rep.Replaced))
		return
	}

	rt := config.NewRuntime(cfg)
	sender := mail.NewDynamic(func() config.SMTP { return rt.Get().SMTP }, logger)
	srv, err := web.New(rt, st, sender, logger, version)
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

	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		if errors.Is(err, syscall.EADDRINUSE) {
			fatal(logger, "servidor", fmt.Errorf("el puerto de %s ya está en uso; cierra la instancia en ejecución (pkill -x mesa) o cambia el puerto en Administración › Ajustes: %w", cfg.Addr, err))
		}
		fatal(logger, "servidor", err)
	}
	serveLn := func(l net.Listener) {
		go func() {
			if err := httpServer.Serve(l); err != nil &&
				!errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
				logger.Error("servidor", "err", err)
			}
		}()
	}
	serveLn(ln)

	// Cambio de puerto en caliente desde la consola: se abre el listener
	// nuevo antes de cerrar el anterior; si el puerto está ocupado, no se
	// guarda el ajuste y el servicio sigue en la dirección actual.
	var lnMu sync.Mutex
	curLn := ln
	srv.SetRebinder(func(addr string) error {
		lnMu.Lock()
		defer lnMu.Unlock()
		newLn, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("no se pudo escuchar en %s: %w", addr, err)
		}
		old := curLn
		curLn = newLn
		serveLn(newLn)
		if old != nil {
			_ = old.Close()
		}
		logger.Info("dirección de escucha cambiada", "addr", addr)
		return nil
	})

	{
		c := rt.Get()
		logger.Info("mesa de servicios escuchando",
			"addr", c.Addr, "db", c.DBPath, "smtp", c.SMTP.Enabled(), "signup", c.AllowSignup)
		if !c.SMTP.Enabled() {
			logger.Warn("SMTP no configurado: ajústalo en Administración › Ajustes")
		}
	}

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
// migrateDataLayout traslada la estructura plana antigua (data/servicedesk.db,
// data/attachments/<id>/) a la nueva disposición aislada por componente
// (data/db/, data/tickets/<id>/attachments/). Es idempotente: si la BD ya
// está en data/db/ o la vieja no existe, no hace nada.
func migrateDataLayout(logger *slog.Logger, cfg config.Config) error {
	oldDB := filepath.Join(cfg.DataDir, "servicedesk.db")
	if _, err := os.Stat(oldDB); err == nil {
		if _, err := os.Stat(cfg.DBPath); err != nil {
			if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o700); err != nil {
				return err
			}
			if err := os.Rename(oldDB, cfg.DBPath); err != nil {
				return err
			}
			// Los archivos WAL/SHM de la sesión anterior no son válidos tras el
			// renombrado; se descartan para forzar una reconstrucción limpia.
			for _, suffix := range []string{"-wal", "-shm"} {
				_ = os.Remove(oldDB + suffix)
			}
			logger.Info("base de datos movida", "de", oldDB, "a", cfg.DBPath)
		}
	}
	oldAttach := filepath.Join(cfg.DataDir, "attachments")
	if entries, err := os.ReadDir(oldAttach); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			id, err := strconv.ParseInt(e.Name(), 10, 64)
			if err != nil {
				continue // no es una carpeta de ticket: se ignora
			}
			src := filepath.Join(oldAttach, e.Name())
			dst := cfg.AttachDir(id)
			if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
				return err
			}
			if _, err := os.Stat(dst); err == nil {
				continue
			}
			if err := os.Rename(src, dst); err != nil {
				return fmt.Errorf("mover adjuntos del ticket %s: %w", e.Name(), err)
			}
			logger.Info("adjuntos movidos", "ticket", e.Name(), "a", dst)
		}
		_ = os.Remove(oldAttach)
	}
	return nil
}

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
	hash, err := auth.HashPassword("admin123")
	if err != nil {
		return err
	}
	user, err := st.CreateUser("admin", "Administrador", model.RoleAdmin, hash)
	if err != nil {
		return err
	}
	st.Audit(nil, "user.bootstrap", "user", user.ID, "administrador inicial")
	logger.Warn("administrador inicial creado con credenciales por defecto admin/admin123; cámbiala cuanto antes")
	fmt.Printf("\n  Administrador creado (credenciales por defecto):\n    usuario:    admin\n    contraseña: admin123\n  Cámbiala cuanto antes en Mi cuenta (/me).\n\n")
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
