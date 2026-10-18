package web

import (
	"context"
	"time"
)

// RunWorkers arranca las tareas de fondo: limpieza de sesiones y vigilancia de SLA.
func (s *Server) RunWorkers(ctx context.Context) {
	sessionTicker := time.NewTicker(30 * time.Minute)
	slaTicker := time.NewTicker(time.Minute)
	defer sessionTicker.Stop()
	defer slaTicker.Stop()

	s.checkSLA()
	for {
		select {
		case <-ctx.Done():
			return
		case <-sessionTicker.C:
			if n, err := s.store.PurgeExpiredSessions(); err != nil {
				s.logger.Warn("limpieza de sesiones", "err", err)
			} else if n > 0 {
				s.logger.Info("sesiones expiradas eliminadas", "n", n)
			}
		case <-slaTicker.C:
			s.checkSLA()
		}
	}
}

func (s *Server) checkSLA() {
	tickets, err := s.store.BreachedTickets()
	if err != nil {
		s.logger.Warn("vigilancia SLA", "err", err)
		return
	}
	for i := range tickets {
		t := &tickets[i]
		if err := s.store.MarkSLABreached(t.ID); err != nil {
			s.logger.Warn("marcar SLA", "ticket", t.Ref, "err", err)
			continue
		}
		s.store.Audit(nil, "sla.breached", "ticket", t.ID, "SLA incumplido")
		s.notifyBreach(t)
		s.logger.Info("SLA incumplido", "ref", t.Ref, "prioridad", t.Priority)
	}
}
