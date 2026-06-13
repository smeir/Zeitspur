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

	rec := NewReconciler(db, clock.System{}, 30*time.Second)
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

	rec := NewReconciler(db, clock.System{}, 30*time.Second)
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

func TestReconciler_TailCredit(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	loc := time.UTC
	day := time.Date(2026, 6, 13, 0, 0, 0, 0, loc)
	insertEvent(t, db, day.Add(8*time.Hour), EventActive)
	insertEvent(t, db, day.Add(8*time.Hour+3*time.Minute), EventIdle)

	rec := NewReconciler(db, clock.System{}, 30*time.Second)
	if err := rec.RebuildDay(context.Background(), loc, day); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	var endStr string
	if err := db.QueryRowContext(context.Background(), `SELECT ended_at FROM work_blocks`).Scan(&endStr); err != nil {
		t.Fatalf("select end: %v", err)
	}
	end, _ := time.Parse(time.RFC3339Nano, endStr)
	expected := day.Add(8*time.Hour + 3*time.Minute + 30*time.Second)
	if !end.Equal(expected) {
		t.Fatalf("expected end %v, got %v", expected, end)
	}
}
