package booking

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

func date(s string) time.Time {
	d, _ := time.Parse("2006-01-02", s)
	return d
}

func TestRepository_SetBooked(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	r := NewRepository(db)
	if err := r.SetBooked(context.Background(), "2026-06-13", true); err != nil {
		t.Fatalf("set booked: %v", err)
	}

	ds, err := r.GetDay(context.Background(), "2026-06-13")
	if err != nil {
		t.Fatalf("get day: %v", err)
	}
	if !ds.Booked {
		t.Fatal("expected booked")
	}
	if ds.BookedAt == nil {
		t.Fatal("expected booked_at")
	}
}

func TestRepository_BookingDay(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	r := NewRepository(db)
	if err := r.SetBookingDay(context.Background(), date("2026-06-19")); err != nil {
		t.Fatalf("set booking day: %v", err)
	}

	day, err := r.GetBookingDay(context.Background())
	if err != nil {
		t.Fatalf("get booking day: %v", err)
	}
	if day == nil || day.Format("2006-01-02") != "2026-06-19" {
		t.Fatalf("unexpected booking day: %v", day)
	}

	if err := r.ClearBookingDay(context.Background()); err != nil {
		t.Fatalf("clear booking day: %v", err)
	}
	day, err = r.GetBookingDay(context.Background())
	if err != nil {
		t.Fatalf("get booking day: %v", err)
	}
	if day != nil {
		t.Fatalf("expected nil booking day, got %v", day)
	}
}
