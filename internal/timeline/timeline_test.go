package timeline

import (
	"context"
	"database/sql"
	"testing"
	"time"

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

func insertBlock(t *testing.T, db *sql.DB, date string, start, end time.Time, source, status string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO work_blocks (work_date, started_at, ended_at, source, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, date, start.Format(time.RFC3339Nano), end.Format(time.RFC3339Nano), source, status, now, now)
	if err != nil {
		t.Fatalf("insert block: %v", err)
	}
}

func TestService_Day(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	day := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
	insertBlock(t, db, day.Format("2006-01-02"), day.Add(8*time.Hour), day.Add(10*time.Hour), "detected", "active")
	insertBlock(t, db, day.Format("2006-01-02"), day.Add(11*time.Hour), day.Add(12*time.Hour), "manual", "active")

	svc := NewService(db)
	sum, err := svc.Day(context.Background(), day.Format("2006-01-02"))
	if err != nil {
		t.Fatalf("day: %v", err)
	}
	if sum.WorkedMinutes != 180 {
		t.Fatalf("expected 180 worked minutes, got %d", sum.WorkedMinutes)
	}
	if sum.BlockCount != 2 {
		t.Fatalf("expected 2 blocks, got %d", sum.BlockCount)
	}
}

func TestService_IgnoredBlockNotCounted(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	day := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
	insertBlock(t, db, day.Format("2006-01-02"), day.Add(8*time.Hour), day.Add(10*time.Hour), "detected", "ignored")

	svc := NewService(db)
	sum, err := svc.Day(context.Background(), day.Format("2006-01-02"))
	if err != nil {
		t.Fatalf("day: %v", err)
	}
	if sum.WorkedMinutes != 0 {
		t.Fatalf("expected 0 worked minutes for ignored block, got %d", sum.WorkedMinutes)
	}
}

func TestService_PauseOnlyBetweenBlocks(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	day := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
	// First block starts at 08:00, last block ends at 14:00.
	// Time before the first block and after the last block must not count as pause.
	insertBlock(t, db, day.Format("2006-01-02"), day.Add(8*time.Hour), day.Add(10*time.Hour), "detected", "active")
	insertBlock(t, db, day.Format("2006-01-02"), day.Add(12*time.Hour), day.Add(14*time.Hour), "detected", "active")

	svc := NewService(db)
	sum, err := svc.Day(context.Background(), day.Format("2006-01-02"))
	if err != nil {
		t.Fatalf("day: %v", err)
	}
	if sum.WorkedMinutes != 240 {
		t.Fatalf("expected 240 worked minutes, got %d", sum.WorkedMinutes)
	}
	if sum.PauseMinutes != 120 {
		t.Fatalf("expected 120 pause minutes between blocks, got %d", sum.PauseMinutes)
	}
	if sum.TotalMinutes != 360 {
		t.Fatalf("expected 360 total minutes, got %d", sum.TotalMinutes)
	}
}

func TestService_OverlappingBlocksCountOnce(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	day := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
	// A manual block 09:00-17:00 fully covers a detected block 10:00-12:00.
	insertBlock(t, db, day.Format("2006-01-02"), day.Add(9*time.Hour), day.Add(17*time.Hour), "manual", "active")
	insertBlock(t, db, day.Format("2006-01-02"), day.Add(10*time.Hour), day.Add(12*time.Hour), "detected", "active")

	svc := NewService(db)
	sum, err := svc.Day(context.Background(), day.Format("2006-01-02"))
	if err != nil {
		t.Fatalf("day: %v", err)
	}
	if sum.WorkedMinutes != 480 {
		t.Fatalf("expected 480 worked minutes for overlapping blocks, got %d", sum.WorkedMinutes)
	}
	if sum.PauseMinutes != 0 {
		t.Fatalf("expected 0 pause minutes for overlapping blocks, got %d", sum.PauseMinutes)
	}
}

func TestService_SubMinuteBlocksDoNotLoseTime(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	day := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
	// Four blocks of 5m30s each: 22 minutes total. Truncating each block
	// individually would yield only 20.
	for i := 0; i < 4; i++ {
		start := day.Add(time.Duration(8+i) * time.Hour)
		insertBlock(t, db, day.Format("2006-01-02"), start, start.Add(5*time.Minute+30*time.Second), "detected", "active")
	}

	svc := NewService(db)
	sum, err := svc.Day(context.Background(), day.Format("2006-01-02"))
	if err != nil {
		t.Fatalf("day: %v", err)
	}
	if sum.WorkedMinutes != 22 {
		t.Fatalf("expected 22 worked minutes, got %d", sum.WorkedMinutes)
	}
}

func TestService_RangeSortedByDate(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	base := time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC)
	// Insert days out of order.
	for _, offset := range []int{4, 0, 2, 1, 3} {
		day := base.AddDate(0, 0, offset)
		insertBlock(t, db, day.Format("2006-01-02"), day.Add(9*time.Hour), day.Add(10*time.Hour), "detected", "active")
	}

	svc := NewService(db)
	days, err := svc.Range(context.Background(), base.Format("2006-01-02"), base.AddDate(0, 0, 6).Format("2006-01-02"))
	if err != nil {
		t.Fatalf("range: %v", err)
	}
	if len(days) != 5 {
		t.Fatalf("expected 5 days, got %d", len(days))
	}
	for i := 1; i < len(days); i++ {
		if days[i-1].Date >= days[i].Date {
			t.Fatalf("days not sorted: %s before %s", days[i-1].Date, days[i].Date)
		}
	}
}

func TestService_SingleBlockHasZeroPause(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	day := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
	insertBlock(t, db, day.Format("2006-01-02"), day.Add(9*time.Hour), day.Add(17*time.Hour), "detected", "active")

	svc := NewService(db)
	sum, err := svc.Day(context.Background(), day.Format("2006-01-02"))
	if err != nil {
		t.Fatalf("day: %v", err)
	}
	if sum.PauseMinutes != 0 {
		t.Fatalf("expected 0 pause minutes for single block, got %d", sum.PauseMinutes)
	}
}
