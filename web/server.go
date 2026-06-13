// Package web serves the local HTML UI.
package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/hex"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/smeir/zeitspur/internal/activity"
	"github.com/smeir/zeitspur/internal/clock"
	"github.com/smeir/zeitspur/internal/config"
)

//go:embed templates/*.html static/*
var assets embed.FS

// Server is the HTTP frontend.
type Server struct {
	mu        sync.RWMutex
	db        *sql.DB
	cfg       config.Config
	paths     config.Paths
	provider  activity.ActivityProvider
	router    chi.Router
	templates map[string]*template.Template
	clk       clock.Clock
	loc       *time.Location
}

// NewServer creates and configures the web server.
func NewServer(db *sql.DB, cfg config.Config, paths config.Paths, provider activity.ActivityProvider, clk clock.Clock) (*Server, error) {
	loc := time.Local
	if cfg.App.Timezone != "" && cfg.App.Timezone != "local" {
		var err error
		loc, err = time.LoadLocation(cfg.App.Timezone)
		if err != nil {
			return nil, fmt.Errorf("load timezone: %w", err)
		}
	}

	templates := make(map[string]*template.Template)
	pageNames := []string{"today", "week", "month", "booking", "closures", "closure_detail", "settings", "close_preview"}
	for _, name := range pageNames {
		tmpl, err := template.New("").ParseFS(assets, "templates/layout.html", "templates/"+name+".html")
		if err != nil {
			return nil, fmt.Errorf("parse template %s: %w", name, err)
		}
		templates[name] = tmpl
	}

	s := &Server{
		db:        db,
		cfg:       cfg,
		paths:     paths,
		provider:  provider,
		templates: templates,
		clk:       clk,
		loc:       loc,
	}

	r := chi.NewRouter()
	r.Use(s.csrfMiddleware)
	r.Get("/static/*", s.handleStatic)

	r.Get("/", s.handleToday)
	r.Get("/today/status", s.handleTodayStatus)
	r.Get("/day/{date}", s.handleDay)
	r.Post("/day/{date}/book", s.handleDayBook)
	r.Post("/day/{date}/block", s.handleDayBlock)
	r.Post("/day/{date}/block/{id}/ignore", s.handleBlockIgnore)

	r.Get("/week", s.handleWeek)
	r.Get("/month", s.handleMonth)

	r.Get("/booking", s.handleBooking)
	r.Post("/booking-day", s.handleBookingDay)
	r.Post("/booking/bulk", s.handleBookingBulk)
	r.Get("/booking/close-preview", s.handleClosePreview)
	r.Post("/booking/close", s.handleClose)

	r.Get("/closures", s.handleClosures)
	r.Get("/closures/{id}", s.handleClosureDetail)

	r.Get("/settings", s.handleSettings)
	r.Post("/settings", s.handleSettingsSave)

	s.router = r
	return s, nil
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

// ListenAddress returns the configured listen address.
func (s *Server) ListenAddress() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.Server.ListenAddress
}

// config returns a snapshot of the current server configuration.
func (s *Server) config() config.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// location returns the currently configured timezone location.
func (s *Server) location() *time.Location {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loc
}

// setConfig updates the server configuration and location atomically.
func (s *Server) setConfig(cfg config.Config, loc *time.Location) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = cfg
	s.loc = loc
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	http.FileServer(http.FS(assets)).ServeHTTP(w, r)
}

// csrfMiddleware sets a CSRF token cookie and makes it available in the request context.
func (s *Server) csrfMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var token string
		cookie, err := r.Cookie("csrf_token")
		if err == nil && cookie.Value != "" {
			token = cookie.Value
		} else {
			b := make([]byte, 16)
			_, _ = rand.Read(b)
			token = hex.EncodeToString(b)
			http.SetCookie(w, &http.Cookie{
				Name:     "csrf_token",
				Value:    token,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
			})
		}
		r = r.WithContext(context.WithValue(r.Context(), ctxCSRF, token))
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if r.FormValue("csrf_token") != token {
				slog.Warn("CSRF token mismatch", "path", r.URL.Path)
				http.Error(w, "invalid csrf token", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

type ctxKey string

const ctxCSRF ctxKey = "csrf_token"

func csrfToken(r *http.Request) string {
	v, _ := r.Context().Value(ctxCSRF).(string)
	return v
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, data map[string]any, status int) {
	data["CSRFToken"] = csrfToken(r)
	tmpl, ok := s.templates[name]
	if !ok {
		slog.Error("unknown template", "name", name)
		http.Error(w, "template not found", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "content", data); err != nil {
		slog.Error("template render failed", "error", err)
		http.Error(w, "template render failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

func (s *Server) renderLayout(w http.ResponseWriter, r *http.Request, name string, data map[string]any, status int) {
	data["CSRFToken"] = csrfToken(r)
	tmpl, ok := s.templates[name]
	if !ok {
		slog.Error("unknown template", "name", name)
		http.Error(w, "template not found", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "layout", data); err != nil {
		slog.Error("layout render failed", "error", err)
		http.Error(w, "layout render failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}
