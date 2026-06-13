package activity

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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
	tailCredit    time.Duration
	pollInterval  time.Duration

	lastState    ActivityState
	lastActiveAt time.Time
}

// NewEngine creates a new activity engine.
func NewEngine(db *sql.DB, provider ActivityProvider, clk clock.Clock, idleThreshold, tailCredit, pollInterval time.Duration) *Engine {
	return &Engine{
		db:            db,
		provider:      provider,
		clock:         clk,
		reconciler:    NewReconciler(db, clk, tailCredit),
		idleThreshold: idleThreshold,
		tailCredit:    tailCredit,
		pollInterval:  pollInterval,
		lastState:     ActivityUnknown,
	}
}

// RecordServiceStarted writes the service_started event.
func (e *Engine) RecordServiceStarted(ctx context.Context) error {
	return e.insertEvent(ctx, EventServiceStarted, nil)
}

// RecordServiceStopped writes the service_stopped event.
func (e *Engine) RecordServiceStopped(ctx context.Context) error {
	return e.insertEvent(ctx, EventServiceStopped, nil)
}

// Run polls the provider until the context is cancelled.
func (e *Engine) Run(ctx context.Context) error {
	if err := e.RecordServiceStarted(ctx); err != nil {
		return fmt.Errorf("record service started: %w", err)
	}
	defer func() {
		_ = e.RecordServiceStopped(context.Background())
	}()

	ticker := time.NewTicker(e.pollInterval)
	defer ticker.Stop()

	var lastErr error
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := e.Process(ctx); err != nil {
				if lastErr == nil {
					_ = e.insertEvent(ctx, EventProviderError, map[string]any{"error": err.Error()})
				}
				lastErr = err
			} else {
				lastErr = nil
			}
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
			e.reconcileCurrentDay(ctx, now)
			e.lastState = ActivityActive
		}
	case ActivityIdle:
		if e.lastState == ActivityActive || e.lastState == ActivityUnknown {
			if now.Sub(e.lastActiveAt) >= e.idleThreshold {
				pauseStart := e.lastActiveAt.Add(e.idleThreshold)
				if err := e.insertEventAt(ctx, EventIdle, pauseStart, nil); err != nil {
					return err
				}
				e.reconcileCurrentDay(ctx, pauseStart)
				e.lastState = ActivityIdle
			}
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

func (e *Engine) reconcileCurrentDay(ctx context.Context, t time.Time) {
	if e.reconciler == nil {
		return
	}
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
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
	`, occurredAt.Format(time.RFC3339Nano), string(eventType), e.provider.Name(), metaJSON, e.clock.Now().Format(time.RFC3339Nano))
	return err
}
