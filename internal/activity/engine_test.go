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

	eng := NewEngine(db, mock, clk, 3*time.Minute, 5*time.Second)
	ctx := context.Background()

	eng.Process(ctx)

	// Transition to idle at exactly the threshold.
	clk.Set(start.Add(3 * time.Minute))
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

	eng := NewEngine(db, mock, clk, 3*time.Minute, 5*time.Second)
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

	eng := NewEngine(db, mock, clk, 3*time.Minute, 5*time.Second)
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

	eng := NewEngine(db, mock, clk, 3*time.Minute, 5*time.Second)
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

func TestEngine_RestoreStateAvoidsDuplicateActiveEvent(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	start := time.Date(2026, 6, 13, 8, 0, 0, 0, time.UTC)
	// Pre-seed an active event as if the service was restarted while active.
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO activity_events (occurred_at, event_type, provider, created_at)
		VALUES (?, ?, 'mock', ?)
	`, start.Format(time.RFC3339Nano), string(EventActive), start.Format(time.RFC3339Nano))
	if err != nil {
		t.Fatalf("insert event: %v", err)
	}

	mock := NewMockProvider(ActivityActive)
	clk := clock.NewFixed(start.Add(2 * time.Minute))
	eng := NewEngine(db, mock, clk, 3*time.Minute, 5*time.Second)

	ctx := context.Background()
	eng.Process(ctx)

	types := eventTypes(t, db)
	if len(types) != 1 || types[0] != string(EventActive) {
		t.Fatalf("expected no duplicate active event, got %v", types)
	}
}

func TestEngine_GapDetection(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	start := time.Date(2026, 6, 13, 8, 0, 0, 0, time.UTC)
	mock := NewMockProvider(ActivityActive)
	clk := clock.NewFixed(start)

	idleThreshold := 3 * time.Minute
	pollInterval := 5 * time.Second
	eng := NewEngine(db, mock, clk, idleThreshold, pollInterval)
	ctx := context.Background()

	// Initial active event
	eng.Process(ctx)

	// Simulate a long gap (e.g. system sleep) while the next tick happens.
	// Gap must be > idleThreshold + 2*pollInterval.
	gapTime := start.Add(10 * time.Minute)
	clk.Set(gapTime)
	// Still reported as Active by the provider upon wake
	mock.SetState(ActivityActive)
	eng.Process(ctx)

	types := eventTypes(t, db)
	// Expected: Active (8:00), Suspend (retroactive to 8:00), Resume (at 8:10)
	if len(types) != 3 {
		t.Fatalf("expected 3 events, got %d: %v", len(types), types)
	}
	if types[1] != string(EventSuspend) || types[2] != string(EventResume) {
		t.Fatalf("expected suspend then resume after gap, got %v", types)
	}

	// Verify the Suspend event happened at the previous lastActiveAt (8:00)
	var suspendTs string
	err := db.QueryRow("SELECT occurred_at FROM activity_events WHERE event_type = 'suspend'").Scan(&suspendTs)
	if err != nil {
		t.Fatalf("query suspend ts: %v", err)
	}
	if suspendTs != start.Format(time.RFC3339Nano) {
		t.Errorf("expected suspend at %v, got %v", start.Format(time.RFC3339Nano), suspendTs)
	}
}

func TestEngine_GracefulShutdown(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	start := time.Date(2026, 6, 13, 8, 0, 0, 0, time.UTC)
	mock := NewMockProvider(ActivityActive)
	clk := clock.NewFixed(start)

	eng := NewEngine(db, mock, clk, 3*time.Minute, 10*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())

	// Start engine in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- eng.Run(ctx)
	}()

	// Wait for first tick/poll
	time.Sleep(50 * time.Millisecond)

	// Cancel context to trigger shutdown
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Run() returned error: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Run() did not stop in time")
	}

	types := eventTypes(t, db)
	// Expected: Active (from tick), Suspend (from shutdown)
	foundSuspend := false
	for _, typ := range types {
		if typ == string(EventSuspend) {
			foundSuspend = true
			break
		}
	}
	if !foundSuspend {
		t.Fatalf("expected suspend event after shutdown, got %v", types)
	}
}
