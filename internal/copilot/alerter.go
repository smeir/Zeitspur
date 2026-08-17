package copilot

import (
	"context"
	"log/slog"
	"time"

	"github.com/smeir/zeitspur/internal/clock"
)

// MessageBuilder produces the localized title and body for a threshold
// notification. It is supplied from the cmd layer so the capture package does
// not depend on i18n (forbidden by the boundary rules).
type MessageBuilder func(consumed float64, limit int) (title, body string)

// Alerter checks whether the current day's Copilot credit consumption has
// crossed the configured threshold and, if so, fires a desktop notification
// once per day. It is invoked by the Fetcher after each successful snapshot.
type Alerter struct {
	repo       *Repository
	state      *StateStore
	notifier   Notifier
	clock      clock.Clock
	loc        *time.Location
	dailyLimit int
	build      MessageBuilder
}

// NewAlerter creates an Alerter. A nil notifier or a dailyLimit of 0 disables
// notifications (Check becomes a no-op).
func NewAlerter(repo *Repository, state *StateStore, notifier Notifier, clk clock.Clock, loc *time.Location, dailyLimit int, build MessageBuilder) *Alerter {
	return &Alerter{
		repo:       repo,
		state:      state,
		notifier:   notifier,
		clock:      clk,
		loc:        loc,
		dailyLimit: dailyLimit,
		build:      build,
	}
}

// Check evaluates the current day's consumption and notifies if the threshold
// was reached and no notification has been sent for today yet. It is safe to
// call after every successful fetch; the per-day debounce prevents spam.
func (a *Alerter) Check(ctx context.Context, fetchedAt time.Time) {
	if a == nil || a.dailyLimit <= 0 || a.notifier == nil || a.build == nil {
		return
	}
	loc := a.loc
	if loc == nil {
		loc = time.UTC
	}

	midnight := dayStart(fetchedAt, loc)

	entries, err := a.repo.Consumption(ctx, midnight, fetchedAt, loc)
	if err != nil {
		slog.Warn("copilot alerter consumption query failed", "error", err)
		return
	}
	consumed := TotalConsumed(entries)
	if consumed < float64(a.dailyLimit) {
		return
	}

	today := midnight.Format("2006-01-02")
	last, err := a.state.LastNotifyDate(ctx)
	if err != nil {
		slog.Warn("copilot alerter state read failed", "error", err)
		return
	}
	if last == today {
		return
	}

	title, body := a.build(consumed, a.dailyLimit)
	if err := a.notifier.Notify(ctx, title, body); err != nil {
		slog.Warn("copilot notify failed", "error", err)
		return
	}
	if err := a.state.SetNotifyDate(ctx, today, a.clock.Now()); err != nil {
		slog.Warn("copilot alerter state write failed", "error", err)
	}
}

// Close releases the notifier's resources.
func (a *Alerter) Close() error {
	if a == nil || a.notifier == nil {
		return nil
	}
	return a.notifier.Close()
}
