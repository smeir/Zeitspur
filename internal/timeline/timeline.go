// Package timeline computes daily working-time summaries from stored blocks.
package timeline

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// DaySummary represents the aggregated data for a single calendar day.
type DaySummary struct {
	Date          string
	DateObj       time.Time
	WorkedMinutes int
	PauseMinutes  int
	TotalMinutes  int
	BlockCount    int
	Booked        bool
}

// Service computes timeline summaries.
type Service struct {
	db *sql.DB
}

// NewService creates a timeline service.
func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

// Day returns the summary for a single day.
func (s *Service) Day(ctx context.Context, date string) (*DaySummary, error) {
	list, err := s.summarize(ctx, date, date)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		dateObj, _ := time.Parse("2006-01-02", date)
		return &DaySummary{Date: date, DateObj: dateObj}, nil
	}
	return list[0], nil
}

// Range returns summaries for a date range inclusive.
func (s *Service) Range(ctx context.Context, start, end string) ([]*DaySummary, error) {
	return s.summarize(ctx, start, end)
}

func (s *Service) summarize(ctx context.Context, start, end string) ([]*DaySummary, error) {
	// Load day status.
	rows, err := s.db.QueryContext(ctx, `
		SELECT work_date, booked
		FROM day_status
		WHERE work_date >= ? AND work_date <= ?
		ORDER BY work_date ASC
	`, start, end)
	if err != nil {
		return nil, fmt.Errorf("query day_status: %w", err)
	}
	defer rows.Close()

	statusByDate := make(map[string]*DaySummary)
	for rows.Next() {
		var ds DaySummary
		if err := rows.Scan(&ds.Date, &ds.Booked); err != nil {
			return nil, err
		}
		ds.DateObj, _ = time.Parse("2006-01-02", ds.Date)
		statusByDate[ds.Date] = &ds
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Load work blocks.
	blockRows, err := s.db.QueryContext(ctx, `
		SELECT work_date, started_at, ended_at, source, status
		FROM work_blocks
		WHERE work_date >= ? AND work_date <= ?
		ORDER BY work_date ASC, started_at ASC
	`, start, end)
	if err != nil {
		return nil, fmt.Errorf("query work_blocks: %w", err)
	}
	defer blockRows.Close()

	type blockInfo struct {
		date   string
		start  time.Time
		end    time.Time
		source string
		status string
	}
	var blocks []blockInfo
	for blockRows.Next() {
		var b blockInfo
		var startStr, endStr string
		if err := blockRows.Scan(&b.date, &startStr, &endStr, &b.source, &b.status); err != nil {
			return nil, err
		}
		b.start, _ = time.Parse(time.RFC3339Nano, startStr)
		b.end, _ = time.Parse(time.RFC3339Nano, endStr)
		blocks = append(blocks, b)
	}
	if err := blockRows.Err(); err != nil {
		return nil, err
	}

	// Ensure all block dates exist in statusByDate.
	for _, b := range blocks {
		if _, ok := statusByDate[b.date]; !ok {
			dateObj, _ := time.Parse("2006-01-02", b.date)
			statusByDate[b.date] = &DaySummary{Date: b.date, DateObj: dateObj}
		}
	}

	// Aggregate.
	for _, b := range blocks {
		ds := statusByDate[b.date]
		ds.BlockCount++
		if b.status == "deleted" || b.status == "ignored" {
			continue
		}
		d := b.end.Sub(b.start)
		if d < 0 {
			d = 0
		}
		ds.WorkedMinutes += int(d.Minutes())
	}

	// Compute pause minutes as gaps between blocks (active only).
	for _, ds := range statusByDate {
		var dayBlocks []blockInfo
		for _, b := range blocks {
			if b.date == ds.Date && b.status != "deleted" && b.status != "ignored" {
				dayBlocks = append(dayBlocks, b)
			}
		}
		for i := 1; i < len(dayBlocks); i++ {
			gap := dayBlocks[i].start.Sub(dayBlocks[i-1].end)
			if gap > 0 {
				ds.PauseMinutes += int(gap.Minutes())
			}
		}
		ds.TotalMinutes = ds.WorkedMinutes + ds.PauseMinutes
	}

	// Sort result.
	var result []*DaySummary
	for _, ds := range statusByDate {
		result = append(result, ds)
	}
	return result, nil
}

// MinutesBetween sums active work-block minutes between two timestamps.
func (s *Service) MinutesBetween(ctx context.Context, start, end time.Time) (int, error) {
	var total int
	rows, err := s.db.QueryContext(ctx, `
		SELECT started_at, ended_at FROM work_blocks
		WHERE status = 'active' AND started_at < ? AND ended_at > ?
	`, end.Format(time.RFC3339Nano), start.Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	for rows.Next() {
		var startStr, endStr string
		if err := rows.Scan(&startStr, &endStr); err != nil {
			return 0, err
		}
		s0, _ := time.Parse(time.RFC3339Nano, startStr)
		e0, _ := time.Parse(time.RFC3339Nano, endStr)
		if s0.Before(start) {
			s0 = start
		}
		if e0.After(end) {
			e0 = end
		}
		d := e0.Sub(s0)
		if d > 0 {
			total += int(d.Minutes())
		}
	}
	return total, rows.Err()
}

// NullTimeToString returns an empty string for a NULL time.
func NullTimeToString(t sql.NullTime) string {
	if t.Valid {
		return t.Time.Format("15:04")
	}
	return ""
}
