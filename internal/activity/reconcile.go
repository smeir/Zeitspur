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
	Open  bool
}

// Reconciler computes work blocks from activity events.
type Reconciler struct {
	db    *sql.DB
	clock clock.Clock
}

// NewReconciler creates a reconciler.
func NewReconciler(db *sql.DB, clk clock.Clock) *Reconciler {
	return &Reconciler{db: db, clock: clk}
}

// RebuildDay recalculates detected work blocks for a single calendar day.
func (r *Reconciler) RebuildDay(ctx context.Context, loc *time.Location, day time.Time) error {
	start := day.In(loc)
	start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, loc)
	// Load a small window around the day to capture blocks crossing midnight.
	windowStart := start.AddDate(0, 0, -1)
	windowEnd := start.AddDate(0, 0, 2)

	blocks, err := r.computeBlocks(ctx, windowStart, windowEnd, r.clock.Now())
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

func (r *Reconciler) computeBlocks(ctx context.Context, start, end, now time.Time) ([]Block, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT occurred_at, event_type FROM activity_events
		WHERE occurred_at >= ? AND occurred_at < ?
		ORDER BY occurred_at ASC, id ASC
	`, start.Format(time.RFC3339Nano), end.Format(time.RFC3339Nano))
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	return r.buildBlocks(rows, now)
}

type eventRow struct {
	occurredAt time.Time
	typ        string
}

func (r *Reconciler) buildBlocks(rows *sql.Rows, now time.Time) ([]Block, error) {
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
	for _, e := range events {
		if startEvents[e.typ] {
			if !inBlock {
				blockStart = e.occurredAt
				inBlock = true
			}
		}
		if endEvents[e.typ] && inBlock {
			end := e.occurredAt
			if end.Before(blockStart) {
				end = blockStart
			}
			blocks = append(blocks, Block{Start: blockStart, End: end})
			inBlock = false
		}
	}

	// Persist an open block so the current active period is visible even before
	// an idle/locked/suspend event ends it.
	if inBlock {
		end := now
		if end.Before(blockStart) {
			end = blockStart
		}
		blocks = append(blocks, Block{Start: blockStart, End: end, Open: true})
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

	ignored, err := r.loadIgnoredBlocks(ctx, tx, dateStr)
	if err != nil {
		return err
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
		if b.Open {
			var err error
			segEnd, ignored, err = r.extendOverlappingIgnoredBlocks(ctx, tx, segStart, segEnd, ignored, now)
			if err != nil {
				return err
			}
		}
		for _, segment := range r.subtractIgnoredBlocks(segStart, segEnd, ignored) {
			segDate := segment.Start.Format("2006-01-02")
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO work_blocks (work_date, started_at, ended_at, source, status, created_at, updated_at)
				VALUES (?, ?, ?, 'detected', 'active', ?, ?)
			`, segDate, segment.Start.Format(time.RFC3339Nano), segment.End.Format(time.RFC3339Nano), now, now); err != nil {
				return fmt.Errorf("insert detected block: %w", err)
			}
		}
	}

	return tx.Commit()
}

type ignoredBlock struct {
	ID int64
	Block
}

func (r *Reconciler) loadIgnoredBlocks(ctx context.Context, tx *sql.Tx, dateStr string) ([]ignoredBlock, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, started_at, ended_at FROM work_blocks
		WHERE work_date = ? AND source = 'detected' AND status = 'ignored'
	`, dateStr)
	if err != nil {
		return nil, fmt.Errorf("query ignored blocks: %w", err)
	}
	defer rows.Close()

	var ignored []ignoredBlock
	for rows.Next() {
		var id int64
		var startStr, endStr string
		if err := rows.Scan(&id, &startStr, &endStr); err != nil {
			return nil, err
		}
		start, _ := time.Parse(time.RFC3339Nano, startStr)
		end, _ := time.Parse(time.RFC3339Nano, endStr)
		ignored = append(ignored, ignoredBlock{
			ID:    id,
			Block: Block{Start: start, End: end},
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ignored, nil
}

func (r *Reconciler) extendOverlappingIgnoredBlocks(ctx context.Context, tx *sql.Tx, start, end time.Time, ignored []ignoredBlock, now string) (time.Time, []ignoredBlock, error) {
	for i, ig := range ignored {
		if ig.Start.After(start) || !blocksOverlap(start, end, ig.Start, ig.End) || !ig.End.Before(end) {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE work_blocks SET ended_at = ?, updated_at = ? WHERE id = ?
		`, end.Format(time.RFC3339Nano), now, ig.ID); err != nil {
			return end, ignored, fmt.Errorf("extend ignored block: %w", err)
		}
		ignored[i].End = end
	}
	return end, ignored, nil
}

func (r *Reconciler) subtractIgnoredBlocks(start, end time.Time, ignored []ignoredBlock) []Block {
	segments := []Block{{Start: start, End: end}}
	for _, ig := range ignored {
		var next []Block
		for _, segment := range segments {
			if !blocksOverlap(segment.Start, segment.End, ig.Start, ig.End) {
				next = append(next, segment)
				continue
			}
			if segment.Start.Before(ig.Start) {
				next = append(next, Block{Start: segment.Start, End: ig.Start})
			}
			if ig.End.Before(segment.End) {
				next = append(next, Block{Start: ig.End, End: segment.End})
			}
		}
		segments = next
	}
	return segments
}

func blocksOverlap(aStart, aEnd, bStart, bEnd time.Time) bool {
	return aStart.Before(bEnd) && bStart.Before(aEnd)
}
