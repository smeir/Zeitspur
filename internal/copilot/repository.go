package copilot

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Repository reads and writes Copilot credit snapshots in SQLite.
type Repository struct {
	db *sql.DB
}

// NewRepository returns a Repository backed by the given database.
func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

// Store inserts a snapshot. fetchedAt is recorded as-is; created_at defaults
// to fetchedAt when zero so callers do not need a separate clock for the row.
func (r *Repository) Store(ctx context.Context, snap *Snapshot) error {
	if snap == nil {
		return fmt.Errorf("store: nil snapshot")
	}
	createdAt := snap.FetchedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	orgs := strings.Join(snap.Organizations, ",")
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO copilot_snapshots (
			fetched_at, ok, plan, organizations,
			entitlement_credits, remaining_credits, used_credits,
			percent_remaining, reset_at, token_based_billing,
			warning_level, error_message, error_kind, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		snap.FetchedAt.UTC().Format(time.RFC3339Nano),
		snap.OK,
		snap.Plan,
		orgs,
		snap.EntitlementCredits,
		snap.RemainingCredits,
		snap.UsedCredits,
		snap.PercentRemaining,
		nullTime(snap.ResetAt),
		snap.TokenBasedBilling,
		string(snap.WarningLevel),
		snap.ErrorMessage,
		string(snap.ErrorKind),
		createdAt.UTC().Format(time.RFC3339Nano),
	)
	return err
}

// Latest returns the most recent snapshot, or sql.ErrNoRows if none exists.
func (r *Repository) Latest(ctx context.Context) (*Snapshot, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT fetched_at, ok, plan, organizations,
		       entitlement_credits, remaining_credits, used_credits,
		       percent_remaining, reset_at, token_based_billing,
		       warning_level, error_message, error_kind
		FROM copilot_snapshots
		ORDER BY fetched_at DESC, id DESC
		LIMIT 1
	`)
	return scanSnapshot(row)
}

// Range returns successful snapshots with fetched_at in [start, end],
// ascending.
func (r *Repository) Range(ctx context.Context, start, end time.Time) ([]*Snapshot, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT fetched_at, ok, plan, organizations,
		       entitlement_credits, remaining_credits, used_credits,
		       percent_remaining, reset_at, token_based_billing,
		       warning_level, error_message, error_kind
		FROM copilot_snapshots
		WHERE fetched_at >= ? AND fetched_at <= ? AND ok = 1
		ORDER BY fetched_at ASC, id ASC
	`, start.UTC().Format(time.RFC3339Nano), end.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Snapshot
	for rows.Next() {
		s, err := scanSnapshot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// FirstSuccessful returns the earliest successful snapshot, or nil. Its used
// credits form the one-time opening balance for consumption history.
func (r *Repository) FirstSuccessful(ctx context.Context) (*Snapshot, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT fetched_at, ok, plan, organizations,
		       entitlement_credits, remaining_credits, used_credits,
		       percent_remaining, reset_at, token_based_billing,
		       warning_level, error_message, error_kind
		FROM copilot_snapshots
		WHERE ok = 1
		ORDER BY fetched_at ASC, id ASC
		LIMIT 1
	`)
	s, err := scanSnapshot(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return s, err
}

// FirstBefore returns the most recent successful snapshot strictly before t,
// or nil.
func (r *Repository) FirstBefore(ctx context.Context, t time.Time) (*Snapshot, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT fetched_at, ok, plan, organizations,
		       entitlement_credits, remaining_credits, used_credits,
		       percent_remaining, reset_at, token_based_billing,
		       warning_level, error_message, error_kind
		FROM copilot_snapshots
		WHERE fetched_at < ? AND ok = 1
		ORDER BY fetched_at DESC, id DESC
		LIMIT 1
	`, t.UTC().Format(time.RFC3339Nano))
	s, err := scanSnapshot(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return s, err
}

// scanner abstracts *sql.Row and *sql.Rows for scanSnapshot.
type scanner interface {
	Scan(dest ...any) error
}

func scanSnapshot(s scanner) (*Snapshot, error) {
	var (
		snap              Snapshot
		orgs              sql.NullString
		plan              sql.NullString
		resetAt           sql.NullString
		warningLevel      sql.NullString
		errorMessage      sql.NullString
		errorKind         sql.NullString
		entitlement       sql.NullFloat64
		remaining         sql.NullFloat64
		used              sql.NullFloat64
		percentRemaining  sql.NullFloat64
		ok                bool
		tokenBasedBilling bool
		fetchedAt         string
	)
	if err := s.Scan(
		&fetchedAt, &ok, &plan, &orgs,
		&entitlement, &remaining, &used,
		&percentRemaining, &resetAt, &tokenBasedBilling,
		&warningLevel, &errorMessage, &errorKind,
	); err != nil {
		return nil, err
	}
	t, err := time.Parse(time.RFC3339Nano, fetchedAt)
	if err != nil {
		return nil, fmt.Errorf("parse fetched_at %q: %w", fetchedAt, err)
	}
	snap.FetchedAt = t.UTC()
	snap.OK = ok
	snap.Plan = plan.String
	snap.Organizations = splitCSV(orgs.String)
	snap.EntitlementCredits = entitlement.Float64
	snap.RemainingCredits = remaining.Float64
	snap.UsedCredits = used.Float64
	snap.PercentRemaining = percentRemaining.Float64
	if resetAt.Valid {
		if rt, err := time.Parse(time.RFC3339Nano, resetAt.String); err == nil {
			snap.ResetAt = rt.UTC()
		}
	}
	snap.TokenBasedBilling = tokenBasedBilling
	if warningLevel.Valid {
		snap.WarningLevel = WarningLevel(warningLevel.String)
	}
	snap.ErrorMessage = errorMessage.String
	snap.ErrorKind = ErrorKind(errorKind.String)
	return &snap, nil
}

// baselineBefore returns the last successful snapshot strictly before t that
// is not an implausible reading, or nil. A reading whose used credits drop below
// its predecessor's without a quota reset is skipped, so a placeholder
// response just before a range boundary cannot become the baseline.
func (r *Repository) baselineBefore(ctx context.Context, t time.Time) (*Snapshot, error) {
	candidate, err := r.FirstBefore(ctx, t)
	for candidate != nil && err == nil {
		prev, prevErr := r.FirstBefore(ctx, candidate.FetchedAt)
		if prevErr != nil {
			return nil, prevErr
		}
		if prev == nil || !implausibleDrop(prev, candidate) {
			return candidate, nil
		}
		candidate = prev
	}
	return nil, err
}

// DailyConsumption is the credit consumption of one calendar day.
type DailyConsumption struct {
	Date     time.Time
	Consumed float64
}

// Consumption returns the per-day consumption for snapshots in [start, end],
// in ascending date order, using loc to split days. Consumption between two
// consecutive successful snapshots is the positive delta of used credits;
// after a quota reset the new period's used total counts instead. A drop
// without a reset is an implausible reading and is skipped. Each delta is
// attributed to the day of its later snapshot. A plausible predecessor before
// start gives the first in-range snapshot a baseline. For the first successful
// snapshot ever, its existing usage is attributed to the previous local
// calendar day when that day is inside the requested range.
func (r *Repository) Consumption(ctx context.Context, start, end time.Time, loc *time.Location) ([]DailyConsumption, error) {
	first, err := r.FirstSuccessful(ctx)
	if err != nil {
		return nil, err
	}
	anchor, err := r.baselineBefore(ctx, start)
	if err != nil {
		return nil, err
	}
	snaps, err := r.Range(ctx, start, end)
	if err != nil {
		return nil, err
	}

	// Snapshots arrive in ascending order, so each day is either the last
	// entry or a new one.
	var out []DailyConsumption
	addConsumption := func(day time.Time, delta float64) {
		if n := len(out); n == 0 || !out[n-1].Date.Equal(day) {
			out = append(out, DailyConsumption{Date: day})
		}
		out[len(out)-1].Consumed += delta
	}

	if first != nil && first.UsedCredits > 0 {
		baselineDay := dayStart(first.FetchedAt, loc).AddDate(0, 0, -1)
		if !baselineDay.Before(dayStart(start, loc)) && !baselineDay.After(dayStart(end, loc)) {
			addConsumption(baselineDay, first.UsedCredits)
		}
	}

	prev := anchor
	for _, s := range snaps {
		if prev == nil {
			prev = s
			continue
		}
		if implausibleDrop(prev, s) {
			// Placeholder or partial response: keep prev so the next real
			// reading diffs against the last plausible one.
			continue
		}
		delta := s.UsedCredits - prev.UsedCredits
		if delta < 0 {
			// Quota reset: the new period's used total is the consumption
			// since the reset.
			delta = s.UsedCredits
		}
		addConsumption(dayStart(s.FetchedAt, loc), delta)
		prev = s
	}
	return out, nil
}

// quotaReset reports whether a billing period ended between prev and s:
// either GitHub moved the reset date forward, or s was fetched at or after
// prev's reset date. Snapshots without a reset date never count as a reset.
func quotaReset(prev, s *Snapshot) bool {
	if prev.ResetAt.IsZero() || s.ResetAt.IsZero() {
		return false
	}
	return s.ResetAt.After(prev.ResetAt) || !s.FetchedAt.Before(prev.ResetAt)
}

// implausibleDrop reports whether s shows fewer used credits than prev
// without a quota reset. Used credits only grow within a billing period, so
// such a reading comes from a placeholder response (for example an
// entitlement of 1) and must not be treated as a reset: the next real reading
// would otherwise count the full monthly usage again.
func implausibleDrop(prev, s *Snapshot) bool {
	return s.UsedCredits < prev.UsedCredits && !quotaReset(prev, s)
}

// dayStart returns midnight in loc for the given instant.
func dayStart(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

// TotalConsumed sums the consumption entries.
func TotalConsumed(entries []DailyConsumption) float64 {
	var total float64
	for _, e := range entries {
		total += e.Consumed
	}
	return total
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func splitCSV(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
