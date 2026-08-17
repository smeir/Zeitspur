package copilot

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
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

// Range returns snapshots with fetched_at in [start, end], ascending.
func (r *Repository) Range(ctx context.Context, start, end time.Time) ([]*Snapshot, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT fetched_at, ok, plan, organizations,
		       entitlement_credits, remaining_credits, used_credits,
		       percent_remaining, reset_at, token_based_billing,
		       warning_level, error_message, error_kind
		FROM copilot_snapshots
		WHERE fetched_at >= ? AND fetched_at <= ?
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

// FirstBefore returns the most recent snapshot strictly before t, or nil.
// It anchors the consumption delta at the start of a range so the first
// snapshot inside the range has a predecessor to diff against.
func (r *Repository) FirstBefore(ctx context.Context, t time.Time) (*Snapshot, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT fetched_at, ok, plan, organizations,
		       entitlement_credits, remaining_credits, used_credits,
		       percent_remaining, reset_at, token_based_billing,
		       warning_level, error_message, error_kind
		FROM copilot_snapshots
		WHERE fetched_at < ?
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

// DailyConsumption aggregates credit consumption per calendar day (in loc).
// Consumption between two consecutive successful snapshots is the positive
// delta of used credits; a negative delta (quota reset) means the new period's
// used total counts instead. Each delta is attributed to the day of its later
// snapshot's fetched_at, in the given location.
type DailyConsumption struct {
	Date     time.Time
	Consumed float64
	Samples  int
}

// Consumption returns the per-day consumption for snapshots in [start, end],
// using loc to split days. It pulls one predecessor before start so the first
// in-range snapshot has a baseline.
func (r *Repository) Consumption(ctx context.Context, start, end time.Time, loc *time.Location) ([]DailyConsumption, error) {
	anchor, err := r.FirstBefore(ctx, start)
	if err != nil {
		return nil, err
	}
	snaps, err := r.Range(ctx, start, end)
	if err != nil {
		return nil, err
	}
	chain := snaps
	if anchor != nil {
		chain = append([]*Snapshot{anchor}, snaps...)
	}

	byDay := make(map[string]*DailyConsumption)
	var order []string
	prev := (*Snapshot)(nil)
	for _, s := range chain {
		if !s.OK {
			// Skip failed snapshots but keep prev so the next OK snapshot
			// still diffs against the last good one.
			continue
		}
		if prev != nil {
			delta := s.UsedCredits - prev.UsedCredits
			if delta < 0 {
				// Quota reset (or entitlement change): the new period's used
				// total is the consumption since the reset.
				delta = s.UsedCredits
			}
			day := s.FetchedAt.In(loc).Format("2006-01-02")
			dc, ok := byDay[day]
			if !ok {
				dc = &DailyConsumption{Date: dayStart(s.FetchedAt, loc)}
				byDay[day] = dc
				order = append(order, day)
			}
			dc.Consumed += delta
			dc.Samples++
		}
		prev = s
	}

	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	out := make([]DailyConsumption, 0, len(order))
	for _, day := range order {
		out = append(out, *byDay[day])
	}
	return out, nil
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
