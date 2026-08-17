package copilot

import (
	"context"
	"log/slog"
	"time"

	"github.com/smeir/zeitspur/internal/clock"
)

// Fetcher periodically calls a CreditProvider and persists each snapshot. It
// mirrors the activity engine's lifecycle: a Run loop driven by a ticker,
// graceful cancellation via context, and error logging that avoids spamming
// on repeated identical failures.
type Fetcher struct {
	repo     *Repository
	provider CreditProvider
	clock    clock.Clock
	interval time.Duration
	alerter  *Alerter
	lastErr  error
}

// WithAlerter attaches an Alerter to the fetcher so it is checked after each
// successful snapshot. Returns the fetcher for chaining.
func (f *Fetcher) WithAlerter(a *Alerter) *Fetcher {
	f.alerter = a
	return f
}

// NewFetcher creates a fetcher that polls every interval and stores results
// via repo.
func NewFetcher(repo *Repository, provider CreditProvider, clk clock.Clock, interval time.Duration) *Fetcher {
	return &Fetcher{
		repo:     repo,
		provider: provider,
		clock:    clk,
		interval: interval,
	}
}

// Run polls the provider until ctx is cancelled. It fetches once immediately
// on start so a freshly started daemon shows data without waiting an interval,
// then continues on the ticker.
func (f *Fetcher) Run(ctx context.Context) error {
	f.poll(ctx)

	ticker := time.NewTicker(f.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			f.poll(ctx)
		}
	}
}

// poll executes a single fetch+store cycle. Repeated identical failures are
// logged once on transition (nil→err and err→nil) instead of every tick.
func (f *Fetcher) poll(ctx context.Context) {
	snap, err := f.provider.Fetch(ctx)
	now := f.clock.Now().UTC()
	if err != nil {
		kind := KindOf(err)
		if err := f.repo.Store(ctx, &Snapshot{
			FetchedAt:    now,
			OK:           false,
			ErrorMessage: err.Error(),
			ErrorKind:    kind,
		}); err != nil {
			slog.Error("copilot store failed-status failed", "error", err)
		}
		if f.lastErr == nil {
			slog.Warn("copilot fetch failed", "kind", string(kind), "error", err)
		}
		f.lastErr = err
		return
	}
	snap.FetchedAt = now
	if err := f.repo.Store(ctx, snap); err != nil {
		slog.Error("copilot store failed", "error", err)
	}
	if f.alerter != nil {
		f.alerter.Check(ctx, now)
	}
	if f.lastErr != nil {
		slog.Info("copilot fetch recovered")
	}
	f.lastErr = nil
}
