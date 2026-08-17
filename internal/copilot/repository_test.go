package copilot

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/smeir/zeitspur/internal/database"
)

// newTestDB returns an in-memory SQLite database with migrations applied.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func mustStore(t *testing.T, repo *Repository, snap *Snapshot) {
	t.Helper()
	if err := repo.Store(context.Background(), snap); err != nil {
		t.Fatalf("store: %v", err)
	}
}

func TestStoreAndLatest(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db)
	at := time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC)
	mustStore(t, repo, &Snapshot{
		FetchedAt:          at,
		OK:                 true,
		Plan:               "business",
		Organizations:      []string{"bosch-copilot"},
		EntitlementCredits: 3000,
		RemainingCredits:   1197.7,
		UsedCredits:        1802.3,
		PercentRemaining:   39.9,
		ResetAt:            time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		TokenBasedBilling:  true,
		WarningLevel:       WarningNone,
	})

	got, err := repo.Latest(context.Background())
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if got.Plan != "business" {
		t.Errorf("plan = %q", got.Plan)
	}
	if len(got.Organizations) != 1 || got.Organizations[0] != "bosch-copilot" {
		t.Errorf("organizations = %v", got.Organizations)
	}
	if got.UsedCredits != 1802.3 {
		t.Errorf("used = %v", got.UsedCredits)
	}
	if !got.TokenBasedBilling {
		t.Errorf("token_based_billing = false")
	}
}

func TestLatestEmpty(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db)
	if _, err := repo.Latest(context.Background()); err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestRangeOrdersAscending(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db)
	t1 := time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 6, 8, 11, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	// Store out of order to verify ordering.
	mustStore(t, repo, &Snapshot{FetchedAt: t3, OK: true, UsedCredits: 3})
	mustStore(t, repo, okSnap(t1, 1))
	mustStore(t, repo, okSnap(t2, 2))

	got, err := repo.Range(context.Background(), t1, t3)
	if err != nil {
		t.Fatalf("range: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].UsedCredits != 1 || got[2].UsedCredits != 3 {
		t.Errorf("order = %v, %v, %v", got[0].UsedCredits, got[1].UsedCredits, got[2].UsedCredits)
	}
}

func TestConsumptionSumsPositiveDeltas(t *testing.T) {
	loc := time.UTC
	db := newTestDB(t)
	repo := NewRepository(db)
	// Same billing period (same reset_at); used climbs 0 → 10 → 25 → 30.
	mustStore(t, repo, okSnap(time.Date(2026, 6, 8, 9, 0, 0, 0, loc), 0))
	mustStore(t, repo, okSnap(time.Date(2026, 6, 8, 10, 0, 0, 0, loc), 10))
	mustStore(t, repo, okSnap(time.Date(2026, 6, 8, 11, 0, 0, 0, loc), 25))
	mustStore(t, repo, okSnap(time.Date(2026, 6, 8, 12, 0, 0, 0, loc), 30))

	start := time.Date(2026, 6, 8, 0, 0, 0, 0, loc)
	end := time.Date(2026, 6, 8, 23, 59, 59, 0, loc)
	entries, err := repo.Consumption(context.Background(), start, end, loc)
	if err != nil {
		t.Fatalf("consumption: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].Consumed != 30 {
		t.Errorf("consumed = %v, want 30", entries[0].Consumed)
	}
	if entries[0].Samples != 3 {
		t.Errorf("samples = %d, want 3", entries[0].Samples)
	}
}

func TestConsumptionResetsNegativeDelta(t *testing.T) {
	loc := time.UTC
	db := newTestDB(t)
	repo := NewRepository(db)
	// Day 1: used climbs to 20. Day 2: quota resets so used drops to 5.
	mustStore(t, repo, okSnap(time.Date(2026, 6, 8, 9, 0, 0, 0, loc), 0))
	mustStore(t, repo, okSnap(time.Date(2026, 6, 8, 10, 0, 0, 0, loc), 10))
	mustStore(t, repo, okSnap(time.Date(2026, 6, 8, 11, 0, 0, 0, loc), 20))
	mustStore(t, repo, okSnap(time.Date(2026, 6, 9, 9, 0, 0, 0, loc), 0))
	mustStore(t, repo, okSnap(time.Date(2026, 6, 9, 10, 0, 0, 0, loc), 5))

	start := time.Date(2026, 6, 8, 0, 0, 0, 0, loc)
	end := time.Date(2026, 6, 9, 23, 59, 59, 0, loc)
	entries, err := repo.Consumption(context.Background(), start, end, loc)
	if err != nil {
		t.Fatalf("consumption: %v", err)
	}
	byDay := map[string]float64{}
	for _, e := range entries {
		byDay[e.Date.Format("2006-01-02")] = e.Consumed
	}
	if byDay["2026-06-08"] != 20 {
		t.Errorf("day1 = %v, want 20", byDay["2026-06-08"])
	}
	// After reset (used 20 → 0), the next delta is 5-0=5; total for day2 = 5.
	if byDay["2026-06-09"] != 5 {
		t.Errorf("day2 = %v, want 5", byDay["2026-06-09"])
	}
}

func TestConsumptionSkipsFailedSnapshots(t *testing.T) {
	loc := time.UTC
	db := newTestDB(t)
	repo := NewRepository(db)
	mustStore(t, repo, okSnap(time.Date(2026, 6, 8, 9, 0, 0, 0, loc), 0))
	mustStore(t, repo, okSnap(time.Date(2026, 6, 8, 10, 0, 0, 0, loc), 10))
	// A failed snapshot must not anchor a delta; the next OK snapshot diffs
	// against the last OK one (10 → 25 = 15, not 0 → 25 = 25).
	mustStore(t, repo, &Snapshot{FetchedAt: time.Date(2026, 6, 8, 11, 0, 0, 0, loc), OK: false, ErrorMessage: "boom"})
	mustStore(t, repo, okSnap(time.Date(2026, 6, 8, 12, 0, 0, 0, loc), 25))

	start := time.Date(2026, 6, 8, 0, 0, 0, 0, loc)
	end := time.Date(2026, 6, 8, 23, 59, 59, 0, loc)
	entries, err := repo.Consumption(context.Background(), start, end, loc)
	if err != nil {
		t.Fatalf("consumption: %v", err)
	}
	if TotalConsumed(entries) != 25 {
		t.Errorf("total = %v, want 25", TotalConsumed(entries))
	}
}

func TestConsumptionUsesPredecessor(t *testing.T) {
	loc := time.UTC
	db := newTestDB(t)
	repo := NewRepository(db)
	// Snapshot just before the range start provides the baseline so the first
	// in-range delta is counted (0 → 10 = 10).
	mustStore(t, repo, okSnap(time.Date(2026, 6, 7, 23, 0, 0, 0, loc), 0))
	mustStore(t, repo, okSnap(time.Date(2026, 6, 8, 1, 0, 0, 0, loc), 10))

	start := time.Date(2026, 6, 8, 0, 0, 0, 0, loc)
	end := time.Date(2026, 6, 8, 23, 59, 59, 0, loc)
	entries, err := repo.Consumption(context.Background(), start, end, loc)
	if err != nil {
		t.Fatalf("consumption: %v", err)
	}
	if TotalConsumed(entries) != 10 {
		t.Errorf("total = %v, want 10 (predecessor used)", TotalConsumed(entries))
	}
}

// okSnap is a successful snapshot with the given used-credits value and a
// fixed entitlement, to keep consumption tests readable.
func okSnap(at time.Time, used float64) *Snapshot {
	return &Snapshot{
		FetchedAt:          at,
		OK:                 true,
		EntitlementCredits: 3000,
		RemainingCredits:   3000 - used,
		UsedCredits:        used,
		WarningLevel:       WarningNone,
	}
}
