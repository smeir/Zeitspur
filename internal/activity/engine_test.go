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

func TestEngine_IdleEventTimestamp(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	start := time.Date(2026, 6, 13, 8, 0, 0, 0, time.UTC)
	mock := NewMockProvider(ActivityActive)
	clk := clock.NewFixed(start)

	eng := NewEngine(db, mock, clk, 3*time.Minute, 5*time.Second)
	ctx := context.Background()
	eng.Process(ctx)

	clk.Set(start.Add(2 * time.Minute))
	mock.SetState(ActivityIdle)
	eng.Process(ctx)

	var ts string
	err := db.QueryRow("SELECT occurred_at FROM activity_events WHERE event_type = 'idle'").Scan(&ts)
	if err != nil {
		t.Fatalf("expected idle event, got %v", err)
	}

	expected := start.Add(-1 * time.Minute).Format(time.RFC3339Nano)
	if ts != expected {
		t.Errorf("expected idle at %v, got %v", expected, ts)
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

func TestEngine_HandleSuspendSignal(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	// Mirrors the real incident: the screen was unlocked at 23:12:26, stayed
	// active for a while, and the laptop lid was closed at 23:46:26.
	unlockedAt := time.Date(2026, 7, 2, 23, 12, 26, 0, time.UTC)
	suspendAt := unlockedAt.Add(34 * time.Minute)
	mock := NewMockProvider(ActivityActive)
	clk := clock.NewFixed(unlockedAt)

	eng := NewEngine(db, mock, clk, 10*time.Minute, time.Minute)
	ctx := context.Background()
	eng.Process(ctx) // records "active" at unlockedAt, lastActiveAt = unlockedAt

	// PrepareForSleep(true) arrives immediately when the lid is closed, long
	// before the polling gap heuristic (idleThreshold + 2*pollInterval =
	// 12min) would ever notice anything.
	clk.Set(suspendAt)
	eng.handleSuspendSignal(ctx)

	types := eventTypes(t, db)
	if len(types) != 2 || types[0] != string(EventActive) || types[1] != string(EventSuspend) {
		t.Fatalf("expected active, suspend; got %v", types)
	}
	if eng.lastState != ActivitySuspended {
		t.Fatalf("expected engine state Suspended, got %v", eng.lastState)
	}

	var suspendTs string
	if err := db.QueryRow("SELECT occurred_at FROM activity_events WHERE event_type = 'suspend'").Scan(&suspendTs); err != nil {
		t.Fatalf("query suspend ts: %v", err)
	}
	if want := suspendAt.Format(time.RFC3339Nano); suspendTs != want {
		t.Errorf("expected suspend recorded at signal time %v, got %v", want, suspendTs)
	}

	// The work block for the day must already be closed at the signal time,
	// not left open across the (simulated) rest of the night.
	var endedAt string
	if err := db.QueryRow(`
		SELECT ended_at FROM work_blocks
		WHERE work_date = ? ORDER BY started_at DESC LIMIT 1
	`, suspendAt.Format("2006-01-02")).Scan(&endedAt); err != nil {
		t.Fatalf("query work block: %v", err)
	}
	if want := suspendAt.Format(time.RFC3339Nano); endedAt != want {
		t.Errorf("expected work block to end at %v, got %v", want, endedAt)
	}

	// A duplicate PrepareForSleep(true) signal must be a no-op.
	clk.Advance(time.Minute)
	eng.handleSuspendSignal(ctx)
	if got := eventTypes(t, db); len(got) != 2 {
		t.Fatalf("expected duplicate suspend signal to be a no-op, got %v", got)
	}
}

func TestEngine_ResumeSignalTriggersImmediatePoll(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	start := time.Date(2026, 7, 2, 23, 46, 0, 0, time.UTC)
	mock := NewMockProvider(ActivityActive)
	clk := clock.NewFixed(start)

	// A poll interval long enough that this test would time out waiting for
	// the regular ticker if the resume signal did not trigger an immediate
	// poll.
	eng := NewEngine(db, mock, clk, 3*time.Minute, time.Hour)
	ctx := context.Background()

	// Seed the initial "active" event synchronously so the test does not
	// depend on the (deliberately slow) ticker firing.
	if err := eng.Process(ctx); err != nil {
		t.Fatalf("initial process: %v", err)
	}

	sleepEvents := make(chan bool, 1)
	eng.sleepEvents = sleepEvents

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- eng.Run(runCtx) }()

	sleepEvents <- true // about to suspend
	waitForEventCount(t, db, 2)

	// The machine sleeps for hours; wall clock jumps far past anything the
	// hour-long poll interval would catch in time for this test. Both
	// mutations below happen-before the receive completes in Run's select,
	// because they are ordered before the (synchronizing) channel send.
	resumeAt := start.Add(9 * time.Hour)
	clk.Set(resumeAt)
	mock.SetState(ActivityLocked) // woke up already locked (lock-on-suspend)
	sleepEvents <- false          // resumed

	waitForEventCount(t, db, 3)

	types := eventTypes(t, db)
	if types[1] != string(EventSuspend) || types[2] != string(EventLocked) {
		t.Fatalf("expected active, suspend, locked; got %v", types)
	}

	var lockedTs string
	if err := db.QueryRow("SELECT occurred_at FROM activity_events WHERE event_type = 'locked'").Scan(&lockedTs); err != nil {
		t.Fatalf("query locked ts: %v", err)
	}
	if want := resumeAt.Format(time.RFC3339Nano); lockedTs != want {
		t.Errorf("expected locked recorded at resume time %v, got %v", want, lockedTs)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Run() returned error: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Run() did not stop in time")
	}
}

// waitForEventCount polls the activity_events table until it holds at least
// n rows or the deadline expires, avoiding a fixed sleep for timing-sensitive
// assertions.
func waitForEventCount(t *testing.T, db *sql.DB, n int) {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		if len(eventTypes(t, db)) >= n {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %d events, got %v", n, eventTypes(t, db))
		case <-time.After(5 * time.Millisecond):
		}
	}
}
