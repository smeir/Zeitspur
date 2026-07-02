// Package timeline computes daily working-time summaries from stored blocks.
package timeline

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
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

type interval struct {
	start time.Time
	end   time.Time
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
		SELECT work_date, started_at, ended_at, status
		FROM work_blocks
		WHERE work_date >= ? AND work_date <= ?
		ORDER BY work_date ASC, started_at ASC
	`, start, end)
	if err != nil {
		return nil, fmt.Errorf("query work_blocks: %w", err)
	}
	defer blockRows.Close()

	intervalsByDate := make(map[string][]interval)
	for blockRows.Next() {
		var date, startStr, endStr, status string
		if err := blockRows.Scan(&date, &startStr, &endStr, &status); err != nil {
			return nil, err
		}
		if _, ok := statusByDate[date]; !ok {
			dateObj, _ := time.Parse("2006-01-02", date)
			statusByDate[date] = &DaySummary{Date: date, DateObj: dateObj}
		}
		if status == "deleted" || status == "ignored" {
			continue
		}
		var iv interval
		if iv.start, err = time.Parse(time.RFC3339Nano, startStr); err != nil {
			return nil, fmt.Errorf("parse started_at %q: %w", startStr, err)
		}
		if iv.end, err = time.Parse(time.RFC3339Nano, endStr); err != nil {
			return nil, fmt.Errorf("parse ended_at %q: %w", endStr, err)
		}
		if iv.end.Before(iv.start) {
			iv.end = iv.start
		}
		intervalsByDate[date] = append(intervalsByDate[date], iv)
	}
	if err := blockRows.Err(); err != nil {
		return nil, err
	}

	// Aggregate per day. Overlapping blocks (e.g. a manual block over a detected
	// one) are merged so overlapping time counts only once; pauses are the gaps
	// between the merged intervals. Durations are summed before converting to
	// minutes so per-block rounding does not accumulate.
	for date, intervals := range intervalsByDate {
		ds := statusByDate[date]
		ds.BlockCount = len(intervals)
		merged := mergeIntervals(intervals)
		var worked, pause time.Duration
		for i, iv := range merged {
			worked += iv.end.Sub(iv.start)
			if i > 0 {
				pause += iv.start.Sub(merged[i-1].end)
			}
		}
		ds.WorkedMinutes = int(worked.Minutes())
		ds.PauseMinutes = int(pause.Minutes())
		ds.TotalMinutes = ds.WorkedMinutes + ds.PauseMinutes
	}

	result := make([]*DaySummary, 0, len(statusByDate))
	for _, ds := range statusByDate {
		result = append(result, ds)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Date < result[j].Date })
	return result, nil
}

// mergeIntervals merges overlapping or touching intervals. The input must be
// sorted by start time.
func mergeIntervals(intervals []interval) []interval {
	var merged []interval
	for _, iv := range intervals {
		if n := len(merged); n > 0 && !iv.start.After(merged[n-1].end) {
			if iv.end.After(merged[n-1].end) {
				merged[n-1].end = iv.end
			}
			continue
		}
		merged = append(merged, iv)
	}
	return merged
}

// MinutesBetween sums active work-block minutes between two timestamps.
func (s *Service) MinutesBetween(ctx context.Context, start, end time.Time) (int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT started_at, ended_at FROM work_blocks
		WHERE status = 'active' AND started_at < ? AND ended_at > ?
	`, end.UTC().Format(time.RFC3339Nano), start.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var total time.Duration
	for rows.Next() {
		var startStr, endStr string
		if err := rows.Scan(&startStr, &endStr); err != nil {
			return 0, err
		}
		s0, err := time.Parse(time.RFC3339Nano, startStr)
		if err != nil {
			return 0, fmt.Errorf("parse started_at %q: %w", startStr, err)
		}
		e0, err := time.Parse(time.RFC3339Nano, endStr)
		if err != nil {
			return 0, fmt.Errorf("parse ended_at %q: %w", endStr, err)
		}
		if s0.Before(start) {
			s0 = start
		}
		if e0.After(end) {
			e0 = end
		}
		if d := e0.Sub(s0); d > 0 {
			total += d
		}
	}
	return int(total.Minutes()), rows.Err()
}

// NullTimeToString returns an empty string for a NULL time.
func NullTimeToString(t sql.NullTime) string {
	if t.Valid {
		return t.Time.Format("15:04")
	}
	return ""
}
