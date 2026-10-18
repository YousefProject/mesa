package web

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/YousefProject/servicedesk/internal/backup"
)

type adminBackupView struct {
	base
}

func (s *Server) handleAdminBackupPage(w http.ResponseWriter, r *http.Request) {
	if s.requireAdmin(w, r) == nil {
		return
	}
	s.page(w, r, http.StatusOK, "admin_backup.html", adminBackupView{
		base: s.baseView(r, "Copia de seguridad"),
	})
}

// handleAdminBackupExport descarga un ZIP con el snapshot de todos los
// tickets. Regenera los snapshots antes de empaquetarlos para que el backup
// refleje el estado actual.
func (s *Server) handleAdminBackupExport(w http.ResponseWriter, r *http.Request) {
	admin := s.requireAdmin(w, r)
	if admin == nil {
		return
	}
	ids, err := s.store.TicketIDs()
	if err != nil {
		s.logger.Error("backup: listar tickets", "err", err)
		s.errorPage(w, r, http.StatusInternalServerError, "No se pudo generar la copia.")
		return
	}
	for _, id := range ids {
		s.snaps.Write(id)
	}
	name := fmt.Sprintf("mesa-backup-%s.zip", time.Now().UTC().Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	if _, err := backup.Export(s.store, s.cfg.TicketsDir, s.version, w); err != nil {
		// Los headers ya se enviaron: solo se registra.
		s.logger.Error("backup: exportar", "err", err)
		return
	}
	s.store.Audit(&admin.ID, "backup.export", "system", 0,
		fmt.Sprintf("%d ticket(s)", len(ids)))
}

// handleAdminBackupImport recibe un ZIP de backup y reconstruye sus tickets.
func (s *Server) handleAdminBackupImport(w http.ResponseWriter, r *http.Request) {
	admin := s.requireAdmin(w, r)
	if admin == nil {
		return
	}
	maxBytes := s.cfg.MaxUploadMB * 1024 * 1024
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+maxBytes/10)
	if err := r.ParseMultipartForm(maxBytes); err != nil {
		s.redirect(w, r, "/admin/backup?err="+qr("el archivo es demasiado grande (máx "+strconv.FormatInt(s.cfg.MaxUploadMB, 10)+" MB)"))
		return
	}
	file, _, err := r.FormFile("backup")
	if err != nil {
		s.redirect(w, r, "/admin/backup?err="+qr("no se ha adjuntado ningún archivo"))
		return
	}
	defer file.Close()
	tmp, err := io.ReadAll(io.LimitReader(file, maxBytes))
	if err != nil {
		s.redirect(w, r, "/admin/backup?err="+qr("no se pudo leer el archivo"))
		return
	}
	rep, err := backup.Import(s.store, s.cfg, readerAt(tmp), int64(len(tmp)), backup.ImportOptions{
		Overwrite: r.PostFormValue("overwrite") == "1",
		Logger:    s.logger,
	})
	if err != nil {
		s.logger.Error("backup: importar", "err", err)
		s.redirect(w, r, "/admin/backup?err="+qr(err.Error()))
		return
	}
	s.store.Audit(&admin.ID, "backup.import", "system", 0,
		fmt.Sprintf("importados=%d omitidos=%d reemplazados=%d", len(rep.Imported), len(rep.Skipped), len(rep.Replaced)))
	msg := fmt.Sprintf("importados %d, omitidos %d, reemplazados %d", len(rep.Imported), len(rep.Skipped), len(rep.Replaced))
	if len(rep.Warnings) > 0 {
		msg += fmt.Sprintf(" (%d avisos)", len(rep.Warnings))
	}
	s.redirect(w, r, "/admin/backup?ok="+qr(msg))
}

type readerAt []byte

func (b readerAt) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(b)) {
		return 0, io.EOF
	}
	n := copy(p, b[off:])
	return n, nil
}
