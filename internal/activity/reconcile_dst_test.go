package activity

import (
	"context"
	"testing"
	"time"

	"github.com/smeir/zeitspur/internal/clock"
)

func TestReconciler_DaylightSavingTransition(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	// Europe/Berlin spring forward: 2026-03-29 02:00 -> 03:00.
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}

	day := time.Date(2026, 3, 29, 0, 0, 0, 0, loc)
	start := day.Add(30 * time.Minute) // 00:30 local
	end := day.Add(5 * time.Hour)      // 05:30 local (crosses DST)

	insertEvent(t, db, start, EventActive)
	insertEvent(t, db, end, EventIdle)

	rec := NewReconciler(db, clock.System{}, 30*time.Second)
	if err := rec.RebuildDay(context.Background(), loc, day); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	var count int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM work_blocks WHERE work_date = ?`, day.Format("2006-01-02")).Scan(&count); err != nil {
		t.Fatalf("count blocks: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 block, got %d", count)
	}

	var startedAt, endedAt string
	if err := db.QueryRowContext(context.Background(), `SELECT started_at, ended_at FROM work_blocks`).Scan(&startedAt, &endedAt); err != nil {
		t.Fatalf("select block: %v", err)
	}
	s0, _ := time.Parse(time.RFC3339Nano, startedAt)
	e0, _ := time.Parse(time.RFC3339Nano, endedAt)
	expectedEnd := end.Add(30 * time.Second) // tail credit
	if !s0.Equal(start) || !e0.Equal(expectedEnd) {
		t.Fatalf("block times do not match: %v - %v (expected %v)", s0, e0, expectedEnd)
	}
}
