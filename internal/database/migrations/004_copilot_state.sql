-- Single-row state table for the Copilot alerter, tracking whether a
-- threshold notification has already been fired for the current day so the
-- daemon does not re-notify on every hourly fetch (or after a restart).
CREATE TABLE IF NOT EXISTS copilot_state (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    last_notify_date TEXT,
    updated_at TEXT NOT NULL
);
