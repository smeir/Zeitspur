PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS activity_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    occurred_at TEXT NOT NULL,
    event_type TEXT NOT NULL,
    provider TEXT NOT NULL,
    metadata_json TEXT,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_activity_events_occurred_at ON activity_events(occurred_at);
CREATE INDEX IF NOT EXISTS idx_activity_events_event_type ON activity_events(event_type);

CREATE TABLE IF NOT EXISTS work_blocks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    work_date TEXT NOT NULL,
    started_at TEXT NOT NULL,
    ended_at TEXT NOT NULL,
    source TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    note TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_work_blocks_work_date ON work_blocks(work_date);
CREATE INDEX IF NOT EXISTS idx_work_blocks_started_at ON work_blocks(started_at);

CREATE TABLE IF NOT EXISTS day_status (
    work_date TEXT PRIMARY KEY,
    booked INTEGER NOT NULL DEFAULT 0,
    booked_at TEXT,
    booking_revision INTEGER NOT NULL DEFAULT 0,
    current_revision INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS booking_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    current_booking_day TEXT,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS booking_closures (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    period_start TEXT NOT NULL,
    period_end TEXT NOT NULL,
    booking_day TEXT NOT NULL,
    closed_at TEXT NOT NULL,
    tracked_workday_count INTEGER NOT NULL,
    booked_workday_count INTEGER NOT NULL,
    unbooked_workday_count INTEGER NOT NULL,
    tracked_minutes_snapshot INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS booking_closure_days (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    closure_id INTEGER NOT NULL,
    work_date TEXT NOT NULL,
    booked_snapshot INTEGER NOT NULL,
    tracked_minutes_snapshot INTEGER NOT NULL,
    day_revision_snapshot INTEGER NOT NULL,
    FOREIGN KEY (closure_id) REFERENCES booking_closures(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_booking_closure_days_closure_id ON booking_closure_days(closure_id);

CREATE TABLE IF NOT EXISTS audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    action TEXT NOT NULL,
    previous_value_json TEXT,
    new_value_json TEXT,
    reason TEXT,
    occurred_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_audit_log_entity ON audit_log(entity_type, entity_id);
