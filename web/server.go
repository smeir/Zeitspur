// Package web serves the local HTML UI.
package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
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
	"github.com/smeir/zeitspur/internal/i18n"
	"github.com/smeir/zeitspur/internal/timeutil"
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
	templates map[i18n.Locale]map[string]*template.Template
	catalog   i18n.Catalog
	clk       clock.Clock
	loc       *time.Location
	version   string
}

// NewServer creates and configures the web server.
func NewServer(db *sql.DB, cfg config.Config, paths config.Paths, provider activity.ActivityProvider, clk clock.Clock, version string) (*Server, error) {
	loc := time.Local
	if cfg.App.Timezone != "" && cfg.App.Timezone != "local" {
		var err error
		loc, err = time.LoadLocation(cfg.App.Timezone)
		if err != nil {
			return nil, fmt.Errorf("load timezone: %w", err)
		}
	}

	catalog := i18n.NewCatalog()
	pageNames := []string{"today", "week", "month", "booking", "closures", "closure_detail", "settings", "close_preview"}
	templates := make(map[i18n.Locale]map[string]*template.Template)
	for _, locale := range i18n.SupportedLocales() {
		templates[locale] = make(map[string]*template.Template)
		funcs := templateFuncs(catalog, locale)
		for _, name := range pageNames {
			files := []string{"templates/layout.html", "templates/" + name + ".html"}
			if name == "today" {
				files = append(files, "templates/today_status.html")
			}
			tmpl, err := template.New("").Funcs(funcs).ParseFS(assets, files...)
			if err != nil {
				return nil, fmt.Errorf("parse template %s/%s: %w", locale, name, err)
			}
			templates[locale][name] = tmpl
		}
	}

	s := &Server{
		db:        db,
		cfg:       cfg,
		paths:     paths,
		provider:  provider,
		templates: templates,
		catalog:   catalog,
		clk:       clk,
		loc:       loc,
		version:   version,
	}

	r := chi.NewRouter()
	r.Use(s.csrfMiddleware)
	r.Get("/static/*", s.handleStatic)

	r.Get("/", s.handleToday)
	r.Get("/today/status", s.handleTodayStatus)
	r.Get("/day/{date}", s.handleDay)
	r.Post("/day/{date}/block", s.handleDayBlock)
	r.Post("/day/{date}/block/{id}/ignore", s.handleBlockIgnore)
	r.Post("/day/{date}/block/{id}/unignore", s.handleBlockUnignore)
	r.Post("/day/{date}/block/{id}/delete", s.handleBlockDelete)

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

// locale returns the configured locale, defaulting to German.
func (s *Server) locale() i18n.Locale {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return i18n.ParseLocale(s.cfg.App.Language)
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
	s.renderBlock(w, r, name, "content", data, status)
}

func (s *Server) renderBlock(w http.ResponseWriter, r *http.Request, name, block string, data map[string]any, status int) {
	locale := s.locale()
	data["CSRFToken"] = csrfToken(r)
	data["Lang"] = string(locale)
	data["Locale"] = locale
	setTranslatedTitles(data, s.catalog, locale)
	page, ok := s.templates[locale][name]
	if !ok {
		slog.Error("unknown template", "name", name, "locale", locale)
		http.Error(w, "template not found", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := page.ExecuteTemplate(&buf, block, data); err != nil {
		slog.Error("template render failed", "error", err)
		http.Error(w, "template render failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

func (s *Server) renderLayout(w http.ResponseWriter, r *http.Request, name string, data map[string]any, status int) {
	locale := s.locale()
	data["CSRFToken"] = csrfToken(r)
	data["Lang"] = string(locale)
	data["Locale"] = locale
	setTranslatedTitles(data, s.catalog, locale)
	page, ok := s.templates[locale][name]
	if !ok {
		slog.Error("unknown template", "name", name, "locale", locale)
		http.Error(w, "template not found", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := page.ExecuteTemplate(&buf, "layout", data); err != nil {
		slog.Error("layout render failed", "error", err)
		http.Error(w, "layout render failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

// setTranslatedTitles converts raw title strings to translated text when the
// template data contains a known title key.
func setTranslatedTitles(data map[string]any, catalog i18n.Catalog, locale i18n.Locale) {
	if title, ok := data["Title"].(string); ok && title != "" {
		data["Title"] = catalog.T(locale, title)
	}
}

// templateFuncs returns the helper functions available inside templates.
func templateFuncs(catalog i18n.Catalog, locale i18n.Locale) template.FuncMap {
	return template.FuncMap{
		"dict": func(values ...any) (map[string]any, error) {
			if len(values)%2 != 0 {
				return nil, errors.New("dict expects an even number of arguments")
			}
			d := make(map[string]any, len(values)/2)
			for i := 0; i < len(values); i += 2 {
				key, ok := values[i].(string)
				if !ok {
					return nil, errors.New("dict keys must be strings")
				}
				d[key] = values[i+1]
			}
			return d, nil
		},
		"T": func(key string) string {
			return catalog.T(locale, key)
		},
		"Tf": func(key string, vars map[string]any) string {
			return catalog.Tf(locale, key, vars)
		},
		"statusLabel": func(status string) string {
			return catalog.StatusLabel(locale, status)
		},
		"sourceLabel": func(source string) string {
			return catalog.SourceLabel(locale, source)
		},
		"dateShort": func(t time.Time) string {
			return i18n.DateShort(t, locale)
		},
		"dateLong": func(t time.Time) string {
			return i18n.DateLong(t, locale)
		},
		"dateTime": func(t time.Time) string {
			return i18n.DateTime(t, locale)
		},
		"monthYear": func(t time.Time) string {
			return i18n.MonthYear(t, locale)
		},
		"weekday": func(t time.Time) string {
			return i18n.WeekdayName(t, locale)
		},
		"month": func(t time.Time) string {
			return i18n.MonthName(t, locale)
		},
		"formatMinutes": func(minutes int) string {
			return timeutil.FormatMinutes(minutes)
		},
		"yesNo": func(v bool) string {
			if v {
				return catalog.T(locale, "Yes")
			}
			return catalog.T(locale, "No")
		},
		"bookedLabel": func(v bool) string {
			if v {
				return catalog.T(locale, "Booked")
			}
			return catalog.T(locale, "NotBooked")
		},
	}
}
