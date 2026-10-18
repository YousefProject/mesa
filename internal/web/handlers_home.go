package web

import (
	"net/http"

	"github.com/YousefProject/servicedesk/internal/auth"
	"github.com/YousefProject/servicedesk/internal/model"
	"github.com/YousefProject/servicedesk/internal/store"
)

type dashboardView struct {
	base
	D          *model.Dashboard
	Recent     []model.Ticket
	Mine       []model.Ticket
	Statuses   []model.Status
	Priorities []model.Priority
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	u := s.authUser(w, r)
	if u == nil {
		return
	}
	if !u.Role.IsStaff() {
		tickets, err := s.store.ListTickets(store.TicketFilter{RequesterID: &u.ID, Limit: 50})
		if err != nil {
			s.logger.Error("mis tickets", "err", err)
		}
		s.page(w, r, http.StatusOK, "home_requester.html", struct {
			base
			Tickets []model.Ticket
		}{s.baseView(r, "Mis tickets"), tickets})
		return
	}
	s.handleDashboard(w, r)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	u := s.requireStaff(w, r)
	if u == nil {
		return
	}
	d, err := s.store.Dashboard()
	if err != nil {
		s.logger.Error("dashboard", "err", err)
		s.errorPage(w, r, http.StatusInternalServerError, "No se pudieron calcular los indicadores.")
		return
	}
	mine, err := s.store.ListTickets(store.TicketFilter{AssigneeID: &u.ID, Limit: 10})
	if err != nil {
		s.logger.Error("tickets asignados", "err", err)
	}
	recent, err := s.store.ListTickets(store.TicketFilter{Limit: 10})
	if err != nil {
		s.logger.Error("tickets recientes", "err", err)
	}
	s.page(w, r, http.StatusOK, "dashboard.html", dashboardView{
		base:       s.baseView(r, "Panel"),
		D:          d,
		Recent:     recent,
		Mine:       mine,
		Statuses:   model.Statuses(),
		Priorities: model.Priorities(),
	})
}

func (s *Server) currentUser(r *http.Request) *model.User { return auth.CurrentUser(r) }
