// Package closure implements manual booking-period closure with immutable snapshots.
package closure

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/smeir/zeitspur/internal/booking"
)

// Closure represents an immutable booking-period closure record.
type Closure struct {
	ID                     int
	PeriodStart            string
	PeriodEnd              string
	BookingDay             string
	ClosedAt               time.Time
	TrackedWorkdayCount    int
	BookedWorkdayCount     int
	UnbookedWorkdayCount   int
	TrackedMinutesSnapshot int
	Days                   []ClosureDay
}

// ClosureDay is a snapshot of a single day at closure time.
type ClosureDay struct {
	ID                  int
	ClosureID           int
	WorkDate            string
	BookedSnapshot      bool
	TrackedMinutes      int
	DayRevisionSnapshot int
}

// DaySummary is used to compute a closure.
type DaySummary struct {
	Date            string
	Booked          bool
	BookingRevision int
	CurrentRevision int
	TrackedMinutes  int
}

// Repository provides persistence for closures.
type Repository struct {
	db *sql.DB
}

// NewRepository creates a closure repository.
func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// LastClosure returns the most recent closure, if any.
func (r *Repository) LastClosure(ctx context.Context) (*Closure, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, period_start, period_end, booking_day, closed_at,
			tracked_workday_count, booked_workday_count, unbooked_workday_count, tracked_minutes_snapshot
		FROM booking_closures
		ORDER BY closed_at DESC
		LIMIT 1
	`)
	return r.scanClosure(row)
}

// List returns all closures ordered by closed_at descending.
func (r *Repository) List(ctx context.Context) ([]Closure, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, period_start, period_end, booking_day, closed_at,
			tracked_workday_count, booked_workday_count, unbooked_workday_count, tracked_minutes_snapshot
		FROM booking_closures
		ORDER BY closed_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var closures []Closure
	for rows.Next() {
		c, err := r.scanClosure(rows)
		if err != nil {
			return nil, err
		}
		closures = append(closures, *c)
	}
	return closures, rows.Err()
}

// Get returns a closure by ID including its day snapshot.
func (r *Repository) Get(ctx context.Context, id int) (*Closure, error) {
	c, err := r.scanClosure(r.db.QueryRowContext(ctx, `
		SELECT id, period_start, period_end, booking_day, closed_at,
			tracked_workday_count, booked_workday_count, unbooked_workday_count, tracked_minutes_snapshot
		FROM booking_closures WHERE id = ?
	`, id))
	if err != nil {
		return nil, err
	}
	days, err := r.listDays(ctx, id)
	if err != nil {
		return nil, err
	}
	c.Days = days
	return c, nil
}

// Create stores a new closure and its day snapshot within a transaction.
func (r *Repository) Create(ctx context.Context, bookingDay string, days []DaySummary) (*Closure, error) {
	if len(days) == 0 {
		return nil, fmt.Errorf("no days to close")
	}

	periodStart := days[0].Date
	periodEnd := days[len(days)-1].Date

	var tracked, booked, unbooked, totalMinutes int
	for _, d := range days {
		tracked++
		totalMinutes += d.TrackedMinutes
		if d.Booked {
			booked++
		} else {
			unbooked++
		}
	}

	closedAt := time.Now().UTC()

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `
		INSERT INTO booking_closures
			(period_start, period_end, booking_day, closed_at,
			 tracked_workday_count, booked_workday_count, unbooked_workday_count, tracked_minutes_snapshot)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, periodStart, periodEnd, bookingDay, closedAt.Format(time.RFC3339Nano),
		tracked, booked, unbooked, totalMinutes)
	if err != nil {
		return nil, fmt.Errorf("insert closure: %w", err)
	}

	closureID64, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("last insert id: %w", err)
	}
	closureID := int(closureID64)

	for _, d := range days {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO booking_closure_days
				(closure_id, work_date, booked_snapshot, tracked_minutes_snapshot, day_revision_snapshot)
			VALUES (?, ?, ?, ?, ?)
		`, closureID, d.Date, d.Booked, d.TrackedMinutes, d.CurrentRevision)
		if err != nil {
			return nil, fmt.Errorf("insert closure day: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return r.Get(ctx, closureID)
}

// IsLocked tries to acquire a closure lock to prevent concurrent closures.
func (r *Repository) IsLocked(ctx context.Context) (bool, error) {
	// Simple advisory lock via a dedicated table row.
	var locked int
	err := r.db.QueryRowContext(ctx, `SELECT COALESCE((SELECT 1 FROM closure_lock WHERE id = 1), 0)`).Scan(&locked)
	return locked == 1, err
}

// AcquireLock attempts to acquire the closure lock.
func (r *Repository) AcquireLock(ctx context.Context) (bool, error) {
	_, err := r.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS closure_lock (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			locked_at TEXT NOT NULL
		)
	`)
	if err != nil {
		return false, err
	}
	res, err := r.db.ExecContext(ctx, `
		INSERT INTO closure_lock (id, locked_at) VALUES (1, ?)
		ON CONFLICT(id) DO NOTHING
	`, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// ReleaseLock releases the closure lock.
func (r *Repository) ReleaseLock(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM closure_lock WHERE id = 1`)
	return err
}

// PeriodStart returns the first day of the next open booking period.
// If a closure exists, it is the day after the last closure's period end.
// Otherwise it falls back to the earliest work block date, or the booking day itself.
func (r *Repository) PeriodStart(ctx context.Context, bookingDay time.Time) (time.Time, error) {
	last, err := r.LastClosure(ctx)
	if err != nil {
		return time.Time{}, fmt.Errorf("last closure: %w", err)
	}
	if last != nil {
		end, err := time.Parse("2006-01-02", last.PeriodEnd)
		if err != nil {
			return time.Time{}, fmt.Errorf("parse period end: %w", err)
		}
		return end.AddDate(0, 0, 1), nil
	}

	row := r.db.QueryRowContext(ctx, `SELECT MIN(work_date) FROM work_blocks`)
	var minDate sql.NullString
	if err := row.Scan(&minDate); err != nil {
		return time.Time{}, fmt.Errorf("min work date: %w", err)
	}
	if minDate.Valid {
		d, err := time.Parse("2006-01-02", minDate.String)
		if err != nil {
			return time.Time{}, fmt.Errorf("parse min work date: %w", err)
		}
		return d, nil
	}
	return bookingDay, nil
}

// HasDifferenceSinceClosure reports whether current data differs from the snapshot.
func (r *Repository) HasDifferenceSinceClosure(ctx context.Context, closureID int, statuses []*booking.DayStatus) (bool, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT work_date, booked_snapshot, day_revision_snapshot
		FROM booking_closure_days
		WHERE closure_id = ?
	`, closureID)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	snapshot := make(map[string]struct {
		booked   bool
		revision int
	})
	for rows.Next() {
		var date string
		var booked bool
		var rev int
		if err := rows.Scan(&date, &booked, &rev); err != nil {
			return false, err
		}
		snapshot[date] = struct {
			booked   bool
			revision int
		}{booked: booked, revision: rev}
	}

	for _, ds := range statuses {
		s, ok := snapshot[ds.WorkDate]
		if !ok {
			return true, nil
		}
		if s.booked != ds.Booked || s.revision != ds.CurrentRevision {
			return true, nil
		}
	}
	return false, nil
}

func (r *Repository) scanClosure(row interface{ Scan(...any) error }) (*Closure, error) {
	var c Closure
	var closedAt string
	err := row.Scan(&c.ID, &c.PeriodStart, &c.PeriodEnd, &c.BookingDay, &closedAt,
		&c.TrackedWorkdayCount, &c.BookedWorkdayCount, &c.UnbookedWorkdayCount, &c.TrackedMinutesSnapshot)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.ClosedAt, _ = time.Parse(time.RFC3339Nano, closedAt)
	return &c, nil
}

func (r *Repository) listDays(ctx context.Context, closureID int) ([]ClosureDay, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, closure_id, work_date, booked_snapshot, tracked_minutes_snapshot, day_revision_snapshot
		FROM booking_closure_days
		WHERE closure_id = ?
		ORDER BY work_date ASC
	`, closureID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var days []ClosureDay
	for rows.Next() {
		var d ClosureDay
		if err := rows.Scan(&d.ID, &d.ClosureID, &d.WorkDate, &d.BookedSnapshot, &d.TrackedMinutes, &d.DayRevisionSnapshot); err != nil {
			return nil, err
		}
		days = append(days, d)
	}
	return days, rows.Err()
}
