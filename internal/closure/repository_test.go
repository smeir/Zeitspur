package closure

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/smeir/zeitspur/internal/booking"
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

func insertWorkBlock(t *testing.T, db *sql.DB, workDate, startedAt, endedAt string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO work_blocks (work_date, started_at, ended_at, source, status, created_at, updated_at)
		VALUES (?, ?, ?, 'test', 'active', ?, ?)
	`, workDate, startedAt, endedAt, startedAt, endedAt)
	if err != nil {
		t.Fatalf("insert work block: %v", err)
	}
}

func TestRepository_CreateAndSnapshot(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	br := booking.NewRepository(db)
	_ = br.SetBooked(context.Background(), "2026-06-10", true)
	_ = br.SetBooked(context.Background(), "2026-06-11", false)

	days := []DaySummary{
		{Date: "2026-06-10", Booked: true, TrackedMinutes: 480},
		{Date: "2026-06-11", Booked: false, TrackedMinutes: 240},
	}

	cr := NewRepository(db)
	closure, err := cr.Create(context.Background(), "2026-06-11", days)
	if err != nil {
		t.Fatalf("create closure: %v", err)
	}

	if closure.TrackedWorkdayCount != 2 {
		t.Fatalf("expected 2 workdays, got %d", closure.TrackedWorkdayCount)
	}
	if closure.BookedWorkdayCount != 1 {
		t.Fatalf("expected 1 booked, got %d", closure.BookedWorkdayCount)
	}
	if len(closure.Days) != 2 {
		t.Fatalf("expected 2 snapshot days, got %d", len(closure.Days))
	}
}

func TestRepository_DifferenceDetection(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	br := booking.NewRepository(db)
	_ = br.SetBooked(context.Background(), "2026-06-10", true)

	days := []DaySummary{
		{Date: "2026-06-10", Booked: true, TrackedMinutes: 480},
	}

	cr := NewRepository(db)
	closure, err := cr.Create(context.Background(), "2026-06-10", days)
	if err != nil {
		t.Fatalf("create closure: %v", err)
	}

	_ = br.SetBooked(context.Background(), "2026-06-10", false)
	statuses, _ := br.ListDaysInRange(context.Background(), "2026-06-10", "2026-06-10")

	differs, err := cr.HasDifferenceSinceClosure(context.Background(), closure.ID, statuses)
	if err != nil {
		t.Fatalf("difference check: %v", err)
	}
	if !differs {
		t.Fatal("expected difference")
	}
}

func TestRepository_ConcurrentLock(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	cr := NewRepository(db)
	locked, err := cr.AcquireLock(context.Background())
	if err != nil || !locked {
		t.Fatalf("first lock failed: %v", err)
	}

	locked2, err := cr.AcquireLock(context.Background())
	if err != nil {
		t.Fatalf("second lock error: %v", err)
	}
	if locked2 {
		t.Fatal("expected second lock to fail")
	}

	_ = cr.ReleaseLock(context.Background())
}

func TestRepository_PeriodStart_AfterLastClosure(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	cr := NewRepository(db)
	_, err := cr.Create(context.Background(), "2026-06-10", []DaySummary{
		{Date: "2026-06-01", Booked: true, TrackedMinutes: 480},
		{Date: "2026-06-10", Booked: true, TrackedMinutes: 480},
	})
	if err != nil {
		t.Fatalf("create closure: %v", err)
	}

	bookingDay := time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC)
	got, err := cr.PeriodStart(context.Background(), bookingDay)
	if err != nil {
		t.Fatalf("period start: %v", err)
	}
	if got.Format("2006-01-02") != "2026-06-11" {
		t.Fatalf("expected period start 2026-06-11, got %s", got.Format("2006-01-02"))
	}
}

func TestRepository_PeriodStart_FromEarliestWorkBlock(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	insertWorkBlock(t, db, "2026-05-12", "2026-05-12T08:00:00Z", "2026-05-12T16:00:00Z")
	insertWorkBlock(t, db, "2026-05-15", "2026-05-15T08:00:00Z", "2026-05-15T16:00:00Z")

	cr := NewRepository(db)
	bookingDay := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)
	got, err := cr.PeriodStart(context.Background(), bookingDay)
	if err != nil {
		t.Fatalf("period start: %v", err)
	}
	if got.Format("2006-01-02") != "2026-05-12" {
		t.Fatalf("expected period start 2026-05-12, got %s", got.Format("2006-01-02"))
	}
}

func TestRepository_PeriodStart_FallbackToBookingDay(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	cr := NewRepository(db)
	bookingDay := time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC)
	got, err := cr.PeriodStart(context.Background(), bookingDay)
	if err != nil {
		t.Fatalf("period start: %v", err)
	}
	if got.Format("2006-01-02") != "2026-04-30" {
		t.Fatalf("expected period start 2026-04-30, got %s", got.Format("2006-01-02"))
	}
}
