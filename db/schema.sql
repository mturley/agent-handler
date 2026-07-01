CREATE TABLE IF NOT EXISTS events (
    id TEXT PRIMARY KEY,
    ts TEXT NOT NULL,
    external_ts TEXT,
    source TEXT NOT NULL,
    session_id TEXT,
    type TEXT NOT NULL,
    title TEXT NOT NULL,
    body TEXT,
    author TEXT,
    author_type TEXT,
    broadcast INTEGER NOT NULL DEFAULT 0,
    tags TEXT
);

CREATE INDEX IF NOT EXISTS idx_events_ts ON events(ts);
CREATE INDEX IF NOT EXISTS idx_events_source_type ON events(source, type);
CREATE INDEX IF NOT EXISTS idx_events_session_id ON events(session_id);

CREATE TABLE IF NOT EXISTS event_recipients (
    event_id TEXT NOT NULL REFERENCES events(id),
    recipient_type TEXT NOT NULL,
    recipient_value TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_event_recipients_target ON event_recipients(recipient_type, recipient_value);

CREATE TABLE IF NOT EXISTS event_resources (
    event_id TEXT NOT NULL REFERENCES events(id),
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    resource_url TEXT
);

CREATE INDEX IF NOT EXISTS idx_event_resources_resource ON event_resources(resource_type, resource_id);

CREATE TABLE IF NOT EXISTS sessions (
    session_id TEXT PRIMARY KEY,
    harness TEXT NOT NULL DEFAULT 'claude',
    repo TEXT NOT NULL,
    branch TEXT NOT NULL,
    session_name TEXT,
    pid INTEGER,
    status TEXT NOT NULL DEFAULT 'active',
    inbox_mode TEXT NOT NULL DEFAULT 'manual',
    auto_poll_interval INTEGER,
    role TEXT,
    last_active TEXT NOT NULL,
    registered_at TEXT NOT NULL,
    jsonl_path TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS session_cursors (
    session_id TEXT PRIMARY KEY REFERENCES sessions(session_id),
    last_seen_ts TEXT NOT NULL,
    human_seen_ts TEXT
);


CREATE TABLE IF NOT EXISTS subscriptions (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(session_id),
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    resource_url TEXT,
    created_at TEXT NOT NULL,
    deleted_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_subscriptions_resource ON subscriptions(resource_type, resource_id, deleted_at);

CREATE TABLE IF NOT EXISTS watcher_status (
    name TEXT PRIMARY KEY,
    last_success TEXT,
    last_error TEXT,
    last_error_message TEXT
);

CREATE TABLE IF NOT EXISTS resource_relationships (
    id TEXT PRIMARY KEY,
    child_type TEXT NOT NULL,
    child_id TEXT NOT NULL,
    child_url TEXT,
    parent_type TEXT NOT NULL,
    parent_id TEXT NOT NULL,
    parent_url TEXT,
    relationship TEXT NOT NULL,
    source TEXT NOT NULL,
    created_at TEXT NOT NULL
);
