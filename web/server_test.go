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
