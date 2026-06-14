package activity

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/smeir/zeitspur/internal/clock"
	"github.com/smeir/zeitspur/internal/database"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := database.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func eventTypes(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), `SELECT event_type FROM activity_events ORDER BY id ASC`)
	if err != nil {
		t.Fatalf("query events: %v", err)
	}
	defer rows.Close()
	var types []string
	for rows.Next() {
		var et string
		if err := rows.Scan(&et); err != nil {
			t.Fatalf("scan event: %v", err)
		}
		types = append(types, et)
	}
	return types
}

func TestEngine_ActiveToIdleTransition(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	start := time.Date(2026, 6, 13, 8, 0, 0, 0, time.UTC)
	mock := NewMockProvider(ActivityActive)
	clk := clock.NewFixed(start)

	eng := NewEngine(db, mock, clk, 3*time.Minute, 30*time.Second, 5*time.Second)
	ctx := context.Background()

	eng.Process(ctx)

	// Transition to idle after threshold.
	clk.Set(start.Add(4 * time.Minute))
	mock.SetState(ActivityIdle)
	eng.Process(ctx)

	types := eventTypes(t, db)
	if len(types) < 2 || types[0] != string(EventActive) || types[1] != string(EventIdle) {
		t.Fatalf("expected active, idle; got %v", types)
	}
}

func TestEngine_ScreenLockEndsBlock(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	start := time.Date(2026, 6, 13, 8, 0, 0, 0, time.UTC)
	mock := NewMockProvider(ActivityActive)
	clk := clock.NewFixed(start)

	eng := NewEngine(db, mock, clk, 3*time.Minute, 30*time.Second, 5*time.Second)
	ctx := context.Background()
	eng.Process(ctx)

	mock.SetState(ActivityLocked)
	eng.Process(ctx)

	types := eventTypes(t, db)
	if len(types) < 2 || types[0] != string(EventActive) || types[1] != string(EventLocked) {
		t.Fatalf("expected active then locked, got %v", types)
	}
}

func TestEngine_ShortIdleGapKeepsBlock(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	start := time.Date(2026, 6, 13, 8, 0, 0, 0, time.UTC)
	mock := NewMockProvider(ActivityActive)
	clk := clock.NewFixed(start)

	eng := NewEngine(db, mock, clk, 3*time.Minute, 30*time.Second, 5*time.Second)
	ctx := context.Background()
	eng.Process(ctx)

	clk.Set(start.Add(1 * time.Minute))
	mock.SetState(ActivityIdle)
	eng.Process(ctx)

	// No idle event should be written yet.
	types := eventTypes(t, db)
	if len(types) != 1 {
		t.Fatalf("expected 1 event, got %d: %v", len(types), types)
	}
}

func TestEngine_SuspendResume(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	start := time.Date(2026, 6, 13, 8, 0, 0, 0, time.UTC)
	mock := NewMockProvider(ActivityActive)
	clk := clock.NewFixed(start)

	eng := NewEngine(db, mock, clk, 3*time.Minute, 30*time.Second, 5*time.Second)
	ctx := context.Background()
	eng.Process(ctx)

	mock.SetState(ActivitySuspended)
	eng.Process(ctx)

	mock.SetState(ActivityActive)
	eng.Process(ctx)

	types := eventTypes(t, db)
	if len(types) < 3 || types[1] != string(EventSuspend) || types[2] != string(EventResume) {
		t.Fatalf("expected active, suspend, resume, got %v", types)
	}
}
