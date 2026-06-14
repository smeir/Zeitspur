package web

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/smeir/zeitspur/internal/activity"
	"github.com/smeir/zeitspur/internal/booking"
	"github.com/smeir/zeitspur/internal/closure"
	"github.com/smeir/zeitspur/internal/config"
	"github.com/smeir/zeitspur/internal/i18n"
	"github.com/smeir/zeitspur/internal/timeline"
)

func (s *Server) handleToday(w http.ResponseWriter, r *http.Request) {
	date := s.clk.Now().In(s.location()).Format("2006-01-02")
	http.Redirect(w, r, "/day/"+date, http.StatusFound)
}

func (s *Server) handleTodayStatus(w http.ResponseWriter, r *http.Request) {
	date := s.clk.Now().In(s.location()).Format("2006-01-02")
	d := s.dayData(r, date)
	s.render(w, r, "today", d, http.StatusOK)
}

func (s *Server) handleDay(w http.ResponseWriter, r *http.Request) {
	date := chi.URLParam(r, "date")
	dateObj, err := time.Parse("2006-01-02", date)
	if err != nil {
		http.Error(w, "invalid date", http.StatusBadRequest)
		return
	}
	d := s.dayData(r, date)
	d["DateObj"] = dateObj
	d["Title"] = "PageDay"
	d["Nav"] = "today"
	s.renderLayout(w, r, "today", d, http.StatusOK)
}

func (s *Server) dayData(r *http.Request, date string) map[string]any {
	ctx := r.Context()
	tsvc := timeline.NewService(s.db)
	sum, err := tsvc.Day(ctx, date)
	if err != nil {
		slog.Error("timeline day failed", "error", err)
	}

	br := booking.NewRepository(s.db)
	status, _ := br.GetDay(ctx, date)

	state := activity.ActivityUnknown
	if s.provider != nil {
		state, _ = s.provider.CurrentState(ctx)
	}

	blocks, _ := s.blocksForDay(ctx, date)

	var running *time.Time
	var runningMinutes int
	if state == activity.ActivityActive {
		running, runningMinutes = s.currentBlock(ctx, date)
	}

	dateObj, _ := time.Parse("2006-01-02", date)

	return map[string]any{
		"Date":                date,
		"DateObj":             dateObj,
		"Status":              string(state),
		"CurrentBlockStart":   running,
		"RunningMinutes":      runningMinutes,
		"WorkedMinutes":       sum.WorkedMinutes,
		"PauseMinutes":        sum.PauseMinutes,
		"Booked":              status.Booked,
		"ChangedAfterBooking": status.Booked && status.BookingRevision < status.CurrentRevision,
		"Blocks":              blocks,
	}
}

// currentBlock returns an open running block derived from recent activity events.
func (s *Server) currentBlock(ctx context.Context, date string) (*time.Time, int) {
	loc := s.location()
	day, err := time.ParseInLocation("2006-01-02", date, loc)
	if err != nil {
		return nil, 0
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT occurred_at, event_type FROM activity_events
		WHERE occurred_at >= ?
		ORDER BY occurred_at ASC, id ASC
	`, day.AddDate(0, 0, -1).Format(time.RFC3339Nano))
	if err != nil {
		return nil, 0
	}
	defer rows.Close()

	startEvents := map[string]bool{
		string(activity.EventActive):   true,
		string(activity.EventUnlocked): true,
	}
	endEvents := map[string]bool{
		string(activity.EventIdle):    true,
		string(activity.EventLocked):  true,
		string(activity.EventSuspend): true,
	}
	var blockStart *time.Time
	for rows.Next() {
		var ts, typ string
		if err := rows.Scan(&ts, &typ); err != nil {
			return nil, 0
		}
		t, err := time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			continue
		}
		if startEvents[typ] {
			if blockStart == nil {
				blockStart = &t
			}
		}
		if endEvents[typ] {
			blockStart = nil
		}
	}
	if err := rows.Err(); err != nil || blockStart == nil {
		return nil, 0
	}

	now := s.clk.Now().In(loc)
	if now.Before(*blockStart) {
		return blockStart, 0
	}
	minutes := int(now.Sub(*blockStart).Minutes())
	return blockStart, minutes
}

type blockView struct {
	ID     int
	Date   string
	Start  time.Time
	End    time.Time
	Source string
	Note   string
}

func (s *Server) blocksForDay(ctx context.Context, date string) ([]blockView, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, work_date, started_at, ended_at, source, status, note
		FROM work_blocks
		WHERE work_date = ? AND status NOT IN ('deleted', 'ignored')
		ORDER BY started_at ASC
	`, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var blocks []blockView
	for rows.Next() {
		var b blockView
		var status, note sql.NullString
		var startStr, endStr string
		if err := rows.Scan(&b.ID, &b.Date, &startStr, &endStr, &b.Source, &status, &note); err != nil {
			return nil, err
		}
		b.Start, _ = time.Parse(time.RFC3339Nano, startStr)
		b.End, _ = time.Parse(time.RFC3339Nano, endStr)
		if note.Valid {
			b.Note = note.String
		}
		blocks = append(blocks, b)
	}
	return blocks, rows.Err()
}

func (s *Server) handleDayBook(w http.ResponseWriter, r *http.Request) {
	date := chi.URLParam(r, "date")
	booked := r.FormValue("booked") == "true"
	br := booking.NewRepository(s.db)
	if err := br.SetBooked(r.Context(), date, booked); err != nil {
		slog.Error("set booked failed", "error", err)
	}
	http.Redirect(w, r, "/day/"+date, http.StatusFound)
}

func (s *Server) handleDayBlock(w http.ResponseWriter, r *http.Request) {
	date := chi.URLParam(r, "date")
	start := r.FormValue("start")
	end := r.FormValue("end")
	note := r.FormValue("note")

	s0, e0, err := s.parseDayTimes(date, start, end)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(r.Context(), `
		INSERT INTO work_blocks (work_date, started_at, ended_at, source, status, note, created_at, updated_at)
		VALUES (?, ?, ?, 'manual', 'active', ?, ?, ?)
	`, date, s0.Format(time.RFC3339Nano), e0.Format(time.RFC3339Nano), note, now, now)
	if err != nil {
		slog.Error("insert manual block failed", "error", err)
	}

	br := booking.NewRepository(s.db)
	_ = br.BumpRevision(r.Context(), date)
	http.Redirect(w, r, "/day/"+date, http.StatusFound)
}

func (s *Server) handleBlockIgnore(w http.ResponseWriter, r *http.Request) {
	date := chi.URLParam(r, "date")
	idStr := chi.URLParam(r, "id")
	_, err := s.db.ExecContext(r.Context(), `UPDATE work_blocks SET status = 'ignored', updated_at = ? WHERE id = ?`, time.Now().UTC().Format(time.RFC3339Nano), idStr)
	if err != nil {
		slog.Error("ignore block failed", "error", err)
	}
	br := booking.NewRepository(s.db)
	_ = br.BumpRevision(r.Context(), date)
	http.Redirect(w, r, "/day/"+date, http.StatusFound)
}

func (s *Server) handleWeek(w http.ResponseWriter, r *http.Request) {
	now := s.clk.Now().In(s.location())
	wd := int(now.Weekday())
	if wd == 0 {
		wd = 7
	}
	start := now.AddDate(0, 0, -(wd - 1))
	start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, s.location())
	end := start.AddDate(0, 0, 6)

	startStr := start.Format("2006-01-02")
	endStr := end.Format("2006-01-02")

	tsvc := timeline.NewService(s.db)
	days, _ := tsvc.Range(r.Context(), startStr, endStr)

	total, booked, unbooked := 0, 0, 0
	for _, d := range days {
		total += d.WorkedMinutes
		if d.Booked {
			booked++
		} else {
			unbooked++
		}
	}

	s.renderLayout(w, r, "week", map[string]any{
		"Title":        "PageWeek",
		"Nav":          "week",
		"WeekStart":    start,
		"WeekEnd":      end,
		"Days":         days,
		"TotalMinutes": total,
		"BookedDays":   booked,
		"UnbookedDays": unbooked,
	}, http.StatusOK)
}

func (s *Server) handleMonth(w http.ResponseWriter, r *http.Request) {
	date := s.clk.Now().In(s.location())
	if d := r.URL.Query().Get("date"); d != "" {
		if parsed, err := time.Parse("2006-01-02", d); err == nil {
			date = parsed.In(s.location())
		}
	}
	monthStart := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, s.location())
	monthEnd := monthStart.AddDate(0, 1, -1)

	startStr := monthStart.Format("2006-01-02")
	endStr := monthEnd.Format("2006-01-02")

	tsvc := timeline.NewService(s.db)
	days, _ := tsvc.Range(r.Context(), startStr, endStr)
	dayMap := make(map[string]*timeline.DaySummary)
	maxWork := 1
	for _, d := range days {
		dayMap[d.Date] = d
		if d.WorkedMinutes > maxWork {
			maxWork = d.WorkedMinutes
		}
	}

	type cell struct {
		Empty               bool
		Date                string
		DayNum              int
		WorkedMinutes       int
		HasWork             bool
		BarWidth            int
		Booked              bool
		ChangedAfterBooking bool
	}

	var cells []cell
	// pad leading cells
	firstWeekday := int(monthStart.Weekday())
	for i := 0; i < firstWeekday; i++ {
		cells = append(cells, cell{Empty: true})
	}
	for d := monthStart; !d.After(monthEnd); d = d.AddDate(0, 0, 1) {
		dateStr := d.Format("2006-01-02")
		sum := dayMap[dateStr]
		worked := 0
		booked := false
		changed := false
		if sum != nil {
			worked = sum.WorkedMinutes
			booked = sum.Booked
			changed = sum.ChangedAfterBooking
		}
		cells = append(cells, cell{
			Empty:               false,
			Date:                dateStr,
			DayNum:              d.Day(),
			WorkedMinutes:       worked,
			HasWork:             worked > 0,
			BarWidth:            (worked * 100) / maxWork,
			Booked:              booked,
			ChangedAfterBooking: changed,
		})
	}

	s.renderLayout(w, r, "month", map[string]any{
		"Title":     "PageMonth",
		"Nav":       "month",
		"Month":     monthStart,
		"PrevMonth": monthStart.AddDate(0, -1, 0),
		"NextMonth": monthStart.AddDate(0, 1, 0),
		"Cells":     cells,
	}, http.StatusOK)
}

func (s *Server) handleBooking(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	br := booking.NewRepository(s.db)
	bookingDay, _ := br.GetBookingDay(ctx)

	data := map[string]any{
		"Title": "PageBooking",
		"Nav":   "booking",
	}

	if bookingDay != nil {
		data["BookingDay"] = bookingDay
		now := s.clk.Now().In(s.location()).Truncate(24 * time.Hour)
		data["Overdue"] = bookingDay.Before(now)
		data["DaysRemaining"] = int(bookingDay.Sub(now).Hours() / 24)

		periodStart, err := closure.NewRepository(s.db).PeriodStart(ctx, *bookingDay)
		if err != nil {
			slog.Error("period start failed", "error", err)
		}
		periodStartStr := periodStart.Format("2006-01-02")
		periodEndStr := bookingDay.Format("2006-01-02")
		data["PeriodStart"] = periodStart
		data["PeriodEnd"] = bookingDay

		tsvc := timeline.NewService(s.db)
		days, _ := tsvc.Range(ctx, periodStartStr, periodEndStr)
		data["Days"] = days
	}

	s.renderLayout(w, r, "booking", data, http.StatusOK)
}

func (s *Server) handleBookingDay(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	br := booking.NewRepository(s.db)
	action := r.FormValue("action")
	switch action {
	case "set":
		dateStr := r.FormValue("date")
		d, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			http.Error(w, "invalid date", http.StatusBadRequest)
			return
		}
		_ = br.SetBookingDay(ctx, d)
	case "remove":
		_ = br.ClearBookingDay(ctx)
	}
	http.Redirect(w, r, "/booking", http.StatusFound)
}

func (s *Server) handleBookingBulk(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	booked := r.FormValue("booked") == "true"
	br := booking.NewRepository(s.db)
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	for _, date := range r.Form["dates"] {
		if err := br.SetBooked(ctx, date, booked); err != nil {
			slog.Error("bulk book failed", "error", err)
		}
	}
	http.Redirect(w, r, "/booking", http.StatusFound)
}

func (s *Server) handleClosePreview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	br := booking.NewRepository(s.db)
	bookingDay, err := br.GetBookingDay(ctx)
	if err != nil || bookingDay == nil {
		http.Error(w, "no booking day", http.StatusBadRequest)
		return
	}

	periodStart, err := closure.NewRepository(s.db).PeriodStart(ctx, *bookingDay)
	if err != nil {
		slog.Error("period start failed", "error", err)
		http.Error(w, "failed to compute period start", http.StatusInternalServerError)
		return
	}
	periodStartStr := periodStart.Format("2006-01-02")
	periodEndStr := bookingDay.Format("2006-01-02")

	tsvc := timeline.NewService(s.db)
	days, _ := tsvc.Range(ctx, periodStartStr, periodEndStr)

	var summaries []closure.DaySummary
	var tracked, booked, unbooked, total int
	for _, d := range days {
		summaries = append(summaries, closure.DaySummary{
			Date:            d.Date,
			Booked:          d.Booked,
			BookingRevision: d.BookingRevision,
			CurrentRevision: d.CurrentRevision,
			TrackedMinutes:  d.WorkedMinutes,
		})
		tracked++
		total += d.WorkedMinutes
		if d.Booked {
			booked++
		} else {
			unbooked++
		}
	}

	s.render(w, r, "close_preview", map[string]any{
		"BookingDay":         bookingDay,
		"PeriodStart":        periodStart,
		"PeriodEnd":          bookingDay,
		"Tracked":            tracked,
		"Booked":             booked,
		"Unbooked":           unbooked,
		"TotalMinutes":       total,
		"HasUnbookedWarning": unbooked > 0,
		"UnbookedCount":      unbooked,
	}, http.StatusOK)
}

func (s *Server) handleClose(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	confirmed := r.FormValue("confirm") == "true"
	br := booking.NewRepository(s.db)
	bookingDay, err := br.GetBookingDay(ctx)
	if err != nil || bookingDay == nil {
		http.Error(w, "no booking day", http.StatusBadRequest)
		return
	}

	cr := closure.NewRepository(s.db)
	locked, _ := cr.AcquireLock(ctx)
	if !locked {
		http.Error(w, "another closure is in progress", http.StatusConflict)
		return
	}
	defer cr.ReleaseLock(ctx)

	periodStart, err := cr.PeriodStart(ctx, *bookingDay)
	if err != nil {
		slog.Error("period start failed", "error", err)
		http.Error(w, "failed to compute period start", http.StatusInternalServerError)
		return
	}
	periodStartStr := periodStart.Format("2006-01-02")
	periodEndStr := bookingDay.Format("2006-01-02")

	tsvc := timeline.NewService(s.db)
	days, _ := tsvc.Range(ctx, periodStartStr, periodEndStr)
	if len(days) == 0 {
		http.Error(w, "no days to close", http.StatusBadRequest)
		return
	}

	var unbooked int
	var summaries []closure.DaySummary
	for _, d := range days {
		if !d.Booked {
			unbooked++
		}
		summaries = append(summaries, closure.DaySummary{
			Date:            d.Date,
			Booked:          d.Booked,
			BookingRevision: d.BookingRevision,
			CurrentRevision: d.CurrentRevision,
			TrackedMinutes:  d.WorkedMinutes,
		})
	}

	if unbooked > 0 && !confirmed {
		http.Error(w, "unbooked days require confirmation", http.StatusBadRequest)
		return
	}

	if _, err := cr.Create(ctx, periodEndStr, summaries); err != nil {
		slog.Error("closure failed", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = br.ClearBookingDay(ctx)
	http.Redirect(w, r, "/closures", http.StatusFound)
}

func (s *Server) handleClosures(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cr := closure.NewRepository(s.db)
	closures, _ := cr.List(ctx)

	br := booking.NewRepository(s.db)
	type closureView struct {
		closure.Closure
		Differs bool
	}
	var views []closureView
	for _, c := range closures {
		statuses, _ := br.ListDaysInRange(ctx, c.PeriodStart, c.PeriodEnd)
		differs, _ := cr.HasDifferenceSinceClosure(ctx, c.ID, statuses)
		views = append(views, closureView{Closure: c, Differs: differs})
	}

	s.renderLayout(w, r, "closures", map[string]any{
		"Title":    "PageClosures",
		"Nav":      "closures",
		"Closures": views,
	}, http.StatusOK)
}

func (s *Server) handleClosureDetail(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, _ := strconv.Atoi(idStr)
	cr := closure.NewRepository(s.db)
	c, err := cr.Get(r.Context(), id)
	if err != nil || c == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	br := booking.NewRepository(s.db)
	statuses, _ := br.ListDaysInRange(r.Context(), c.PeriodStart, c.PeriodEnd)
	differs, _ := cr.HasDifferenceSinceClosure(r.Context(), c.ID, statuses)

	s.renderLayout(w, r, "closure_detail", map[string]any{
		"Title":   "PageClosureDetail",
		"Nav":     "closures",
		"Closure": c,
		"Differs": differs,
	}, http.StatusOK)
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	s.renderLayout(w, r, "settings", map[string]any{
		"Title":      "PageSettings",
		"Nav":        "settings",
		"Config":     s.config(),
		"ConfigPath": s.paths.ConfigFile,
		"Timezones":  []string{"UTC", "Europe/Berlin", "Europe/London", "America/New_York", "America/Los_Angeles", "Asia/Tokyo"},
		"Languages":  []i18n.Locale{i18n.German, i18n.English},
		"Version":    s.version,
	}, http.StatusOK)
}

func (s *Server) handleSettingsSave(w http.ResponseWriter, r *http.Request) {
	// For Version 1, settings changes are written back to config.toml.
	// A full reload requires restart; we persist immediately.
	cfg := s.config()
	cfg.Activity.PollInterval = r.FormValue("poll_interval")
	cfg.Activity.IdleThreshold = r.FormValue("idle_threshold")
	cfg.Server.ListenAddress = r.FormValue("listen_address")
	cfg.App.Timezone = r.FormValue("timezone")
	cfg.App.Language = r.FormValue("language")

	if err := config.Write(s.paths.ConfigFile, cfg); err != nil {
		slog.Error("write config failed", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	loc := time.Local
	if cfg.App.Timezone != "" && cfg.App.Timezone != "local" {
		if l, err := time.LoadLocation(cfg.App.Timezone); err == nil {
			loc = l
		}
	}
	s.setConfig(cfg, loc)

	http.Redirect(w, r, "/settings", http.StatusFound)
}

func (s *Server) parseDayTimes(date, start, end string) (time.Time, time.Time, error) {
	loc := s.location()
	s0, err := time.ParseInLocation("2006-01-02T15:04", date+"T"+start, loc)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid start time: %w", err)
	}
	e0, err := time.ParseInLocation("2006-01-02T15:04", date+"T"+end, loc)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid end time: %w", err)
	}
	if !e0.After(s0) {
		return time.Time{}, time.Time{}, fmt.Errorf("end must be after start")
	}
	return s0, e0, nil
}
