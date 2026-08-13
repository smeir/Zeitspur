package activity

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/smeir/zeitspur/internal/clock"
)

// Engine polls an ActivityProvider and persists state transitions.
type Engine struct {
	db            *sql.DB
	provider      ActivityProvider
	clock         clock.Clock
	reconciler    *Reconciler
	idleThreshold time.Duration
	pollInterval  time.Duration

	lastState    ActivityState
	lastActiveAt time.Time

	// sleepEvents delivers true shortly before the system suspends and false
	// right after it resumes (see logind.go's sleepWatcher). Run sets it up
	// from the real system bus unless a test has already injected a fake
	// channel here.
	sleepEvents <-chan bool

	// lastReconciledDay tracks the local midnight of the most recently
	// reconciled day so the previous day gets a final rebuild (closing its
	// open block at midnight) when work crosses a day boundary.
	lastReconciledDay time.Time
}

// NewEngine creates a new activity engine.
func NewEngine(db *sql.DB, provider ActivityProvider, clk clock.Clock, idleThreshold, pollInterval time.Duration) *Engine {
	e := &Engine{
		db:            db,
		provider:      provider,
		clock:         clk,
		reconciler:    NewReconciler(db, clk),
		idleThreshold: idleThreshold,
		pollInterval:  pollInterval,
		lastState:     ActivityUnknown,
	}
	e.restoreState(context.Background())
	return e
}

// restoreState loads the most recent event so the engine avoids inserting
// redundant active events after a restart.
func (e *Engine) restoreState(ctx context.Context) {
	var typ, ts string
	row := e.db.QueryRowContext(ctx, `
		SELECT event_type, occurred_at FROM activity_events
		ORDER BY occurred_at DESC, id DESC
		LIMIT 1
	`)
	if err := row.Scan(&typ, &ts); err != nil {
		return
	}
	occurredAt, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return
	}
	switch EventType(typ) {
	case EventActive, EventUnlocked, EventResume:
		e.lastState = ActivityActive
		e.lastActiveAt = occurredAt

		// Recover the last known active time from work_blocks.
		// If the engine crashed without writing a suspend event, the reconciler
		// had updated the projected work_blocks ended_at on every tick.
		var lastEnded sql.NullString
		err := e.db.QueryRowContext(ctx, `
			SELECT MAX(ended_at) FROM work_blocks 
			WHERE status = 'active' AND source = 'detected' AND ended_at > ?
		`, ts).Scan(&lastEnded)
		if err == nil && lastEnded.Valid {
			if endedAt, err := time.Parse(time.RFC3339Nano, lastEnded.String); err == nil {
				e.lastActiveAt = endedAt
			}
		}
	case EventIdle:
		e.lastState = ActivityIdle
	case EventLocked:
		e.lastState = ActivityLocked
	case EventSuspend:
		e.lastState = ActivitySuspended
	}
}

// Run polls the provider until the context is cancelled. It also reacts
// immediately to logind's PrepareForSleep signal when available, so suspend
// and resume are recorded precisely instead of only being inferred from a
// gap between poll ticks (see tick's gap detection, which remains active as
// a fallback for missed or undelivered signals).
func (e *Engine) Run(ctx context.Context) error {
	ticker := time.NewTicker(e.pollInterval)
	defer ticker.Stop()

	if e.sleepEvents == nil {
		if watcher, err := newSleepWatcher(); err != nil {
			slog.Warn("suspend/resume signal unavailable, falling back to poll-based gap detection", "error", err)
		} else {
			defer watcher.Close()
			e.sleepEvents = watcher.Events()
		}
	}

	var lastErr error
	poll := func() {
		if err := e.Process(ctx); err != nil {
			if lastErr == nil {
				_ = e.insertEvent(ctx, EventProviderError, map[string]any{"error": err.Error()})
			}
			lastErr = err
		} else {
			lastErr = nil
		}
	}

	for {
		select {
		case <-ctx.Done():
			if e.lastState == ActivityActive {
				_ = e.insertEventAt(context.Background(), EventSuspend, e.lastActiveAt, nil)
				e.reconcileCurrentDay(context.Background(), e.lastActiveAt)
			}
			return nil
		case sleeping := <-e.sleepEvents:
			if sleeping {
				e.handleSuspendSignal(ctx)
			} else {
				// Resumed: re-poll immediately instead of waiting up to
				// pollInterval for the next tick, so the post-resume state
				// (active/locked/idle) and its timestamp are as accurate as
				// possible.
				poll()
			}
		case <-ticker.C:
			poll()
		}
	}
}

// Process executes a single polling cycle. Useful for tests.
func (e *Engine) Process(ctx context.Context) error {
	return e.tick(ctx)
}

func (e *Engine) tick(ctx context.Context) error {
	state, err := e.provider.CurrentState(ctx)
	if err != nil {
		return err
	}

	now := e.clock.Now()

	// Check for unexpected large gaps in polling (e.g. system suspend/hibernate)
	// while we were active. We do this before processing the current state so
	// we don't miss the gap if we wake up directly into a locked or idle state.
	if e.lastState == ActivityActive && !e.lastActiveAt.IsZero() {
		gap := now.Sub(e.lastActiveAt)
		if gap > e.idleThreshold+2*e.pollInterval {
			if err := e.insertEventAt(ctx, EventSuspend, e.lastActiveAt, nil); err != nil {
				return err
			}
			e.lastState = ActivitySuspended
			e.reconcileCurrentDay(ctx, e.lastActiveAt)
		}
	}

	switch state {
	case ActivityActive:
		e.lastActiveAt = now
		if e.lastState != ActivityActive {
			eventType := EventActive
			switch e.lastState {
			case ActivityLocked:
				eventType = EventUnlocked
			case ActivitySuspended:
				eventType = EventResume
			}
			if err := e.insertEvent(ctx, eventType, nil); err != nil {
				return err
			}
			e.lastState = ActivityActive
		}
		// Reconcile on every tick while active so the open block is visible
		// even when no state change occurs.
		e.reconcileCurrentDay(ctx, now)
	case ActivityIdle:
		if e.lastState == ActivityActive || e.lastState == ActivityUnknown {
			// The provider already enforces idleThreshold. If it returns ActivityIdle,
			// the user has been idle for exactly idleThreshold.
			pauseStart := now.Add(-e.idleThreshold)
			if err := e.insertEventAt(ctx, EventIdle, pauseStart, nil); err != nil {
				return err
			}
			e.reconcileCurrentDay(ctx, pauseStart)
			e.lastState = ActivityIdle
		}
	case ActivityLocked:
		if e.lastState != ActivityLocked {
			if err := e.insertEvent(ctx, EventLocked, nil); err != nil {
				return err
			}
			e.reconcileCurrentDay(ctx, now)
			e.lastState = ActivityLocked
		}
	case ActivitySuspended:
		if e.lastState != ActivitySuspended {
			if err := e.insertEvent(ctx, EventSuspend, nil); err != nil {
				return err
			}
			e.reconcileCurrentDay(ctx, now)
			e.lastState = ActivitySuspended
		}
	case ActivityUnknown:
		// Do nothing until state is known again.
	}

	return nil
}

// handleSuspendSignal records a suspend event at the current time in
// response to logind's PrepareForSleep(true) signal. Unlike the polling-based
// gap detection in tick, which can only backdate the suspend to the last
// successful poll, this is recorded at the moment the signal arrives, so it
// is a no-op if the engine already knows it is suspended (e.g. tick's gap
// detection or a duplicate signal already handled it).
func (e *Engine) handleSuspendSignal(ctx context.Context) {
	if e.lastState == ActivitySuspended {
		return
	}

	now := e.clock.Now()
	if err := e.insertEventAt(ctx, EventSuspend, now, nil); err != nil {
		slog.Error("insert suspend event failed", "error", err)
		return
	}
	e.lastState = ActivitySuspended
	e.reconcileCurrentDay(ctx, now)
}

func (e *Engine) reconcileCurrentDay(ctx context.Context, t time.Time) {
	if e.reconciler == nil {
		return
	}
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	if !e.lastReconciledDay.IsZero() && !e.lastReconciledDay.Equal(day) {
		if err := e.reconciler.RebuildDay(ctx, t.Location(), e.lastReconciledDay); err != nil {
			slog.Error("reconcile previous day failed", "error", err)
		}
	}
	e.lastReconciledDay = day
	if err := e.reconciler.RebuildDay(ctx, t.Location(), day); err != nil {
		slog.Error("reconcile failed", "error", err)
	}
}

func (e *Engine) insertEvent(ctx context.Context, eventType EventType, metadata map[string]any) error {
	return e.insertEventAt(ctx, eventType, e.clock.Now(), metadata)
}

func (e *Engine) insertEventAt(ctx context.Context, eventType EventType, occurredAt time.Time, metadata map[string]any) error {
	metaJSON := sql.NullString{}
	if metadata != nil {
		b, err := json.Marshal(metadata)
		if err != nil {
			return err
		}
		metaJSON = sql.NullString{String: string(b), Valid: true}
	}

	_, err := e.db.ExecContext(ctx, `
		INSERT INTO activity_events (occurred_at, event_type, provider, metadata_json, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, occurredAt.UTC().Format(time.RFC3339Nano), string(eventType), e.provider.Name(), metaJSON, e.clock.Now().UTC().Format(time.RFC3339Nano))
	return err
}
