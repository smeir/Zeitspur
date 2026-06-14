package activity

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/smeir/zeitspur/internal/clock"
)

func insertEvent(t *testing.T, db *sql.DB, occurred time.Time, eventType EventType) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO activity_events (occurred_at, event_type, provider, created_at)
		VALUES (?, ?, 'mock', ?)
	`, occurred.Format(time.RFC3339Nano), string(eventType), occurred.Format(time.RFC3339Nano))
	if err != nil {
		t.Fatalf("insert event: %v", err)
	}
}

func TestReconciler_BuildsBlocks(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	loc := time.UTC
	day := time.Date(2026, 6, 13, 0, 0, 0, 0, loc)
	insertEvent(t, db, day.Add(8*time.Hour), EventActive)
	insertEvent(t, db, day.Add(10*time.Hour), EventIdle)

	rec := NewReconciler(db, clock.System{})
	if err := rec.RebuildDay(context.Background(), loc, day); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	var count int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM work_blocks`).Scan(&count); err != nil {
		t.Fatalf("count blocks: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 block, got %d", count)
	}
}

func TestReconciler_SplitsAtMidnight(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	loc := time.UTC
	start := time.Date(2026, 6, 13, 22, 0, 0, 0, loc)
	insertEvent(t, db, start, EventActive)
	insertEvent(t, db, start.Add(5*time.Hour), EventIdle)

	rec := NewReconciler(db, clock.System{})
	if err := rec.RebuildDay(context.Background(), loc, start); err != nil {
		t.Fatalf("rebuild first day: %v", err)
	}
	if err := rec.RebuildDay(context.Background(), loc, start.AddDate(0, 0, 1)); err != nil {
		t.Fatalf("rebuild second day: %v", err)
	}

	var count int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM work_blocks`).Scan(&count); err != nil {
		t.Fatalf("count blocks: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 blocks after midnight split, got %d", count)
	}
}

func TestReconciler_OpenBlockEndsAtNow(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	loc := time.UTC
	day := time.Date(2026, 6, 13, 0, 0, 0, 0, loc)
	insertEvent(t, db, day.Add(8*time.Hour), EventActive)

	now := day.Add(10*time.Hour + 15*time.Minute)
	rec := NewReconciler(db, clock.NewFixed(now))
	if err := rec.RebuildDay(context.Background(), loc, day); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	var endStr string
	if err := db.QueryRowContext(context.Background(), `SELECT ended_at FROM work_blocks`).Scan(&endStr); err != nil {
		t.Fatalf("select end: %v", err)
	}
	end, _ := time.Parse(time.RFC3339Nano, endStr)
	if !end.Equal(now) {
		t.Fatalf("expected open block to end at %v, got %v", now, end)
	}
}

func TestReconciler_IgnoredBlockNotRecreated(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	loc := time.UTC
	day := time.Date(2026, 6, 13, 0, 0, 0, 0, loc)
	start := day.Add(8 * time.Hour)
	end := day.Add(10 * time.Hour)
	insertEvent(t, db, start, EventActive)
	insertEvent(t, db, end, EventIdle)

	// First reconcile creates the detected block.
	rec := NewReconciler(db, clock.System{})
	if err := rec.RebuildDay(context.Background(), loc, day); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	// Mark the block as ignored.
	if _, err := db.ExecContext(context.Background(), `
		UPDATE work_blocks SET status = 'ignored', updated_at = ?
		WHERE work_date = ? AND source = 'detected'
	`, time.Now().UTC().Format(time.RFC3339Nano), day.Format("2006-01-02")); err != nil {
		t.Fatalf("ignore block: %v", err)
	}

	// Reconcile again: the ignored block must not come back as active.
	if err := rec.RebuildDay(context.Background(), loc, day); err != nil {
		t.Fatalf("rebuild after ignore: %v", err)
	}

	var activeCount int
	if err := db.QueryRowContext(context.Background(), `
		SELECT COUNT(*) FROM work_blocks WHERE work_date = ? AND source = 'detected' AND status = 'active'
	`, day.Format("2006-01-02")).Scan(&activeCount); err != nil {
		t.Fatalf("count active blocks: %v", err)
	}
	if activeCount != 0 {
		t.Fatalf("expected ignored block to stay ignored, got %d active detected blocks", activeCount)
	}
}

func TestReconciler_IgnoredOpenBlockExtendsWithActiveTick(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	loc := time.UTC
	day := time.Date(2026, 6, 13, 0, 0, 0, 0, loc)
	start := day.Add(8 * time.Hour)
	ignoredUntil := day.Add(10 * time.Hour)
	nextTick := ignoredUntil.Add(15 * time.Minute)
	insertEvent(t, db, start, EventActive)

	rec := NewReconciler(db, clock.NewFixed(ignoredUntil))
	if err := rec.RebuildDay(context.Background(), loc, day); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	if _, err := db.ExecContext(context.Background(), `
		UPDATE work_blocks SET status = 'ignored', updated_at = ?
		WHERE work_date = ? AND source = 'detected'
	`, ignoredUntil.Format(time.RFC3339Nano), day.Format("2006-01-02")); err != nil {
		t.Fatalf("ignore open block: %v", err)
	}

	rec = NewReconciler(db, clock.NewFixed(nextTick))
	if err := rec.RebuildDay(context.Background(), loc, day); err != nil {
		t.Fatalf("rebuild after active tick: %v", err)
	}

	var activeCount int
	if err := db.QueryRowContext(context.Background(), `
		SELECT COUNT(*) FROM work_blocks WHERE work_date = ? AND source = 'detected' AND status = 'active'
	`, day.Format("2006-01-02")).Scan(&activeCount); err != nil {
		t.Fatalf("count active blocks: %v", err)
	}
	if activeCount != 0 {
		t.Fatalf("expected ignored open block to stay ignored, got %d active detected blocks", activeCount)
	}

	var ignoredEndStr string
	if err := db.QueryRowContext(context.Background(), `
		SELECT ended_at FROM work_blocks WHERE work_date = ? AND source = 'detected' AND status = 'ignored'
	`, day.Format("2006-01-02")).Scan(&ignoredEndStr); err != nil {
		t.Fatalf("select ignored end: %v", err)
	}
	ignoredEnd, err := time.Parse(time.RFC3339Nano, ignoredEndStr)
	if err != nil {
		t.Fatalf("parse ignored end: %v", err)
	}
	if !ignoredEnd.Equal(nextTick) {
		t.Fatalf("expected ignored block to extend to %v, got %v", nextTick, ignoredEnd)
	}
}
