// Package booking manages day booking state and the manual Booking Day.
package booking

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// DayStatus represents the booking state of a single calendar day.
type DayStatus struct {
	WorkDate        string
	Booked          bool
	BookedAt        *time.Time
	BookingRevision int
	CurrentRevision int
	UpdatedAt       time.Time
}

// Repository provides persistence for booking state.
type Repository struct {
	db *sql.DB
}

// NewRepository creates a booking repository.
func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// GetDay returns the booking status for a day, creating a default row if missing.
func (r *Repository) GetDay(ctx context.Context, date string) (*DayStatus, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT work_date, booked, booked_at, booking_revision, current_revision, updated_at
		FROM day_status WHERE work_date = ?
	`, date)
	var ds DayStatus
	var bookedAt sql.NullString
	var updatedAt string
	err := row.Scan(&ds.WorkDate, &ds.Booked, &bookedAt, &ds.BookingRevision, &ds.CurrentRevision, &updatedAt)
	if err == sql.ErrNoRows {
		return &DayStatus{WorkDate: date, Booked: false, CurrentRevision: 0}, nil
	}
	if err != nil {
		return nil, err
	}
	if bookedAt.Valid {
		t, _ := time.Parse(time.RFC3339Nano, bookedAt.String)
		ds.BookedAt = &t
	}
	ds.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return &ds, nil
}

// SetBooked sets the booked flag for a day and bumps the current revision.
func (r *Repository) SetBooked(ctx context.Context, date string, booked bool) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO day_status (work_date, booked, booked_at, booking_revision, current_revision, updated_at)
		VALUES (?, ?, ?, 0, 1, ?)
		ON CONFLICT(work_date) DO UPDATE SET
			booked = excluded.booked,
			booked_at = CASE WHEN excluded.booked = 1 THEN COALESCE(day_status.booked_at, excluded.updated_at) ELSE NULL END,
			booking_revision = CASE WHEN excluded.booked = 1 THEN day_status.current_revision + 1 ELSE day_status.booking_revision END,
			current_revision = day_status.current_revision + 1,
			updated_at = excluded.updated_at
	`, date, booked, now, now)
	return err
}

// BumpRevision increments the current revision for a day.
func (r *Repository) BumpRevision(ctx context.Context, date string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO day_status (work_date, booked, booking_revision, current_revision, updated_at)
		VALUES (?, 0, 0, 1, ?)
		ON CONFLICT(work_date) DO UPDATE SET
			current_revision = day_status.current_revision + 1,
			updated_at = excluded.updated_at
	`, date, now)
	return err
}

// GetBookingDay returns the manually configured Booking Day, if any.
func (r *Repository) GetBookingDay(ctx context.Context) (*time.Time, error) {
	row := r.db.QueryRowContext(ctx, `SELECT current_booking_day FROM booking_settings WHERE id = 1`)
	var s sql.NullString
	if err := row.Scan(&s); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if !s.Valid {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", s.String)
	if err != nil {
		return nil, fmt.Errorf("parse booking day: %w", err)
	}
	return &t, nil
}

// SetBookingDay sets the Booking Day.
func (r *Repository) SetBookingDay(ctx context.Context, day time.Time) error {
	dateStr := day.Format("2006-01-02")
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO booking_settings (id, current_booking_day, updated_at)
		VALUES (1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			current_booking_day = excluded.current_booking_day,
			updated_at = excluded.updated_at
	`, dateStr, now)
	return err
}

// ClearBookingDay removes the Booking Day.
func (r *Repository) ClearBookingDay(ctx context.Context) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO booking_settings (id, current_booking_day, updated_at)
		VALUES (1, NULL, ?)
		ON CONFLICT(id) DO UPDATE SET
			current_booking_day = NULL,
			updated_at = excluded.updated_at
	`, now)
	return err
}

// ListDaysInRange returns all day_status rows for a date range.
func (r *Repository) ListDaysInRange(ctx context.Context, start, end string) ([]*DayStatus, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT work_date, booked, booked_at, booking_revision, current_revision, updated_at
		FROM day_status
		WHERE work_date >= ? AND work_date <= ?
		ORDER BY work_date ASC
	`, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*DayStatus
	for rows.Next() {
		var ds DayStatus
		var bookedAt sql.NullString
		var updatedAt string
		if err := rows.Scan(&ds.WorkDate, &ds.Booked, &bookedAt, &ds.BookingRevision, &ds.CurrentRevision, &updatedAt); err != nil {
			return nil, err
		}
		if bookedAt.Valid {
			t, _ := time.Parse(time.RFC3339Nano, bookedAt.String)
			ds.BookedAt = &t
		}
		ds.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
		result = append(result, &ds)
	}
	return result, rows.Err()
}
