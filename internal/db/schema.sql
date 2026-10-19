PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    email         TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    name          TEXT    NOT NULL,
    role          TEXT    NOT NULL DEFAULT 'requester',
    password_hash TEXT    NOT NULL,
    active        INTEGER NOT NULL DEFAULT 1,
    created_at    TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
    token      TEXT    PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    csrf       TEXT    NOT NULL,
    created_at TEXT    NOT NULL,
    expires_at TEXT    NOT NULL,
    user_agent TEXT    NOT NULL DEFAULT '',
    ip         TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);

CREATE TABLE IF NOT EXISTS queues (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    description TEXT    NOT NULL DEFAULT '',
    created_at  TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS tickets (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    ref             TEXT    NOT NULL UNIQUE,
    subject         TEXT    NOT NULL,
    description     TEXT    NOT NULL DEFAULT '',
    status          TEXT    NOT NULL DEFAULT 'new',
    priority        TEXT    NOT NULL DEFAULT 'normal',
    queue_id        INTEGER REFERENCES queues(id) ON DELETE SET NULL,
    requester_id    INTEGER NOT NULL REFERENCES users(id),
    assignee_id     INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at      TEXT    NOT NULL,
    updated_at      TEXT    NOT NULL,
    due_response_at TEXT,
    due_resolution_at TEXT,
    first_response  TEXT,
    resolved_at     TEXT,
    closed_at       TEXT,
    sla_breached    INTEGER NOT NULL DEFAULT 0,
    sla_notified    INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_tickets_status ON tickets(status);
CREATE INDEX IF NOT EXISTS idx_tickets_assignee ON tickets(assignee_id);
CREATE INDEX IF NOT EXISTS idx_tickets_queue ON tickets(queue_id);
CREATE INDEX IF NOT EXISTS idx_tickets_requester ON tickets(requester_id);
CREATE INDEX IF NOT EXISTS idx_tickets_updated ON tickets(updated_at);
CREATE INDEX IF NOT EXISTS idx_tickets_created ON tickets(created_at);
CREATE INDEX IF NOT EXISTS idx_tickets_status_updated ON tickets(status, updated_at);
CREATE INDEX IF NOT EXISTS idx_tickets_assignee_status ON tickets(assignee_id, status);
CREATE INDEX IF NOT EXISTS idx_tickets_requester_updated ON tickets(requester_id, updated_at);
CREATE INDEX IF NOT EXISTS idx_tickets_sla_breached ON tickets(sla_breached) WHERE sla_breached = 1;
CREATE INDEX IF NOT EXISTS idx_tickets_due_response ON tickets(due_response_at) WHERE due_response_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_tickets_due_resolution ON tickets(due_resolution_at) WHERE due_resolution_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS comments (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    ticket_id  INTEGER NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    author_id  INTEGER NOT NULL REFERENCES users(id),
    body       TEXT    NOT NULL,
    internal   INTEGER NOT NULL DEFAULT 0,
    created_at TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_comments_ticket ON comments(ticket_id, created_at);
CREATE INDEX IF NOT EXISTS idx_comments_author ON comments(author_id);

CREATE TABLE IF NOT EXISTS attachments (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    ticket_id   INTEGER NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    comment_id  INTEGER REFERENCES comments(id) ON DELETE SET NULL,
    uploader_id INTEGER NOT NULL REFERENCES users(id),
    filename    TEXT    NOT NULL,
    size        INTEGER NOT NULL,
    mime        TEXT    NOT NULL DEFAULT '',
    stored_path TEXT    NOT NULL,
    created_at  TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_attachments_ticket ON attachments(ticket_id);

CREATE TABLE IF NOT EXISTS audit_events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    actor_id   INTEGER REFERENCES users(id) ON DELETE SET NULL,
    action     TEXT    NOT NULL,
    entity     TEXT    NOT NULL,
    entity_id  INTEGER NOT NULL DEFAULT 0,
    details    TEXT    NOT NULL DEFAULT '',
    created_at TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_entity ON audit_events(entity, entity_id, created_at);
CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_events(created_at);
CREATE INDEX IF NOT EXISTS idx_audit_actor ON audit_events(actor_id);
CREATE INDEX IF NOT EXISTS idx_audit_action ON audit_events(action, created_at);

CREATE TABLE IF NOT EXISTS sla_policies (
    priority           TEXT PRIMARY KEY,
    response_minutes   INTEGER NOT NULL,
    resolution_minutes INTEGER NOT NULL
);

INSERT OR IGNORE INTO sla_policies (priority, response_minutes, resolution_minutes) VALUES
    ('urgent', 15, 240),
    ('high', 60, 480),
    ('normal', 240, 1440),
    ('low', 480, 2880);

INSERT OR IGNORE INTO queues (id, name, description, created_at) VALUES
    (1, 'Soporte', 'Incidencias y peticiones generales', '2024-01-01T00:00:00Z');

-- Ajustes de la aplicación editables desde la consola web. La capa de
-- configuración (internal/config) usa estos valores como fuente de verdad y
-- recurre a las variables de entorno solo como valores por defecto iniciales.
CREATE TABLE IF NOT EXISTS settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
