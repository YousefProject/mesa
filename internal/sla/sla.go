package sla

import (
	"time"

	"github.com/YousefProject/servicedesk/internal/model"
)

type Clock func() time.Time

func Now() time.Time { return time.Now().UTC() }

// Due calcula los vencimientos de respuesta y resolución a partir de la
// política del SLA y el momento de creación del ticket.
func Due(from time.Time, p model.SLAPolicy) (response, resolution time.Time) {
	return from.Add(time.Duration(p.ResponseMinutes) * time.Minute),
		from.Add(time.Duration(p.ResolutionMinutes) * time.Minute)
}

// Recompute recalcula los vencimientos cuando cambia la prioridad, manteniendo
// el momento de creación como origen del cómputo.
func Recompute(created time.Time, p model.SLAPolicy) (response, resolution time.Time) {
	return Due(created, p)
}

type State string

const (
	StateOK       State = "ok"
	StateDueSoon  State = "due_soon"
	StateBreached State = "breached"
	StateDone     State = "done"
)

// Evaluate clasifica el estado de SLA de un ticket en un instante dado.
// El umbral indica a partir de qué fracción del plazo se considera "en riesgo".
func Evaluate(t model.Ticket, now time.Time, threshold float64) State {
	if !t.Status.Open() {
		if t.ResolvedAt != nil && t.DueResolution != nil && t.ResolvedAt.After(*t.DueResolution) {
			return StateBreached
		}
		return StateDone
	}
	if t.ResolutionBreached(now) || t.ResponseBreached(now) {
		return StateBreached
	}
	if t.ResolutionProgress(now) >= threshold {
		return StateDueSoon
	}
	return StateOK
}
