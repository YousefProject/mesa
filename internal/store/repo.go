package store

import (
	"time"

	"github.com/YousefProject/servicedesk/internal/model"
)

// Repo agrupa todas las operaciones de persistencia que usa la capa web.
// Hoy la implementa *Store (SQLite). Para migrar a otra base de datos basta
// con implementar esta interface con otro driver (p. ej. Postgres) sin tocar
// los handlers; cada dominio (usuarios, tickets, colas, auditoría) vive en su
// archivo para poder migrarlo por separado.
type Repo interface {
	// Usuarios y sesiones (users.go, sessions.go)
	CreateUser(email, name string, role model.Role, passwordHash string) (*model.User, error)
	UserByID(id int64) (*model.User, error)
	UserByEmail(email string) (*model.User, string, error)
	Users() ([]model.User, error)
	Staff() ([]model.User, error)
	UpdateUserRole(id int64, role model.Role) error
	SetUserActive(id int64, active bool) error
	UpdateUserProfile(id int64, name, email string) error
	SetPassword(id int64, hash string) error
	PasswordHash(id int64) (string, error)
	CountUsers() (int, error)
	CreateSession(token string, userID int64, csrf string, expires time.Time, ua, ip string) error
	Session(token string) (*Session, *model.User, error)
	DeleteSession(token string) error
	DeleteUserSessions(userID int64) error
	PurgeExpiredSessions() (int64, error)

	// Tickets (tickets.go)
	CreateTicket(in NewTicket) (*model.Ticket, error)
	TicketByID(id int64) (*model.Ticket, error)
	ListTickets(f TicketFilter) ([]model.Ticket, error)
	CountTickets(f TicketFilter) (int, error)
	SetStatus(id int64, status model.Status) error
	SetPriority(id int64, p model.Priority, dueResponse, dueResolution time.Time) error
	SetAssignee(id int64, assignee *int64) error
	SetQueue(id int64, queue *int64) error
	UpdateTicket(id int64, subject, description string) error
	MarkFirstResponse(id int64) error
	BreachedTickets() ([]model.Ticket, error)
	MarkSLABreached(id int64) error

	// Comentarios y adjuntos (comments.go)
	AddComment(ticketID, authorID int64, body string, internal bool) (*model.Comment, error)
	CommentByID(id int64) (*model.Comment, error)
	Comments(ticketID int64, includeInternal bool) ([]model.Comment, error)
	AddAttachment(ticketID int64, commentID *int64, uploaderID int64, filename string, size int64, mime, storedPath string) (*model.Attachment, error)
	AttachmentByID(id int64) (*model.Attachment, error)
	Attachments(ticketID int64) ([]model.Attachment, error)
	AuditFor(entity string, entityID int64, limit int) ([]model.AuditEvent, error)
	RecentAudit(limit int) ([]model.AuditEvent, error)

	// Colas, SLA y métricas (misc.go)
	Queues() ([]model.Queue, error)
	QueueByID(id int64) (*model.Queue, error)
	QueueByName(name string) (*model.Queue, error)
	CreateQueue(name, description string) (*model.Queue, error)
	SLAPolicies() ([]model.SLAPolicy, error)
	SLAPolicy(p model.Priority) (model.SLAPolicy, error)
	UpdateSLAPolicy(p model.SLAPolicy) error
	Dashboard() (*model.Dashboard, error)

	// Plantillas de ticket (templates.go)
	Templates(includeInactive bool) ([]model.TicketTemplate, error)
	TemplateByID(id int64) (*model.TicketTemplate, error)
	CreateTemplate(in NewTemplate) (*model.TicketTemplate, error)
	UpdateTemplate(id int64, in NewTemplate) error
	DeleteTemplate(id int64) error

	// Auditoría (store.go)
	Audit(actorID *int64, action, entity string, entityID int64, details string)

	// Ajustes de la aplicación (settings.go)
	GetSetting(key string) (string, error)
	SetSetting(key, value string) error
	Settings() (map[string]string, error)
	SetSettings(m map[string]string) error

	// Restauración de backups (restore.go)
	TicketIDs() ([]int64, error)
	RestoreTicket(t *model.Ticket) error
	RestoreComment(c *model.Comment) (int64, error)
	RestoreAttachment(a *model.Attachment) error
	RestoreAudit(e *model.AuditEvent) error
	DeleteTicket(id int64) error
	CreateUserWithID(id int64, email, name string, role model.Role, passwordHash string) error
}

var _ Repo = (*Store)(nil)
