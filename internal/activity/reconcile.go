package activity

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/smeir/zeitspur/internal/clock"
)

// Block represents a calculated working period.
type Block struct {
	Start time.Time
	End   time.Time
}

// Reconciler computes work blocks from activity events.
type Reconciler struct {
	db         *sql.DB
	clock      clock.Clock
	tailCredit time.Duration
}

// NewReconciler creates a reconciler.
func NewReconciler(db *sql.DB, clk clock.Clock, tailCredit time.Duration) *Reconciler {
	return &Reconciler{db: db, clock: clk, tailCredit: tailCredit}
}

// RebuildDay recalculates detected work blocks for a single calendar day.
func (r *Reconciler) RebuildDay(ctx context.Context, loc *time.Location, day time.Time) error {
	start := day.In(loc)
	start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, loc)
	// Load a small window around the day to capture blocks crossing midnight.
	windowStart := start.AddDate(0, 0, -1)
	windowEnd := start.AddDate(0, 0, 2)

	blocks, err := r.computeBlocks(ctx, windowStart, windowEnd)
	if err != nil {
		return err
	}

	// Keep only blocks overlapping the requested day.
	var dayBlocks []Block
	dayEnd := start.AddDate(0, 0, 1)
	for _, b := range blocks {
		if b.Start.Before(dayEnd) && b.End.After(start) {
			dayBlocks = append(dayBlocks, b)
		}
	}

	return r.persistDetectedBlocks(ctx, start, dayBlocks)
}

func (r *Reconciler) computeBlocks(ctx context.Context, start, end time.Time) ([]Block, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT occurred_at, event_type FROM activity_events
		WHERE occurred_at >= ? AND occurred_at < ?
		ORDER BY occurred_at ASC, id ASC
	`, start.Format(time.RFC3339Nano), end.Format(time.RFC3339Nano))
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	return r.buildBlocks(rows)
}

type eventRow struct {
	occurredAt time.Time
	typ        string
}

func (r *Reconciler) buildBlocks(rows *sql.Rows) ([]Block, error) {
	var events []eventRow
	for rows.Next() {
		var e eventRow
		var ts string
		if err := rows.Scan(&ts, &e.typ); err != nil {
			return nil, err
		}
		t, err := time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			return nil, err
		}
		e.occurredAt = t
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var blocks []Block
	var inBlock bool
	var blockStart time.Time

	startEvents := map[string]bool{
		string(EventActive):   true,
		string(EventUnlocked): true,
		string(EventResume):   true,
	}
	endEvents := map[string]bool{
		string(EventIdle):    true,
		string(EventLocked):  true,
		string(EventSuspend): true,
	}
	terminators := map[string]bool{
		string(EventServiceStarted): true,
		string(EventServiceStopped): true,
	}

	for _, e := range events {
		if startEvents[e.typ] {
			if !inBlock {
				blockStart = e.occurredAt
				inBlock = true
			}
		}
		if endEvents[e.typ] && inBlock {
			end := e.occurredAt
			if e.typ == string(EventIdle) {
				end = end.Add(r.tailCredit)
			}
			if end.Before(blockStart) {
				end = blockStart
			}
			blocks = append(blocks, Block{Start: blockStart, End: end})
			inBlock = false
		}
		if terminators[e.typ] && inBlock {
			blocks = append(blocks, Block{Start: blockStart, End: e.occurredAt})
			inBlock = false
		}
	}

	return blocks, nil
}

func (r *Reconciler) persistDetectedBlocks(ctx context.Context, day time.Time, blocks []Block) error {
	dateStr := day.Format("2006-01-02")
	now := r.clock.Now().Format(time.RFC3339Nano)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM work_blocks WHERE work_date = ? AND source = 'detected' AND status = 'active'`, dateStr); err != nil {
		return fmt.Errorf("delete old detected blocks: %w", err)
	}

	for _, b := range blocks {
		// Split blocks crossing local midnight, but only store segments for the requested day.
		dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
		next := dayStart.AddDate(0, 0, 1)
		segStart := b.Start
		if segStart.Before(dayStart) {
			segStart = dayStart
		}
		segEnd := b.End
		if segEnd.After(next) {
			segEnd = next
		}
		if !segStart.Before(segEnd) {
			continue
		}
		segDate := segStart.Format("2006-01-02")
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO work_blocks (work_date, started_at, ended_at, source, status, created_at, updated_at)
			VALUES (?, ?, ?, 'detected', 'active', ?, ?)
		`, segDate, segStart.Format(time.RFC3339Nano), segEnd.Format(time.RFC3339Nano), now, now); err != nil {
			return fmt.Errorf("insert detected block: %w", err)
		}
	}

	return tx.Commit()
}
