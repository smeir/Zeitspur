package copilot

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// StateStore persists the Copilot alerter's debounce state in the single-row
// copilot_state table. It records the last calendar day a threshold
// notification was fired so the daemon does not re-notify on every hourly
// fetch or after a restart.
type StateStore struct {
	db *sql.DB
}

// NewStateStore returns a StateStore backed by the given database.
func NewStateStore(db *sql.DB) *StateStore { return &StateStore{db: db} }

// LastNotifyDate returns the stored last-notify date (YYYY-MM-DD), or an empty
// string when no notification has been recorded yet.
func (s *StateStore) LastNotifyDate(ctx context.Context) (string, error) {
	var date sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT last_notify_date FROM copilot_state WHERE id = 1`).Scan(&date)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read copilot_state: %w", err)
	}
	return date.String, nil
}

// SetNotifyDate records that a notification was fired for the given date.
// The row is upserted so the table stays a single row.
func (s *StateStore) SetNotifyDate(ctx context.Context, date string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO copilot_state (id, last_notify_date, updated_at)
		VALUES (1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET last_notify_date = excluded.last_notify_date, updated_at = excluded.updated_at
	`, date, at.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("write copilot_state: %w", err)
	}
	return nil
}
