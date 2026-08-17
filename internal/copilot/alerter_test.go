package copilot

import (
	"context"
	"testing"
	"time"

	"github.com/smeir/zeitspur/internal/clock"
)

// fakeNotifier records the last notification for assertions.
type fakeNotifier struct {
	title string
	body  string
	calls int
}

func (f *fakeNotifier) Notify(_ context.Context, title, body string) error {
	f.title = title
	f.body = body
	f.calls++
	return nil
}
func (f *fakeNotifier) Close() error { return nil }

func newAlerterFixture(t *testing.T, limit int) (*Alerter, *Repository, *StateStore, *fakeNotifier, *clock.Fixed) {
	t.Helper()
	db := newTestDB(t)
	repo := NewRepository(db)
	state := NewStateStore(db)
	notif := &fakeNotifier{}
	clk := clock.NewFixed(time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC))
	loc := time.UTC
	build := func(consumed float64, lim int) (string, string) {
		return "ALERT", "body"
	}
	a := NewAlerter(repo, state, notif, clk, loc, limit, build)
	return a, repo, state, notif, clk
}

func TestAlerterNoopBelowLimit(t *testing.T) {
	a, repo, _, notif, _ := newAlerterFixture(t, 2500)
	// Two snapshots: used goes 0 → 100 (consumed 100, below 2500).
	mustStore(t, repo, okSnap(time.Date(2026, 6, 8, 9, 0, 0, 0, time.UTC), 0))
	mustStore(t, repo, okSnap(time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC), 100))

	a.Check(context.Background(), time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC))
	if notif.calls != 0 {
		t.Errorf("expected no notification, got %d", notif.calls)
	}
}

func TestAlerterNotifiesAtThreshold(t *testing.T) {
	a, repo, _, notif, _ := newAlerterFixture(t, 2500)
	// used 0 → 2500 → crosses the limit.
	mustStore(t, repo, okSnap(time.Date(2026, 6, 8, 9, 0, 0, 0, time.UTC), 0))
	mustStore(t, repo, okSnap(time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC), 2500))

	a.Check(context.Background(), time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC))
	if notif.calls != 1 {
		t.Fatalf("expected 1 notification, got %d", notif.calls)
	}
	if notif.title != "ALERT" {
		t.Errorf("title = %q", notif.title)
	}
}

func TestAlerterDebouncesPerDay(t *testing.T) {
	a, repo, _, notif, _ := newAlerterFixture(t, 2500)
	mustStore(t, repo, okSnap(time.Date(2026, 6, 8, 9, 0, 0, 0, time.UTC), 0))
	mustStore(t, repo, okSnap(time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC), 3000))

	// First check fires.
	a.Check(context.Background(), time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC))
	// A later snapshot the same day must not re-fire.
	mustStore(t, repo, okSnap(time.Date(2026, 6, 8, 11, 0, 0, 0, time.UTC), 3100))
	a.Check(context.Background(), time.Date(2026, 6, 8, 11, 0, 0, 0, time.UTC))

	if notif.calls != 1 {
		t.Errorf("expected 1 notification (debounced), got %d", notif.calls)
	}
}

func TestAlerterNotifiesAgainNextDay(t *testing.T) {
	a, repo, state, notif, _ := newAlerterFixture(t, 2500)
	mustStore(t, repo, okSnap(time.Date(2026, 6, 8, 9, 0, 0, 0, time.UTC), 0))
	mustStore(t, repo, okSnap(time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC), 3000))
	// Fire on day 1.
	a.Check(context.Background(), time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC))

	// Day 2: fresh consumption crossing the limit again.
	mustStore(t, repo, okSnap(time.Date(2026, 6, 9, 9, 0, 0, 0, time.UTC), 0))
	mustStore(t, repo, okSnap(time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC), 2600))
	a.Check(context.Background(), time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC))

	if notif.calls != 2 {
		t.Errorf("expected 2 notifications (one per day), got %d", notif.calls)
	}
	// The state should record day 2.
	last, _ := state.LastNotifyDate(context.Background())
	if last != "2026-06-09" {
		t.Errorf("last_notify_date = %q, want 2026-06-09", last)
	}
}

func TestAlerterDisabledByZeroLimit(t *testing.T) {
	a, repo, _, notif, _ := newAlerterFixture(t, 0)
	mustStore(t, repo, okSnap(time.Date(2026, 6, 8, 9, 0, 0, 0, time.UTC), 0))
	mustStore(t, repo, okSnap(time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC), 5000))
	a.Check(context.Background(), time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC))
	if notif.calls != 0 {
		t.Errorf("expected no notification when limit=0, got %d", notif.calls)
	}
}

func TestAlerterIgnoresFailedSnapshotsInSum(t *testing.T) {
	a, repo, _, notif, _ := newAlerterFixture(t, 2500)
	mustStore(t, repo, okSnap(time.Date(2026, 6, 8, 9, 0, 0, 0, time.UTC), 0))
	mustStore(t, repo, okSnap(time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC), 3000))
	// A failed snapshot does not contribute; consumption stays 3000 (≥2500) so
	// the first check still fires, but a second check the same day is debounced.
	a.Check(context.Background(), time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC))
	if notif.calls != 1 {
		t.Errorf("expected 1 notification, got %d", notif.calls)
	}
}

func TestStateStoreRoundTrip(t *testing.T) {
	db := newTestDB(t)
	state := NewStateStore(db)

	got, err := state.LastNotifyDate(context.Background())
	if err != nil {
		t.Fatalf("initial read: %v", err)
	}
	if got != "" {
		t.Errorf("initial date = %q, want empty", got)
	}

	if err := state.SetNotifyDate(context.Background(), "2026-06-08", time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, _ = state.LastNotifyDate(context.Background())
	if got != "2026-06-08" {
		t.Errorf("after set = %q, want 2026-06-08", got)
	}

	// Upsert to a new date.
	if err := state.SetNotifyDate(context.Background(), "2026-06-09", time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, _ = state.LastNotifyDate(context.Background())
	if got != "2026-06-09" {
		t.Errorf("after upsert = %q, want 2026-06-09", got)
	}
}
