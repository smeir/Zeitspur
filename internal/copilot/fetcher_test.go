package copilot

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/smeir/zeitspur/internal/clock"
)

// TestFetcherRunStoresSnapshots drives the full fetch→store path with a mock
// provider and a fast interval, then asserts the snapshot landed in the DB.
func TestFetcherRunStoresSnapshots(t *testing.T) {
	db := newTestDB(t)
	clk := clock.NewFixed(time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC))
	provider := NewMockProvider(&Snapshot{
		OK:                 true,
		Plan:               "business",
		EntitlementCredits: 3000,
		RemainingCredits:   2900,
		UsedCredits:        100,
		WarningLevel:       WarningNone,
	})

	repo := NewRepository(db)
	fetcher := NewFetcher(repo, provider, clk, 5*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := fetcher.Run(ctx); err != nil {
		t.Fatalf("run: %v", err)
	}

	got, err := repo.Latest(context.Background())
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if !got.OK {
		t.Errorf("latest not OK")
	}
	if got.UsedCredits != 100 {
		t.Errorf("used = %v, want 100", got.UsedCredits)
	}
	if got.FetchedAt.IsZero() {
		t.Error("fetched_at is zero")
	}
}

// TestFetcherStoresFailure verifies that a failing provider still records an
// OK=false row so the UI can surface the error.
func TestFetcherStoresFailure(t *testing.T) {
	db := newTestDB(t)
	clk := clock.NewFixed(time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC))
	provider := &MockProvider{Err: errors.New("boom")}
	repo := NewRepository(db)
	fetcher := NewFetcher(repo, provider, clk, 5*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_ = fetcher.Run(ctx)

	got, err := repo.Latest(context.Background())
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if got.OK {
		t.Error("expected OK=false on failure")
	}
	if got.ErrorMessage == "" {
		t.Error("expected error message")
	}
	if got.ErrorKind != ErrorKindUnknown {
		t.Errorf("ErrorKind = %q, want %q for an unclassified error", got.ErrorKind, ErrorKindUnknown)
	}
}

// TestFetcherStoresErrorKind verifies that a classified provider failure is
// persisted with its kind, so the UI can tell a GitHub outage apart from a
// local authentication problem.
func TestFetcherStoresErrorKind(t *testing.T) {
	db := newTestDB(t)
	clk := clock.NewFixed(time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC))
	provider := &MockProvider{Err: &FetchError{Kind: ErrorKindUnavailable, Status: 503, Detail: "No server is currently available"}}
	repo := NewRepository(db)
	fetcher := NewFetcher(repo, provider, clk, 5*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_ = fetcher.Run(ctx)

	got, err := repo.Latest(context.Background())
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if got.ErrorKind != ErrorKindUnavailable {
		t.Errorf("ErrorKind = %q, want %q", got.ErrorKind, ErrorKindUnavailable)
	}
	if !got.ErrorKind.Transient() {
		t.Error("a 503 should be classified as transient")
	}
	if !strings.Contains(got.ErrorMessage, "HTTP 503") {
		t.Errorf("ErrorMessage = %q, want it to mention the status", got.ErrorMessage)
	}
}
