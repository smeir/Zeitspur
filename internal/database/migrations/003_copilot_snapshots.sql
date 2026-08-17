-- Copilot credit snapshots: one row per hourly fetch of gh api copilot_internal/user.
-- Only high-level quota numbers are stored (entitlement, remaining, used,
-- percent, reset date). No prompts, models, or content are ever captured,
-- keeping the privacy model intact. See docs/architecture.md.
CREATE TABLE IF NOT EXISTS copilot_snapshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    fetched_at TEXT NOT NULL,
    ok INTEGER NOT NULL,
    plan TEXT,
    organizations TEXT,
    entitlement_credits REAL,
    remaining_credits REAL,
    used_credits REAL,
    percent_remaining REAL,
    reset_at TEXT,
    token_based_billing INTEGER,
    warning_level TEXT,
    error_message TEXT,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_copilot_snapshots_fetched_at ON copilot_snapshots(fetched_at);
