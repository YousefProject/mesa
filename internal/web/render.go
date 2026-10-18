package web

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/YousefProject/servicedesk/internal/model"
	"github.com/YousefProject/servicedesk/internal/sla"
)

//go:embed templates/*.html static/*
var assets embed.FS

const slaThreshold = 0.75

var funcs = template.FuncMap{
	"fmtTime": func(t time.Time) string {
		if t.IsZero() {
			return "—"
		}
		return t.Local().Format("02 Jan 2006 15:04")
	},
	"humanAgo": func(t time.Time) string {
		if t.IsZero() {
			return "—"
		}
		return humanDuration(time.Since(t))
	},
	"dueIn": func(t *time.Time, now time.Time) string {
		if t == nil {
			return "sin SLA"
		}
		d := t.Sub(now)
		if d < 0 {
			return "hace " + humanDuration(-d)
		}
		return "en " + humanDuration(d)
	},
	"humanDuration": humanDuration,
	"statusLabel":   statusLabel,
	"statusClass":   statusClass,
	"prioLabel":     prioLabel,
	"prioClass":     prioClass,
	"roleLabel":     roleLabel,
	"isAdmin":       func(u *model.User) bool { return u != nil && u.Role == model.RoleAdmin },
	"isStaff":       func(u *model.User) bool { return u != nil && u.Role.IsStaff() },
	"slaState": func(t model.Ticket, now time.Time) string {
		return string(sla.Evaluate(t, now, slaThreshold))
	},
	"percent": func(f float64) int { return int(f*100 + 0.5) },
	"ratio": func(n, total int) float64 {
		if total <= 0 {
			return 0
		}
		r := float64(n) / float64(total)
		if r > 1 {
			return 1
		}
		return r
	},
	"durationMin": func(f float64) time.Duration { return time.Duration(f * float64(time.Minute)) },
	"initials": func(name string) string {
		parts := strings.Fields(name)
		switch len(parts) {
		case 0:
			return "?"
		case 1:
			return strings.ToUpper(string([]rune(parts[0])[0:1]))
		default:
			return strings.ToUpper(string([]rune(parts[0])[0:1]) + string([]rune(parts[len(parts)-1])[0:1]))
		}
	},
	"humanSize": func(n int64) string {
		const unit = 1024
		if n < unit {
			return fmt.Sprintf("%d B", n)
		}
		div, exp := int64(unit), 0
		for n/div >= unit {
			div *= unit
			exp++
		}
		return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "kMGTPE"[exp])
	},
	"ptrEq": func(p *int64, v int64) bool {
		return p != nil && *p == v
	},
	"isRole": func(u *model.User, r model.Role) bool {
		return u != nil && u.Role == r
	},
	"hasPrefix": strings.HasPrefix,
	"add":       func(a, b int) int { return a + b },
	"sub":       func(a, b int) int { return a - b },
	"openCount": func(d *model.Dashboard) int { return d.OpenTotal },
	"dict": func(kv ...any) map[string]any {
		m := map[string]any{}
		for i := 0; i+1 < len(kv); i += 2 {
			key, ok := kv[i].(string)
			if !ok {
				continue
			}
			m[key] = kv[i+1]
		}
		return m
	},
}

type Renderer struct {
	base *template.Template
}

func NewRenderer() (*Renderer, error) {
	base, err := template.New("root").Funcs(funcs).ParseFS(assets, "templates/layout.html", "templates/partials.html")
	if err != nil {
		return nil, err
	}
	return &Renderer{base: base}, nil
}

func (r *Renderer) Page(w io.Writer, page string, data any) error {
	t, err := r.base.Clone()
	if err != nil {
		return err
	}
	if _, err := t.ParseFS(assets, path.Join("templates", page)); err != nil {
		return err
	}
	return t.ExecuteTemplate(w, "layout", data)
}

func (r *Renderer) Partial(w io.Writer, name string, data any) error {
	return r.base.ExecuteTemplate(w, name, data)
}

func StaticFS() fs.FS {
	sub, err := fs.Sub(assets, "static")
	if err != nil {
		panic(err)
	}
	return sub
}

type base struct {
	Title   string
	User    *model.User
	CSRF    string
	Now     time.Time
	Error   string
	Flash   string
	Path    string
	Version string
}

func statusLabel(s model.Status) string {
	switch s {
	case model.StatusNew:
		return "Nuevo"
	case model.StatusAssigned:
		return "Asignado"
	case model.StatusInProgress:
		return "En progreso"
	case model.StatusWaiting:
		return "En espera"
	case model.StatusResolved:
		return "Resuelto"
	case model.StatusClosed:
		return "Cerrado"
	}
	return string(s)
}

func statusClass(s model.Status) string {
	switch s {
	case model.StatusNew:
		return "st-new"
	case model.StatusAssigned:
		return "st-assigned"
	case model.StatusInProgress:
		return "st-progress"
	case model.StatusWaiting:
		return "st-waiting"
	case model.StatusResolved:
		return "st-resolved"
	case model.StatusClosed:
		return "st-closed"
	}
	return ""
}

func prioLabel(p model.Priority) string {
	switch p {
	case model.PriorityLow:
		return "Baja"
	case model.PriorityNormal:
		return "Media"
	case model.PriorityHigh:
		return "Alta"
	case model.PriorityUrgent:
		return "Urgente"
	}
	return string(p)
}

func prioClass(p model.Priority) string {
	switch p {
	case model.PriorityLow:
		return "prio-low"
	case model.PriorityNormal:
		return "prio-normal"
	case model.PriorityHigh:
		return "prio-high"
	case model.PriorityUrgent:
		return "prio-urgent"
	}
	return ""
}

func roleLabel(r model.Role) string {
	switch r {
	case model.RoleAdmin:
		return "Administrador"
	case model.RoleAgent:
		return "Agente"
	default:
		return "Solicitante"
	}
}

func humanDuration(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
