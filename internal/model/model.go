package model

import "time"

type Role string

const (
	RoleAdmin     Role = "admin"
	RoleAgent     Role = "agent"
	RoleRequester Role = "requester"
)

func (r Role) Valid() bool {
	switch r {
	case RoleAdmin, RoleAgent, RoleRequester:
		return true
	}
	return false
}

func (r Role) String() string { return string(r) }

func (r Role) IsStaff() bool { return r == RoleAdmin || r == RoleAgent }

type Status string

const (
	StatusNew        Status = "new"
	StatusAssigned   Status = "assigned"
	StatusInProgress Status = "in_progress"
	StatusWaiting    Status = "waiting"
	StatusResolved   Status = "resolved"
	StatusClosed     Status = "closed"
)

var statusOrder = []Status{StatusNew, StatusAssigned, StatusInProgress, StatusWaiting, StatusResolved, StatusClosed}

func Statuses() []Status { return statusOrder }

func (s Status) Valid() bool {
	for _, x := range statusOrder {
		if x == s {
			return true
		}
	}
	return false
}

func (s Status) Open() bool {
	switch s {
	case StatusResolved, StatusClosed:
		return false
	}
	return true
}

func (s Status) String() string { return string(s) }

type Priority string

const (
	PriorityLow    Priority = "low"
	PriorityNormal Priority = "normal"
	PriorityHigh   Priority = "high"
	PriorityUrgent Priority = "urgent"
)

var priorityOrder = []Priority{PriorityLow, PriorityNormal, PriorityHigh, PriorityUrgent}

func Priorities() []Priority { return priorityOrder }

func (p Priority) Valid() bool {
	for _, x := range priorityOrder {
		if x == p {
			return true
		}
	}
	return false
}

func (p Priority) String() string { return string(p) }

type User struct {
	ID        int64
	Email     string
	Name      string
	Role      Role
	Active    bool
	CreatedAt time.Time
}

type Queue struct {
	ID          int64
	Name        string
	Description string
	CreatedAt   time.Time
	TicketCount int
}

type Ticket struct {
	ID            int64      `json:"id"`
	Ref           string     `json:"ref"`
	Subject       string     `json:"subject"`
	Description   string     `json:"description"`
	Status        Status     `json:"status"`
	Priority      Priority   `json:"priority"`
	QueueID       *int64     `json:"queue_id,omitempty"`
	RequesterID   int64      `json:"requester_id"`
	AssigneeID    *int64     `json:"assignee_id,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	DueResponse   *time.Time `json:"due_response_at,omitempty"`
	DueResolution *time.Time `json:"due_resolution_at,omitempty"`
	FirstResponse *time.Time `json:"first_response_at,omitempty"`
	ResolvedAt    *time.Time `json:"resolved_at,omitempty"`
	ClosedAt      *time.Time `json:"closed_at,omitempty"`
	SLABreached   bool       `json:"sla_breached"`

	QueueName     string `json:"queue_name,omitempty"`
	RequesterName string `json:"requester_name,omitempty"`
	RequesterMail string `json:"requester_mail,omitempty"`
	AssigneeName  string `json:"assignee_name,omitempty"`
}

func (t Ticket) ResponseBreached(now time.Time) bool {
	return t.Status.Open() && t.FirstResponse == nil && t.DueResponse != nil && now.After(*t.DueResponse)
}

func (t Ticket) ResolutionBreached(now time.Time) bool {
	return t.Status.Open() && t.DueResolution != nil && now.After(*t.DueResolution)
}

func (t Ticket) Breached(now time.Time) bool {
	return t.SLABreached || t.ResponseBreached(now) || t.ResolutionBreached(now)
}

func (t Ticket) ResponseProgress(now time.Time) float64 {
	if t.DueResponse == nil {
		return 0
	}
	return progress(t.CreatedAt, *t.DueResponse, t.FirstResponse, now)
}

func (t Ticket) ResolutionProgress(now time.Time) float64 {
	if t.DueResolution == nil {
		return 0
	}
	return progress(t.CreatedAt, *t.DueResolution, t.ResolvedAt, now)
}

func progress(start, due time.Time, done *time.Time, now time.Time) float64 {
	end := now
	if done != nil {
		end = *done
	}
	total := due.Sub(start)
	if total <= 0 {
		return 1
	}
	p := end.Sub(start).Seconds() / total.Seconds()
	if p < 0 {
		return 0
	}
	if p > 1 {
		return 1
	}
	return p
}

type Comment struct {
	ID        int64     `json:"id"`
	TicketID  int64     `json:"ticket_id"`
	AuthorID  int64     `json:"author_id"`
	Body      string    `json:"body"`
	Internal  bool      `json:"internal"`
	CreatedAt time.Time `json:"created_at"`

	AuthorName string `json:"author_name,omitempty"`
	AuthorRole Role   `json:"author_role,omitempty"`
}

type Attachment struct {
	ID         int64     `json:"id"`
	TicketID   int64     `json:"ticket_id"`
	CommentID  *int64    `json:"comment_id,omitempty"`
	UploaderID int64     `json:"uploader_id"`
	Filename   string    `json:"filename"`
	Size       int64     `json:"size"`
	MIME       string    `json:"mime"`
	StoredPath string    `json:"stored_path"`
	CreatedAt  time.Time `json:"created_at"`

	UploaderName string `json:"uploader_name,omitempty"`
}

type AuditEvent struct {
	ID        int64     `json:"id"`
	ActorID   *int64    `json:"actor_id,omitempty"`
	Action    string    `json:"action"`
	Entity    string    `json:"entity"`
	EntityID  int64     `json:"entity_id"`
	Details   string    `json:"details"`
	CreatedAt time.Time `json:"created_at"`

	ActorName string `json:"actor_name,omitempty"`
}

type SLAPolicy struct {
	Priority          Priority
	ResponseMinutes   int
	ResolutionMinutes int
}

type Dashboard struct {
	OpenTotal       int
	Unassigned      int
	Breached        int
	ResolvedToday   int
	CreatedToday    int
	Open7d          []DayCount
	Resolved7d      []DayCount
	SLACompliance   float64 // % de tickets resueltos sin incumplir SLA (últimos 30 días)
	ByStatus        map[Status]int
	ByPriority      map[Priority]int
	ByQueue         []QueueCount
	ByAgent         []AgentLoad
	AvgResolveMin   float64
	AvgFirstRespMin float64
}

// DayCount es una fila de la tabla de actividad diaria del dashboard.
type DayCount struct {
	Day   string `json:"day"`
	Count int    `json:"count"`
}

type QueueCount struct {
	Name  string
	Count int
}

type AgentLoad struct {
	Name       string
	Open       int
	Breached   int
	Resolved7d int
}
