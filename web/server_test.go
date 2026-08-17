package web

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/smeir/zeitspur/internal/activity"
	"github.com/smeir/zeitspur/internal/clock"
	"github.com/smeir/zeitspur/internal/config"
	"github.com/smeir/zeitspur/internal/copilot"
	"github.com/smeir/zeitspur/internal/database"
	"github.com/smeir/zeitspur/internal/i18n"
)

func newTestServer(t *testing.T) (*Server, *sql.DB) {
	t.Helper()
	db, err := database.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	cfg := config.Config{}
	cfg.Server.ListenAddress = "127.0.0.1:8787"
	cfg.App.Timezone = "local"
	paths := config.Paths{ConfigFile: t.TempDir() + "/config.toml"}
	provider := activity.NewMockProvider(activity.ActivityActive)
	srv, err := NewServer(db, cfg, paths, provider, clock.System{}, "test")
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return srv, db
}

func TestServer_CSRFRejectsStateChange(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/day/2026-06-13/block", strings.NewReader("start_hour=09&start_minute=00&end_hour=10&end_minute=00"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestServer_DayBookingRouteRemoved(t *testing.T) {
	srv, _ := newTestServer(t)
	token := fetchCSRToken(t, srv)
	body := "csrf_token=" + token + "&booked=true"
	req := httptest.NewRequest(http.MethodPost, "/day/2026-06-13/book", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: token})
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func fetchCSRToken(t *testing.T, srv *Server) string {
	t.Helper()
	getReq := httptest.NewRequest(http.MethodGet, "/day/2026-06-13", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, getReq)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	for _, c := range rr.Result().Cookies() {
		if c.Name == "csrf_token" {
			return c.Value
		}
	}
	t.Fatal("no csrf token cookie")
	return ""
}

func TestServer_AddManualBlock(t *testing.T) {
	srv, db := newTestServer(t)
	token := fetchCSRToken(t, srv)

	body := "csrf_token=" + token + "&start_hour=09&start_minute=30&end_hour=12&end_minute=00&note=test"
	req := httptest.NewRequest(http.MethodPost, "/day/2026-06-13/block", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: token})
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d", rr.Code)
	}

	var count int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM work_blocks WHERE work_date = ? AND source = 'manual'`, "2026-06-13").Scan(&count); err != nil {
		t.Fatalf("count manual blocks: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 manual block, got %d", count)
	}
}

func TestServer_BlockIgnoreAndUnignore(t *testing.T) {
	srv, db := newTestServer(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO work_blocks (work_date, started_at, ended_at, source, status, created_at, updated_at)
		VALUES ('2026-06-13', '2026-06-13T09:00:00Z', '2026-06-13T10:00:00Z', 'detected', 'active', '2026-06-13T09:00:00Z', '2026-06-13T09:00:00Z')
	`); err != nil {
		t.Fatalf("insert block: %v", err)
	}

	token := fetchCSRToken(t, srv)

	ignore := func(path string) {
		body := "csrf_token=" + token
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: "csrf_token", Value: token})
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusFound {
			t.Fatalf("expected redirect from %s, got %d", path, rr.Code)
		}
	}

	ignore("/day/2026-06-13/block/1/ignore")

	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM work_blocks WHERE id = 1`).Scan(&status); err != nil {
		t.Fatalf("query status: %v", err)
	}
	if status != "ignored" {
		t.Errorf("expected ignored, got %s", status)
	}

	ignore("/day/2026-06-13/block/1/unignore")

	if err := db.QueryRowContext(ctx, `SELECT status FROM work_blocks WHERE id = 1`).Scan(&status); err != nil {
		t.Fatalf("query status: %v", err)
	}
	if status != "active" {
		t.Errorf("expected active, got %s", status)
	}
}

func TestServer_BlockDeleteManual(t *testing.T) {
	srv, db := newTestServer(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO work_blocks (work_date, started_at, ended_at, source, status, created_at, updated_at)
		VALUES ('2026-06-13', '2026-06-13T14:00:00Z', '2026-06-13T15:00:00Z', 'manual', 'active', '2026-06-13T09:00:00Z', '2026-06-13T09:00:00Z')
	`); err != nil {
		t.Fatalf("insert block: %v", err)
	}

	token := fetchCSRToken(t, srv)
	body := "csrf_token=" + token
	req := httptest.NewRequest(http.MethodPost, "/day/2026-06-13/block/1/delete", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: token})
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d", rr.Code)
	}

	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM work_blocks WHERE id = 1`).Scan(&status); err != nil {
		t.Fatalf("query status: %v", err)
	}
	if status != "deleted" {
		t.Errorf("expected deleted, got %s", status)
	}
}

func TestServer_BlockDeleteDatabaseErrorReturns500(t *testing.T) {
	srv, db := newTestServer(t)
	token := fetchCSRToken(t, srv)
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	body := "csrf_token=" + token
	req := httptest.NewRequest(http.MethodPost, "/day/2026-06-13/block/1/delete", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: token})
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
}

func TestServer_BlockIgnoreFallbackByTime(t *testing.T) {
	srv, db := newTestServer(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO work_blocks (work_date, started_at, ended_at, source, status, created_at, updated_at)
		VALUES ('2026-06-13', '2026-06-13T09:00:00Z', '2026-06-13T10:00:00Z', 'detected', 'active', '2026-06-13T09:00:00Z', '2026-06-13T09:00:00Z')
	`); err != nil {
		t.Fatalf("insert block: %v", err)
	}

	token := fetchCSRToken(t, srv)
	body := "csrf_token=" + token + "&started_at=2026-06-13T09:00:00Z&ended_at=2026-06-13T10:00:00Z"
	req := httptest.NewRequest(http.MethodPost, "/day/2026-06-13/block/999/ignore", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: token})
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d", rr.Code)
	}

	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM work_blocks WHERE id = 1`).Scan(&status); err != nil {
		t.Fatalf("query status: %v", err)
	}
	if status != "ignored" {
		t.Errorf("expected ignored via fallback, got %s", status)
	}
}

func TestServer_StaticAssets(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/static/htmx.min.js", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestServer_SettingsRendersCopilotFields(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/settings", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{
		`data-settings-tab="copilot"`,
		`name="copilot_enabled"`,
		`name="copilot_fetch_interval"`,
		`name="copilot_gh_path"`,
		`name="copilot_daily_limit"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("settings page missing %q", want)
		}
	}
}

func TestServer_SettingsSavePersistsCopilotFields(t *testing.T) {
	srv, _ := newTestServer(t)
	token := fetchCSRToken(t, srv)

	form := url.Values{}
	form.Set("csrf_token", token)
	form.Set("active_tab", "copilot")
	form.Set("activity_mode", "idle_and_lock")
	form.Set("poll_interval", "30s")
	form.Set("idle_threshold", "5m")
	form.Set("listen_address", "127.0.0.1:8787")
	form.Set("timezone", "local")
	form.Set("language", "de")
	form.Set("today_weekdays", "mon")
	form.Set("copilot_enabled", "true")
	form.Set("copilot_fetch_interval", "1h")
	form.Set("copilot_gh_path", "gh")
	form.Set("copilot_daily_limit", "1800")

	req := httptest.NewRequest(http.MethodPost, "/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: token})
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d", rr.Code)
	}

	cfg, err := config.Load(srv.paths.ConfigFile)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if !cfg.CopilotEnabled() {
		t.Errorf("copilot enabled not persisted")
	}
	if cfg.Copilot.FetchInterval != "1h" {
		t.Errorf("fetch_interval = %q", cfg.Copilot.FetchInterval)
	}
	if cfg.CopilotDailyLimit() != 1800 {
		t.Errorf("daily_limit = %d, want 1800", cfg.CopilotDailyLimit())
	}
}

func TestServer_SettingsSaveRejectsInvalidDailyLimit(t *testing.T) {
	srv, _ := newTestServer(t)
	token := fetchCSRToken(t, srv)

	form := url.Values{}
	form.Set("csrf_token", token)
	form.Set("active_tab", "copilot")
	form.Set("activity_mode", "idle_and_lock")
	form.Set("poll_interval", "30s")
	form.Set("idle_threshold", "5m")
	form.Set("listen_address", "127.0.0.1:8787")
	form.Set("timezone", "local")
	form.Set("language", "de")
	form.Set("today_weekdays", "mon")
	form.Set("copilot_enabled", "true")
	form.Set("copilot_fetch_interval", "1h")
	form.Set("copilot_gh_path", "gh")
	form.Set("copilot_daily_limit", "abc") // non-numeric

	req := httptest.NewRequest(http.MethodPost, "/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: token})
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-numeric daily_limit, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "nicht-negative Zahl") {
		t.Errorf("expected localized error message in body, got: %s", body)
	}

	// The config file must not have been rewritten with a silent default.
	if _, err := os.Stat(srv.paths.ConfigFile); !os.IsNotExist(err) {
		cfg, _ := config.Load(srv.paths.ConfigFile)
		if cfg.CopilotDailyLimit() == config.DefaultCopilotDailyLimit {
			t.Errorf("daily_limit silently reset to default %d", config.DefaultCopilotDailyLimit)
		}
	}
}

func TestServer_CopilotPageRendersStatusCard(t *testing.T) {
	srv, db := newTestServer(t)
	ctx := context.Background()

	// Empty state: renders the no-data placeholder, not a broken template.
	req := httptest.NewRequest(http.MethodGet, "/copilot", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for empty state, got %d", rr.Code)
	}

	// Populate one successful snapshot.
	repo := copilot.NewRepository(db)
	snap := &copilot.Snapshot{
		OK:                 true,
		Plan:               "test-plan",
		EntitlementCredits: 5000,
		RemainingCredits:   4000,
		UsedCredits:        1000,
		PercentRemaining:   80,
		WarningLevel:       copilot.WarningNotice,
	}
	if err := repo.Store(ctx, snap); err != nil {
		t.Fatalf("store snapshot: %v", err)
	}

	req = httptest.NewRequest(http.MethodGet, "/copilot", nil)
	rr = httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	// The dashboard hero shows the used% ring, the entitlement, and the used
	// credits. UsedCredits=1000 renders as "1000.0"; entitlement 5000 as "5000".
	for _, want := range []string{"cp-ring-used", "5000", "1000.0"} {
		if !strings.Contains(body, want) {
			t.Errorf("copilot page missing %q", want)
		}
	}
	// The old verbose detail table (e.g. "Letzter Abruf") is gone; the hero now
	// uses a compact "aktualisiert vor … min" subtitle.
	for _, gone := range []string{"Letzter Abruf"} {
		if strings.Contains(body, gone) {
			t.Errorf("copilot page still contains trimmed label %q", gone)
		}
	}
}

func TestServer_CopilotPageHidesQuotaOnFailedFetch(t *testing.T) {
	srv, db := newTestServer(t)
	ctx := context.Background()

	// A failed snapshot: OK=false, no quota numbers.
	repo := copilot.NewRepository(db)
	if err := repo.Store(ctx, &copilot.Snapshot{
		OK:           false,
		ErrorMessage: "boom",
	}); err != nil {
		t.Fatalf("store snapshot: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/copilot", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()

	// The error box must be shown.
	if !strings.Contains(body, "copilotFetchError") && !strings.Contains(body, "boom") {
		t.Errorf("expected error message in body")
	}
	// The quota ring is hidden on failure (it lives inside the hero, which only
	// renders for a successful snapshot).
	for _, gone := range []string{"cp-ring-used"} {
		if strings.Contains(body, gone) {
			t.Errorf("copilot page shows quota on failed fetch: %q", gone)
		}
	}
}

// TestServer_CopilotPageClassifiesFetchError asserts that a transient upstream
// failure (GitHub outage) renders as a neutral info box with its own wording,
// while a local problem (missing authentication) stays a warning that names the
// required action.
func TestServer_CopilotPageClassifiesFetchError(t *testing.T) {
	tests := []struct {
		name         string
		snap         *copilot.Snapshot
		want         string
		wantInfoBox  bool
		unwantedText string
	}{
		{
			name: "github outage",
			snap: &copilot.Snapshot{
				OK:           false,
				ErrorKind:    copilot.ErrorKindUnavailable,
				ErrorMessage: "copilot fetch: GitHub is temporarily unavailable (HTTP 503)",
			},
			want:         "vor\u00fcbergehend nicht erreichbar",
			wantInfoBox:  true,
			unwantedText: "gh auth login",
		},
		{
			name: "not authenticated",
			snap: &copilot.Snapshot{
				OK:           false,
				ErrorKind:    copilot.ErrorKindAuth,
				ErrorMessage: "copilot fetch: GitHub CLI is not authenticated (HTTP 401)",
			},
			want:        "gh auth login",
			wantInfoBox: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, db := newTestServer(t)
			if err := copilot.NewRepository(db).Store(context.Background(), tc.snap); err != nil {
				t.Fatalf("store snapshot: %v", err)
			}

			req := httptest.NewRequest(http.MethodGet, "/copilot", nil)
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", rr.Code)
			}
			body := rr.Body.String()
			if !strings.Contains(body, tc.want) {
				t.Errorf("copilot page missing localized hint %q", tc.want)
			}
			if tc.unwantedText != "" && strings.Contains(body, tc.unwantedText) {
				t.Errorf("copilot page should not contain %q for a transient failure", tc.unwantedText)
			}
			if got := strings.Contains(body, "info-box"); got != tc.wantInfoBox {
				t.Errorf("info-box present = %v, want %v", got, tc.wantInfoBox)
			}
			// The raw upstream detail stays visible for debugging.
			if !strings.Contains(body, "HTTP") {
				t.Error("expected the raw error detail to be rendered")
			}
		})
	}
}

func TestServer_LocalizedDayView(t *testing.T) {
	srv, db := newTestServer(t)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `
		INSERT INTO work_blocks (work_date, started_at, ended_at, source, status, created_at, updated_at)
		VALUES ('2026-06-13', '2026-06-13T09:00:00Z', '2026-06-13T10:00:00Z', 'detected', 'active', '2026-06-13T09:00:00Z', '2026-06-13T09:00:00Z')
	`)
	if err != nil {
		t.Fatalf("insert detected block: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/day/2026-06-13", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Heute") {
		t.Errorf("expected German 'Heute' in default German day view")
	}
	if !strings.Contains(body, "Automatisch") {
		t.Errorf("expected German 'Automatisch' for detected source, got body:\n%s", body)
	}
}

func TestServer_EnglishDayView(t *testing.T) {
	cfg := config.Config{}
	cfg.Server.ListenAddress = "127.0.0.1:8787"
	cfg.App.Timezone = "local"
	cfg.App.Language = "en"
	paths := config.Paths{ConfigFile: t.TempDir() + "/config.toml"}

	db, err := database.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	provider := activity.NewMockProvider(activity.ActivityActive)
	srv, err := NewServer(db, cfg, paths, provider, clock.System{}, "test")
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/day/2026-06-13", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Today") {
		t.Errorf("expected English 'Today' in English day view")
	}
	if strings.Contains(body, "Heute") {
		t.Errorf("did not expect German 'Heute' in English day view")
	}
	if strings.Contains(body, "KW ") {
		t.Errorf("did not expect German ISO week label in English day view")
	}
	if !strings.Contains(body, "Week 24") {
		t.Errorf("expected English ISO week label, got body:\n%s", body)
	}
}

func TestServer_TodayWeekChartUsesConfiguredWeekdays(t *testing.T) {
	db, err := database.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO work_blocks (work_date, started_at, ended_at, source, status, created_at, updated_at)
		VALUES
		('2026-06-15', '2026-06-15T09:00:00Z', '2026-06-15T10:00:00Z', 'detected', 'active', '2026-06-15T09:00:00Z', '2026-06-15T09:00:00Z'),
		('2026-06-16', '2026-06-16T09:00:00Z', '2026-06-16T11:00:00Z', 'detected', 'active', '2026-06-16T09:00:00Z', '2026-06-16T09:00:00Z')
	`); err != nil {
		t.Fatalf("insert blocks: %v", err)
	}

	cfg := config.Config{}
	cfg.Server.ListenAddress = "127.0.0.1:8787"
	cfg.App.Timezone = "UTC"
	cfg.App.TodayWeekdays = []string{"mon", "wed", "fri"}
	paths := config.Paths{ConfigFile: t.TempDir() + "/config.toml"}
	provider := activity.NewMockProvider(activity.ActivityActive)
	srv, err := NewServer(db, cfg, paths, provider, clock.NewFixed(time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)), "test")
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	days, total, _, unbooked := srv.weekChart(ctx, time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC))
	if len(days) != 3 {
		t.Fatalf("expected 3 configured weekdays, got %d", len(days))
	}
	if days[0].Date != "2026-06-15" || days[1].Date != "2026-06-17" || days[2].Date != "2026-06-19" {
		t.Fatalf("expected mon/wed/fri dates, got %+v", days)
	}
	if total != 60 {
		t.Fatalf("expected total to include only configured weekdays, got %d", total)
	}
	if unbooked != 1 {
		t.Fatalf("expected unbooked count to include only configured weekdays, got %d", unbooked)
	}
}

func TestServer_ClosurePagesFormatDurations(t *testing.T) {
	srv, db := newTestServer(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO booking_closures
			(period_start, period_end, booking_day, closed_at, tracked_workday_count, booked_workday_count, unbooked_workday_count, tracked_minutes_snapshot)
		VALUES
			('2026-06-12', '2026-06-13', '2026-06-13', '2026-06-13T17:00:00Z', 2, 1, 1, 150)
	`); err != nil {
		t.Fatalf("insert closure: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO booking_closure_days
			(closure_id, work_date, booked_snapshot, tracked_minutes_snapshot, day_revision_snapshot)
		VALUES
			(1, '2026-06-12', 1, 90, 1),
			(1, '2026-06-13', 0, 60, 1)
	`); err != nil {
		t.Fatalf("insert closure days: %v", err)
	}

	assertClosureDuration := func(path string, want ...string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 for %s, got %d", path, rr.Code)
		}
		body := rr.Body.String()
		for _, value := range want {
			if !strings.Contains(body, value) {
				t.Errorf("expected %s to contain %q, got body:\n%s", path, value, body)
			}
		}
		if strings.Contains(body, ">150<") || strings.Contains(body, ">90<") || strings.Contains(body, ">60<") {
			t.Errorf("expected %s to avoid raw minute values, got body:\n%s", path, body)
		}
	}

	assertClosureDuration("/closures", "2h 30m")
	assertClosureDuration("/closures/1", "2h 30m", "1h 30m", "1h 0m")
}

func TestServer_DayViewReflectsInactiveActivityStatus(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.provider.(*activity.MockProvider).SetState(activity.ActivityIdle)

	req := httptest.NewRequest(http.MethodGet, "/day/2026-06-13", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `class="live live-idle"`) {
		t.Errorf("expected header to render idle status, got body:\n%s", body)
	}
	if strings.Contains(body, "System aktiv") {
		t.Errorf("did not expect fixed active label in idle day view")
	}
}

// TestServer_WeekNavigation verifies that /week honors the ?date= query
// parameter and renders previous/next navigation links. When the displayed
// week is the ongoing one, the jump-to-current-week button is hidden.
func TestServer_WeekNavigation(t *testing.T) {
	db, err := database.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	cfg := config.Config{}
	cfg.Server.ListenAddress = "127.0.0.1:8787"
	cfg.App.Timezone = "UTC"
	paths := config.Paths{ConfigFile: t.TempDir() + "/config.toml"}
	provider := activity.NewMockProvider(activity.ActivityActive)
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC) // Wednesday, ISO week 25
	srv, err := NewServer(db, cfg, paths, provider, clock.NewFixed(now), "test")
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	// Current week: no jump-to-current button, but prev/next links present.
	req := httptest.NewRequest(http.MethodGet, "/week", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "KW 25") {
		t.Errorf("expected ISO week 25 label, got body:\n%s", body)
	}
	if !strings.Contains(body, "/week?date=2026-06-08") {
		t.Errorf("expected previous-week link (2026-06-08), got body:\n%s", body)
	}
	if !strings.Contains(body, "/week?date=2026-06-22") {
		t.Errorf("expected next-week link (2026-06-22), got body:\n%s", body)
	}
	if strings.Contains(body, "Aktuelle Woche") {
		t.Errorf("did not expect current-week button on the ongoing week, got body:\n%s", body)
	}

	// Previous week: jump-to-current button should now appear.
	req = httptest.NewRequest(http.MethodGet, "/week?date=2026-06-08", nil)
	rr = httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body = rr.Body.String()
	if !strings.Contains(body, "KW 24") {
		t.Errorf("expected ISO week 24 label, got body:\n%s", body)
	}
	if !strings.Contains(body, "Aktuelle Woche") {
		t.Errorf("expected current-week button on a past week, got body:\n%s", body)
	}
}

// TestServer_WeekNavigationTimezone verifies that the ?date= query parameter
// is parsed in the configured location, so a date is not shifted to the
// previous day in zones west of UTC (regression for time.Parse vs.
// time.ParseInLocation).
func TestServer_WeekNavigationTimezone(t *testing.T) {
	db, err := database.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	cfg := config.Config{}
	cfg.Server.ListenAddress = "127.0.0.1:8787"
	cfg.App.Timezone = "America/New_York" // UTC-5 (EDT), west of UTC
	paths := config.Paths{ConfigFile: t.TempDir() + "/config.toml"}
	provider := activity.NewMockProvider(activity.ActivityActive)
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	srv, err := NewServer(db, cfg, paths, provider, clock.NewFixed(now), "test")
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	// 2026-06-08 is a Monday (ISO week 24). With the buggy time.Parse path it
	// would shift to Sunday 2026-06-07 20:00 EDT and land in ISO week 23.
	req := httptest.NewRequest(http.MethodGet, "/week?date=2026-06-08", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "KW 24") {
		t.Errorf("expected ISO week 24 for 2026-06-08 in America/New_York, got body:\n%s", body)
	}
	if !strings.Contains(body, "/week?date=2026-06-01") {
		t.Errorf("expected previous-week link (2026-06-01), got body:\n%s", body)
	}
}

// TestStatusLabelCoverage ensures every defined ActivityState maps to
// a dedicated label rather than silently falling back to the "Unknown" label.
func TestStatusLabelCoverage(t *testing.T) {
	cat := i18n.NewCatalog()

	states := []activity.ActivityState{
		activity.ActivityActive,
		activity.ActivityIdle,
		activity.ActivityLocked,
		activity.ActivitySuspended,
	}

	for _, locale := range i18n.SupportedLocales() {
		unknown := cat.StatusLabel(locale, "not-a-real-status")
		for _, s := range states {
			if got := cat.StatusLabel(locale, string(s)); got == unknown {
				t.Errorf("ActivityState %q in %q fell back to unknown label %q", s, locale, unknown)
			}
		}
	}
}
