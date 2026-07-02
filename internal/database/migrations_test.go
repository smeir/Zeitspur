package database

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// TestMigrateUTCTimestamps verifies that migration 002 rewrites legacy
// local-offset timestamps to UTC so lexical SQL comparisons match
// chronological order, keeps sub-second precision, and preserves NULLs.
func TestMigrateUTCTimestamps(t *testing.T) {
	db, err := sql.Open("sqlite", "file:migcheck?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	// Apply only the initial schema, then insert legacy rows with local offsets.
	migs, err := LoadMigrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if len(migs) < 2 {
		t.Fatalf("expected at least 2 migrations, got %d", len(migs))
	}
	if _, err := db.ExecContext(ctx, migs[0].SQL); err != nil {
		t.Fatalf("apply initial schema: %v", err)
	}
	if _, err := db.ExecContext(ctx, schemaMigrationsTable); err != nil {
		t.Fatalf("create schema_migrations: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (1, datetime('now'))`); err != nil {
		t.Fatalf("record migration 1: %v", err)
	}

	// Legacy timestamps incl. the DST fall-back hour where lexical order of
	// local-offset strings contradicts chronological order.
	legacy := []string{
		"2026-10-25T02:45:00.123456+02:00", // 00:45 UTC
		"2026-10-25T02:15:00+01:00",        // 01:15 UTC, lexically before the row above
		"2026-07-02T10:00:00+02:00",
		"2026-07-02T09:30:00Z", // already UTC
	}
	for _, ts := range legacy {
		if _, err := db.ExecContext(ctx, `INSERT INTO activity_events (occurred_at, event_type, provider, created_at) VALUES (?, 'active', 'test', ?)`, ts, ts); err != nil {
			t.Fatalf("insert event: %v", err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO work_blocks (work_date, started_at, ended_at, source, status, created_at, updated_at)
		VALUES ('2026-07-02', '2026-07-02T10:00:00+02:00', '2026-07-02T12:00:00.5+02:00', 'detected', 'active', '2026-07-02T10:00:00+02:00', '2026-07-02T10:00:00+02:00')`); err != nil {
		t.Fatalf("insert block: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO day_status (work_date, booked, booked_at, updated_at) VALUES ('2026-07-02', 0, NULL, '2026-07-02T10:00:00+02:00')`); err != nil {
		t.Fatalf("insert day_status: %v", err)
	}

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	rows, err := db.QueryContext(ctx, `SELECT occurred_at FROM activity_events ORDER BY occurred_at ASC`)
	if err != nil {
		t.Fatalf("query events: %v", err)
	}
	defer rows.Close()
	var prev time.Time
	for rows.Next() {
		var ts string
		if err := rows.Scan(&ts); err != nil {
			t.Fatalf("scan: %v", err)
		}
		tm, err := time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			t.Fatalf("migrated timestamp %q not parseable: %v", ts, err)
		}
		if tm.Before(prev) {
			t.Fatalf("lexical order does not match chronological order at %q", ts)
		}
		prev = tm
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	var startStr, endStr string
	if err := db.QueryRowContext(ctx, `SELECT started_at, ended_at FROM work_blocks`).Scan(&startStr, &endStr); err != nil {
		t.Fatalf("select block: %v", err)
	}
	s0, err := time.Parse(time.RFC3339Nano, startStr)
	if err != nil {
		t.Fatalf("parse started_at %q: %v", startStr, err)
	}
	e0, err := time.Parse(time.RFC3339Nano, endStr)
	if err != nil {
		t.Fatalf("parse ended_at %q: %v", endStr, err)
	}
	if !s0.Equal(time.Date(2026, 7, 2, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("started_at wrong after migration: %q", startStr)
	}
	if got := e0.Sub(s0); got != 2*time.Hour+500*time.Millisecond {
		t.Fatalf("sub-second precision lost: duration %v (ended_at %q)", got, endStr)
	}

	var bookedAt sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT booked_at FROM day_status`).Scan(&bookedAt); err != nil {
		t.Fatalf("select day_status: %v", err)
	}
	if bookedAt.Valid {
		t.Fatalf("NULL booked_at was overwritten: %q", bookedAt.String)
	}
}
