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
	ID            int64
	Ref           string
	Subject       string
	Description   string
	Status        Status
	Priority      Priority
	QueueID       *int64
	RequesterID   int64
	AssigneeID    *int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DueResponse   *time.Time
	DueResolution *time.Time
	FirstResponse *time.Time
	ResolvedAt    *time.Time
	ClosedAt      *time.Time
	SLABreached   bool

	QueueName     string
	RequesterName string
	RequesterMail string
	AssigneeName  string
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
	ID        int64
	TicketID  int64
	AuthorID  int64
	Body      string
	Internal  bool
	CreatedAt time.Time

	AuthorName string
	AuthorRole Role
}

type Attachment struct {
	ID         int64
	TicketID   int64
	CommentID  *int64
	UploaderID int64
	Filename   string
	Size       int64
	MIME       string
	StoredPath string
	CreatedAt  time.Time

	UploaderName string
}

type AuditEvent struct {
	ID        int64
	ActorID   *int64
	Action    string
	Entity    string
	EntityID  int64
	Details   string
	CreatedAt time.Time

	ActorName string
}

type SLAPolicy struct {
	Priority          Priority
	ResponseMinutes   int
	ResolutionMinutes int
}

type Dashboard struct {
	OpenTotal     int
	Unassigned    int
	Breached      int
	ResolvedToday int
	ByStatus      map[Status]int
	ByPriority    map[Priority]int
	ByQueue       []QueueCount
	ByAgent       []AgentLoad
	AvgResolveMin float64
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
