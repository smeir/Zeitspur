package booking

import (
	"context"
	"testing"
)

func TestRepository_ChangedAfterBooking(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	r := NewRepository(db)
	if err := r.SetBooked(context.Background(), "2026-06-13", true); err != nil {
		t.Fatalf("set booked: %v", err)
	}

	// Simulate a later edit bumping current_revision.
	if err := r.BumpRevision(context.Background(), "2026-06-13"); err != nil {
		t.Fatalf("bump revision: %v", err)
	}

	ds, err := r.GetDay(context.Background(), "2026-06-13")
	if err != nil {
		t.Fatalf("get day: %v", err)
	}
	if ds.CurrentRevision <= ds.BookingRevision {
		t.Fatalf("expected current revision > booking revision, got booking=%d current=%d", ds.BookingRevision, ds.CurrentRevision)
	}
}
