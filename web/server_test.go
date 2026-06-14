package web

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/smeir/zeitspur/internal/activity"
	"github.com/smeir/zeitspur/internal/clock"
	"github.com/smeir/zeitspur/internal/config"
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
}

// TestStatusLabelCoverage guards against the catalog drifting out of sync with
// the activity package: every ActivityState rendered in the UI must resolve to
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
