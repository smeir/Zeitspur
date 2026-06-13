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
	srv, err := NewServer(db, cfg, paths, provider, clock.System{})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return srv, db
}

func TestServer_CSRFRejectsStateChange(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/day/2026-06-13/book", strings.NewReader("booked=true"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestServer_DayBooking(t *testing.T) {
	srv, _ := newTestServer(t)

	// First GET to obtain CSRF cookie.
	getReq := httptest.NewRequest(http.MethodGet, "/day/2026-06-13", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, getReq)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	cookies := rr.Result().Cookies()
	var token string
	for _, c := range cookies {
		if c.Name == "csrf_token" {
			token = c.Value
		}
	}
	if token == "" {
		t.Fatal("no csrf token cookie")
	}

	body := "csrf_token=" + token + "&booked=true"
	req := httptest.NewRequest(http.MethodPost, "/day/2026-06-13/book", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: token})
	rr = httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d", rr.Code)
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
	srv, err := NewServer(db, cfg, paths, provider, clock.System{})
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
