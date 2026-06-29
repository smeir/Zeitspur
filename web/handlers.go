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
	"github.com/smeir/zeitspur/internal/timeutil"
)

func (s *Server) handleToday(w http.ResponseWriter, r *http.Request) {
	date := s.clk.Now().In(s.location()).Format("2006-01-02")
	http.Redirect(w, r, "/day/"+date, http.StatusFound)
}

func (s *Server) handleTodayStatus(w http.ResponseWriter, r *http.Request) {
	date := s.clk.Now().In(s.location()).Format("2006-01-02")
	d := s.dayData(r, date, false)
	s.renderBlock(w, r, "today", "today_status", d, http.StatusOK)
}

func (s *Server) handleDay(w http.ResponseWriter, r *http.Request) {
	date := chi.URLParam(r, "date")
	dateObj, err := time.Parse("2006-01-02", date)
	if err != nil {
		http.Error(w, "invalid date", http.StatusBadRequest)
		return
	}
	d := s.dayData(r, date, true)
	d["DateObj"] = dateObj
	d["Title"] = "PageDay"
	d["Nav"] = "today"
	s.renderLayout(w, r, "today", d, http.StatusOK)
}

func (s *Server) dayData(r *http.Request, date string, full bool) map[string]any {
	ctx := r.Context()
	tsvc := timeline.NewService(s.db)
	sum, err := tsvc.Day(ctx, date)
	if err != nil {
		slog.Error("timeline day failed", "error", err)
	}
	if sum == nil {
		sum = &timeline.DaySummary{}
	}

	br := booking.NewRepository(s.db)
	status, _ := br.GetDay(ctx, date)

	state := activity.ActivityUnknown
	if s.provider != nil {
		state, _ = s.provider.CurrentState(ctx)
	}

	blocks, _ := s.blocksForDay(ctx, date)

	// The current running block and the "now" marker only make sense for today;
	// for past or future days they would otherwise leak the current activity
	// onto a foreign day's timeline.
	today := s.clk.Now().In(s.location()).Format("2006-01-02")
	var running *time.Time
	var runningMinutes int
	if date == today && state == activity.ActivityActive {
		running, runningMinutes = s.currentBlock(ctx, date)
	}

	dateObj, _ := time.Parse("2006-01-02", date)

	data := map[string]any{
		"Date":           date,
		"DateObj":        dateObj,
		"WorkedMinutes":  sum.WorkedMinutes,
		"PauseMinutes":   sum.PauseMinutes,
		"Booked":         status.Booked,
		"Blocks":         blocks,
		"ActivityStatus": string(state),
	}
	s.addTodayViewData(ctx, data, date, sum, blocks, running, runningMinutes, status.Booked, full)
	return data
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
	ID              int
	Date            string
	Start           time.Time
	End             time.Time
	StartStr        string
	EndStr          string
	Source          string
	Status          string
	Note            string
	DurationMinutes int
}

type timelineBlockView struct {
	Label string
	Class string
	Left  string
	Width string
}

type weekChartDayView struct {
	Date          string
	Label         string
	WorkedMinutes int
	TopLabel      string
	BottomLabel   string
	Booked        bool
	IsToday       bool
	IsFuture      bool
	HeightPercent int
}

type settingsWeekdayOption struct {
	Value    string
	Label    string
	Selected bool
}

func (s *Server) blocksForDay(ctx context.Context, date string) ([]blockView, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, work_date, started_at, ended_at, source, status, note
		FROM work_blocks
		WHERE work_date = ? AND status != 'deleted'
		ORDER BY started_at ASC
	`, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var blocks []blockView
	for rows.Next() {
		var b blockView
		var note sql.NullString
		var startStr, endStr string
		if err := rows.Scan(&b.ID, &b.Date, &startStr, &endStr, &b.Source, &b.Status, &note); err != nil {
			return nil, err
		}
		b.Start, _ = time.Parse(time.RFC3339Nano, startStr)
		b.End, _ = time.Parse(time.RFC3339Nano, endStr)
		b.StartStr = startStr
		b.EndStr = endStr
		if d := b.End.Sub(b.Start); d > 0 {
			b.DurationMinutes = int(d.Minutes())
		}
		if note.Valid {
			b.Note = note.String
		}
		blocks = append(blocks, b)
	}
	return blocks, rows.Err()
}

func (s *Server) handleDayBlock(w http.ResponseWriter, r *http.Request) {
	date := chi.URLParam(r, "date")
	start := r.FormValue("start_hour") + ":" + r.FormValue("start_minute")
	end := r.FormValue("end_hour") + ":" + r.FormValue("end_minute")
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

	http.Redirect(w, r, "/day/"+date, http.StatusFound)
}

func (s *Server) handleBlockIgnore(w http.ResponseWriter, r *http.Request) {
	date := chi.URLParam(r, "date")
	idStr := chi.URLParam(r, "id")
	started := r.FormValue("started_at")
	ended := r.FormValue("ended_at")
	s.updateBlockStatus(r.Context(), date, idStr, started, ended, "ignored", "active")
	http.Redirect(w, r, "/day/"+date, http.StatusFound)
}

func (s *Server) handleBlockUnignore(w http.ResponseWriter, r *http.Request) {
	date := chi.URLParam(r, "date")
	idStr := chi.URLParam(r, "id")
	started := r.FormValue("started_at")
	ended := r.FormValue("ended_at")
	s.updateBlockStatus(r.Context(), date, idStr, started, ended, "active", "ignored")
	http.Redirect(w, r, "/day/"+date, http.StatusFound)
}

func (s *Server) handleBlockDelete(w http.ResponseWriter, r *http.Request) {
	date := chi.URLParam(r, "date")
	idStr := chi.URLParam(r, "id")
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(r.Context(), `UPDATE work_blocks SET status = 'deleted', updated_at = ? WHERE id = ? AND source = 'manual'`, now, idStr)
	if err != nil {
		slog.Error("delete block failed", "error", err)
		http.Error(w, "delete block failed", http.StatusInternalServerError)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		started := r.FormValue("started_at")
		ended := r.FormValue("ended_at")
		_, err = s.db.ExecContext(r.Context(), `UPDATE work_blocks SET status = 'deleted', updated_at = ? WHERE work_date = ? AND source = 'manual' AND status = 'active' AND started_at = ? AND ended_at = ?`, now, date, started, ended)
		if err != nil {
			slog.Error("delete block fallback failed", "error", err)
			http.Error(w, "delete block failed", http.StatusInternalServerError)
			return
		}
	}
	http.Redirect(w, r, "/day/"+date, http.StatusFound)
}

func (s *Server) updateBlockStatus(ctx context.Context, date, idStr, started, ended, newStatus, fallbackCurrentStatus string) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `UPDATE work_blocks SET status = ?, updated_at = ? WHERE id = ?`, newStatus, now, idStr)
	if err != nil {
		slog.Error("update block status failed", "error", err, "id", idStr, "status", newStatus)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		_, err = s.db.ExecContext(ctx, `UPDATE work_blocks SET status = ?, updated_at = ? WHERE work_date = ? AND status = ? AND started_at = ? AND ended_at = ?`, newStatus, now, date, fallbackCurrentStatus, started, ended)
		if err != nil {
			slog.Error("fallback update block status failed", "error", err, "date", date, "status", newStatus)
		}
	}
}

func (s *Server) handleWeek(w http.ResponseWriter, r *http.Request) {
	now := s.clk.Now().In(s.location())
	start, end := weekBounds(now)

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

// dailyTargetMinutes is the assumed daily working-time target (8 hours).
const dailyTargetMinutes = 8 * 60

// addTodayViewData enriches data with the rendered day view. The week chart and
// booking aside require extra DB queries and are only built when full is set;
// the htmx status poll (today_status) does not render them.
func (s *Server) addTodayViewData(ctx context.Context, data map[string]any, date string, sum *timeline.DaySummary, blocks []blockView, running *time.Time, runningMinutes int, booked, full bool) {
	loc := s.location()
	now := s.clk.Now().In(loc)
	dateObj, _ := time.ParseInLocation("2006-01-02", date, loc)
	if dateObj.IsZero() {
		dateObj = now
	}

	isToday := sameDate(dateObj, now)

	activeBlocks := 0
	runningBlockRendered := false
	var timelineBlocks []timelineBlockView
	for _, b := range blocks {
		if b.Status != "active" {
			continue
		}
		activeBlocks++
		isRunning := running != nil && sameInstant(b.Start, *running)
		if isRunning {
			runningBlockRendered = true
		}
		// The timeline view-model is only consumed by the full day page; the
		// htmx status poll (full=false) just needs the block counts above.
		if !full {
			continue
		}
		end := b.End.In(loc)
		duration := b.DurationMinutes
		class := "auto"
		if b.Source == "manual" {
			class = "manual"
		}
		if isRunning {
			end = now
			duration = runningMinutes
			class = "run"
		}
		timelineBlocks = append(timelineBlocks, timelineBlockView{
			Label: timeutil.FormatMinutes(duration),
			Class: class,
			Left:  percentOfDay(b.Start.In(loc)),
			Width: widthOfDay(b.Start.In(loc), end),
		})
	}
	extraRunningBlock := running != nil && !runningBlockRendered
	if full && extraRunningBlock {
		timelineBlocks = append(timelineBlocks, timelineBlockView{
			Label: timeutil.FormatMinutes(runningMinutes),
			Class: "run",
			Left:  percentOfDay(running.In(loc)),
			Width: widthOfDay(running.In(loc), now),
		})
	}

	totalBlocks := activeBlocks
	if extraRunningBlock {
		totalBlocks++
	}

	// NowPercent drives the "now" marker; only render it on today's timeline.
	nowPercent := ""
	if isToday {
		nowPercent = percentOfDay(now)
	}

	data["IsToday"] = isToday
	data["TodayHeadline"] = fmt.Sprintf("%s, %s", i18n.WeekdayName(dateObj, s.locale()), i18n.DateShort(dateObj, s.locale()))
	data["TodaySubline"] = s.catalog.Tf(s.locale(), "ISOWeekShort", map[string]any{"Week": fmt.Sprintf("%02d", isoWeek(dateObj))})
	data["NowPercent"] = nowPercent
	data["TimelineBlocks"] = timelineBlocks
	data["ActiveBlockCount"] = totalBlocks
	data["DayProgressPercent"] = progressPercent(sum.WorkedMinutes, dailyTargetMinutes)
	data["DayTargetMinutes"] = dailyTargetMinutes
	data["DayRemainingMinutes"] = max(0, dailyTargetMinutes-sum.WorkedMinutes)

	if !full {
		return
	}

	weekDays, weekTotal, weekBooked, weekUnbooked := s.weekChart(ctx, now)
	data["WeekChartDays"] = weekDays
	data["WeekTotalMinutes"] = weekTotal
	data["WeekBookedDays"] = weekBooked
	data["WeekUnbookedDays"] = weekUnbooked
	data["BookingAside"] = s.bookingAside(ctx, dateObj, booked)
}

func (s *Server) weekChart(ctx context.Context, now time.Time) ([]weekChartDayView, int, int, int) {
	start, end := weekBounds(now)
	days, err := timeline.NewService(s.db).Range(ctx, start.Format("2006-01-02"), end.Format("2006-01-02"))
	byDate := make(map[string]*timeline.DaySummary, len(days))
	maxMinutes := 1
	if err == nil {
		for _, d := range days {
			byDate[d.Date] = d
		}
	}

	configuredWeekdays := make(map[time.Weekday]bool)
	for _, weekday := range s.config().TodayWeekdays() {
		configuredWeekdays[weekday] = true
	}
	for i := 0; i < 7; i++ {
		day := start.AddDate(0, 0, i)
		if !configuredWeekdays[day.Weekday()] {
			continue
		}
		if summary := byDate[day.Format("2006-01-02")]; summary != nil && summary.WorkedMinutes > maxMinutes {
			maxMinutes = summary.WorkedMinutes
		}
	}

	var result []weekChartDayView
	total, booked, unbooked := 0, 0, 0
	for i := 0; i < 7; i++ {
		day := start.AddDate(0, 0, i)
		if !configuredWeekdays[day.Weekday()] {
			continue
		}
		date := day.Format("2006-01-02")
		summary := byDate[date]
		worked := 0
		isBooked := false
		if summary != nil {
			worked = summary.WorkedMinutes
			isBooked = summary.Booked
		}
		total += worked
		if isBooked {
			booked++
		} else if worked > 0 && !day.After(now) {
			// Only days with tracked activity can be "not booked yet";
			// empty weekends/holidays must not inflate the warning count.
			unbooked++
		}
		height := 6
		if worked > 0 {
			height = max(12, (worked*100)/maxMinutes)
		}
		result = append(result, weekChartDayView{
			Date:          date,
			Label:         fmt.Sprintf("%s %d", i18n.WeekdayShort(day, s.locale()), day.Day()),
			WorkedMinutes: worked,
			TopLabel:      weekChartTopLabel(worked),
			BottomLabel:   weekChartBottomLabel(worked),
			Booked:        isBooked,
			IsToday:       sameDate(day, now),
			IsFuture:      day.After(now),
			HeightPercent: min(height, 100),
		})
	}
	return result, total, booked, unbooked
}

// weekChartTopLabel is the primary line shown inside a week-chart bar: the hour
// count for full hours, the minute count for sub-hour durations, or a dash when
// nothing was tracked.
func weekChartTopLabel(minutes int) string {
	if minutes <= 0 {
		return "-"
	}
	if minutes < 60 {
		return fmt.Sprintf("%dm", minutes)
	}
	return fmt.Sprintf("%dh", minutes/60)
}

// weekChartBottomLabel is the secondary minute line, only shown once at least
// one full hour was tracked (e.g. "30m" below "1h"). Sub-hour durations carry
// their value in the top label instead.
func weekChartBottomLabel(minutes int) string {
	if minutes < 60 {
		return ""
	}
	return fmt.Sprintf("%02dm", minutes%60)
}

func (s *Server) bookingAside(ctx context.Context, date time.Time, booked bool) map[string]any {
	br := booking.NewRepository(s.db)
	bookingDay, err := br.GetBookingDay(ctx)
	if err != nil || bookingDay == nil {
		return map[string]any{
			"Configured": false,
			"Booked":     booked,
		}
	}
	now := s.clk.Now().In(s.location())
	periodStart, err := closure.NewRepository(s.db).PeriodStart(ctx, *bookingDay)
	if err != nil {
		periodStart = date
	}
	days, _ := timeline.NewService(s.db).Range(ctx, periodStart.Format("2006-01-02"), bookingDay.Format("2006-01-02"))
	openDays := 0
	for _, d := range days {
		if !d.Booked {
			openDays++
		}
	}
	return map[string]any{
		"Configured":    true,
		"PeriodStart":   periodStart,
		"PeriodEnd":     *bookingDay,
		"BookingDay":    *bookingDay,
		"DaysRemaining": int(bookingDay.Sub(time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())).Hours() / 24),
		"OpenDays":      openDays,
		"Booked":        booked,
	}
}

func percentOfDay(t time.Time) string {
	minutes := t.Hour()*60 + t.Minute()
	return fmt.Sprintf("%.2f%%", float64(minutes)*100/1440)
}

func widthOfDay(start, end time.Time) string {
	minutes := int(end.Sub(start).Minutes())
	if minutes < 1 {
		minutes = 1
	}
	return fmt.Sprintf("%.2f%%", float64(minutes)*100/1440)
}

func progressPercent(value, target int) int {
	if target <= 0 {
		return 0
	}
	return min(100, max(0, (value*100)/target))
}

func isoWeek(t time.Time) int {
	_, week := t.ISOWeek()
	return week
}

// weekBounds returns the Monday and Sunday (both at midnight, in now's
// location) of the ISO week containing now.
func weekBounds(now time.Time) (start, end time.Time) {
	wd := int(now.Weekday())
	if wd == 0 {
		wd = 7
	}
	start = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -(wd - 1))
	end = start.AddDate(0, 0, 6)
	return start, end
}

func sameDate(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func sameInstant(a, b time.Time) bool {
	delta := a.Sub(b)
	if delta < 0 {
		delta = -delta
	}
	return delta < time.Second
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
		Empty         bool
		Date          string
		DayNum        int
		WorkedMinutes int
		HasWork       bool
		BarWidth      int
		Booked        bool
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
		if sum != nil {
			worked = sum.WorkedMinutes
			booked = sum.Booked
		}
		cells = append(cells, cell{
			Empty:         false,
			Date:          dateStr,
			DayNum:        d.Day(),
			WorkedMinutes: worked,
			HasWork:       worked > 0,
			BarWidth:      (worked * 100) / maxWork,
			Booked:        booked,
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
			Date:           d.Date,
			Booked:         d.Booked,
			TrackedMinutes: d.WorkedMinutes,
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
			Date:           d.Date,
			Booked:         d.Booked,
			TrackedMinutes: d.WorkedMinutes,
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
	s.renderSettings(w, r, s.config(), "", http.StatusOK)
}

// renderSettings renders the settings form for the given config. When flash is
// non-empty it is shown as an error banner and the form keeps the submitted
// values, so a validation failure on save does not discard the user's edits.
func (s *Server) renderSettings(w http.ResponseWriter, r *http.Request, cfg config.Config, flash string, status int) {
	data := map[string]any{
		"Title":      "PageSettings",
		"Nav":        "settings",
		"Config":     cfg,
		"ConfigPath": s.paths.ConfigFile,
		"Timezones":  []string{"UTC", "Europe/Berlin", "Europe/London", "America/New_York", "America/Los_Angeles", "Asia/Tokyo"},
		"Languages":  []i18n.Locale{i18n.German, i18n.English},
		"Weekdays":   s.settingsWeekdayOptions(cfg),
		"Version":    s.version,
	}
	if flash != "" {
		data["Flash"] = flash
		data["FlashType"] = "error"
	}
	s.renderLayout(w, r, "settings", data, status)
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
	if weekdays, ok := r.Form["today_weekdays"]; ok {
		cfg.App.TodayWeekdays = weekdays
	} else {
		cfg.App.TodayWeekdays = []string{}
	}

	if err := cfg.Validate(); err != nil {
		s.renderSettings(w, r, cfg, err.Error(), http.StatusBadRequest)
		return
	}

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

	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) settingsWeekdayOptions(cfg config.Config) []settingsWeekdayOption {
	selected := make(map[time.Weekday]bool)
	for _, weekday := range cfg.TodayWeekdays() {
		selected[weekday] = true
	}
	options := make([]settingsWeekdayOption, 0, len(config.WeekdayOrder))
	for _, wd := range config.WeekdayOrder {
		options = append(options, settingsWeekdayOption{
			Value:    wd.Token,
			Label:    i18n.WeekdayNameFor(wd.Weekday, s.locale()),
			Selected: selected[wd.Weekday],
		})
	}
	return options
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
