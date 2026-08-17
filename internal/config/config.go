// Package config loads and validates application configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

const (
	AppName = "zeitspur"

	DefaultPollInterval  = 5 * time.Second
	DefaultIdleThreshold = 5 * time.Minute
	DefaultActivityMode  = ActivityModeIdleAndLock
	DefaultListenAddress = "127.0.0.1:8787"
	DefaultTimezone      = "local"
	DefaultLanguage      = "de"
	DefaultNavigation    = "top"
	DefaultTheme         = "teal"

	DefaultCopilotEnabled       = true
	DefaultCopilotFetchInterval = 1 * time.Hour
	DefaultCopilotDailyLimit    = 2500
)

const (
	// ActivityModeIdleAndLock tracks pauses from idle, lock, and suspend signals.
	ActivityModeIdleAndLock = "idle_and_lock"
	// ActivityModeLockOnly tracks pauses from screen lock and suspend signals only.
	ActivityModeLockOnly = "lock_only"
)

var defaultTodayWeekdays = []string{"mon", "tue", "wed", "thu", "fri"}

// Weekday pairs a config token (e.g. "mon") with its time.Weekday.
type Weekday struct {
	Token   string
	Weekday time.Weekday
}

// WeekdayOrder is the single source of truth for the weekday vocabulary, in
// Monday-first display order. It drives parsing, validation, and the settings
// UI so the token list never has to be repeated.
var WeekdayOrder = []Weekday{
	{"mon", time.Monday},
	{"tue", time.Tuesday},
	{"wed", time.Wednesday},
	{"thu", time.Thursday},
	{"fri", time.Friday},
	{"sat", time.Saturday},
	{"sun", time.Sunday},
}

// Config is the runtime configuration for Zeitspur.
type Config struct {
	Activity ActivityConfig `toml:"activity"`
	Server   ServerConfig   `toml:"server"`
	App      AppConfig      `toml:"app"`
	Copilot  CopilotConfig  `toml:"copilot"`
}

// ActivityConfig holds activity-detection settings.
type ActivityConfig struct {
	PollInterval  string `toml:"poll_interval"`
	IdleThreshold string `toml:"idle_threshold"`
	Mode          string `toml:"mode"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	ListenAddress string `toml:"listen_address"`
}

// AppConfig holds general application settings.
type AppConfig struct {
	Timezone      string   `toml:"timezone"`
	Language      string   `toml:"language"`
	Navigation    string   `toml:"navigation"`
	Theme         string   `toml:"theme"`
	TodayWeekdays []string `toml:"today_weekdays"`
}

// NavigationLayout reports the configured primary navigation placement:
// "top" (default) renders the nav as the top bar; "side" renders it as a
// fixed left sidebar.
func (c Config) NavigationLayout() string {
	v := strings.ToLower(strings.TrimSpace(c.App.Navigation))
	if v != "top" && v != "side" {
		return DefaultNavigation
	}
	return v
}

// Theme reports the configured color scheme: "teal" (default) or "aurora"
// (indigo). The value is mirrored onto the <body data-theme="..."> attribute
// and drives CSS variable overrides in style.css.
func (c Config) Theme() string {
	v := strings.ToLower(strings.TrimSpace(c.App.Theme))
	if v != "teal" && v != "aurora" {
		return DefaultTheme
	}
	return v
}

// CopilotConfig holds GitHub Copilot credit tracking settings.
type CopilotConfig struct {
	Enabled       bool   `toml:"enabled"`
	FetchInterval string `toml:"fetch_interval"`
	GHPath        string `toml:"gh_path"`
	DailyLimit    int    `toml:"daily_limit"`
}

// Duration helpers after parsing.
func (c Config) PollInterval() time.Duration {
	return parseDuration(c.Activity.PollInterval, DefaultPollInterval)
}
func (c Config) IdleThreshold() time.Duration {
	return parseDuration(c.Activity.IdleThreshold, DefaultIdleThreshold)
}

// ActivityMode returns the configured pause tracking mode.
func (c Config) ActivityMode() string {
	if c.Activity.Mode == "" {
		return DefaultActivityMode
	}
	return strings.ToLower(strings.TrimSpace(c.Activity.Mode))
}

// Location returns the configured timezone as a *time.Location. The empty
// string and "local" resolve to time.Local. Call after Validate so the
// timezone is guaranteed loadable; a parse failure falls back to time.Local.
func (c Config) Location() *time.Location {
	if c.App.Timezone != "" && c.App.Timezone != "local" {
		if loc, err := time.LoadLocation(c.App.Timezone); err == nil {
			return loc
		}
	}
	return time.Local
}

// CopilotEnabled reports whether the hourly Copilot credit fetcher runs.
func (c Config) CopilotEnabled() bool {
	return c.Copilot.Enabled
}

// CopilotInterval returns the interval between Copilot credit fetches.
func (c Config) CopilotInterval() time.Duration {
	return parseDuration(c.Copilot.FetchInterval, DefaultCopilotFetchInterval)
}

// CopilotGHPath returns the path to the gh binary, defaulting to "gh".
func (c Config) CopilotGHPath() string {
	if p := strings.TrimSpace(c.Copilot.GHPath); p != "" {
		return p
	}
	return "gh"
}

// CopilotDailyLimit returns the per-day credit consumption threshold that
// triggers a desktop notification. A value of 0 disables notifications.
// Validate rejects negative values, so callers that run Validate first can
// treat the result as non-negative.
func (c Config) CopilotDailyLimit() int {
	return c.Copilot.DailyLimit
}

// TodayWeekdays returns the weekdays shown in the Today view week chart.
func (c Config) TodayWeekdays() []time.Weekday {
	values := c.App.TodayWeekdays
	if len(values) == 0 {
		values = defaultTodayWeekdays
	}
	weekdays := make([]time.Weekday, 0, len(values))
	for _, value := range values {
		if wd, ok := parseWeekday(value); ok {
			weekdays = append(weekdays, wd)
		}
	}
	return weekdays
}

func parseDuration(s string, def time.Duration) time.Duration {
	if s == "" {
		return def
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return def
	}
	return d
}

// Paths returns standard XDG paths for the application.
type Paths struct {
	ConfigDir  string
	DataDir    string
	ConfigFile string
	DBFile     string
}

// DefaultPaths returns the default filesystem paths for the current user.
func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("home dir: %w", err)
	}
	configDir := filepath.Join(home, ".config", AppName)
	dataDir := filepath.Join(home, ".local", "share", AppName)
	return Paths{
		ConfigDir:  configDir,
		DataDir:    dataDir,
		ConfigFile: filepath.Join(configDir, "config.toml"),
		DBFile:     filepath.Join(dataDir, AppName+".db"),
	}, nil
}

// Load reads the configuration file, or returns defaults if it does not exist.
func Load(path string) (Config, error) {
	cfg := Config{
		Activity: ActivityConfig{
			PollInterval:  DefaultPollInterval.String(),
			IdleThreshold: DefaultIdleThreshold.String(),
			Mode:          DefaultActivityMode,
		},
		Server: ServerConfig{
			ListenAddress: DefaultListenAddress,
		},
		App: AppConfig{
			Timezone:      DefaultTimezone,
			Language:      DefaultLanguage,
			Navigation:    DefaultNavigation,
			Theme:         DefaultTheme,
			TodayWeekdays: append([]string(nil), defaultTodayWeekdays...),
		},
		Copilot: CopilotConfig{
			Enabled:       DefaultCopilotEnabled,
			FetchInterval: DefaultCopilotFetchInterval.String(),
			GHPath:        "gh",
			DailyLimit:    DefaultCopilotDailyLimit,
		},
	}

	if path == "" {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read config: %w", err)
	}

	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}

	return cfg, nil
}

// EnsureDirs creates config and data directories.
func (p Paths) EnsureDirs() error {
	for _, dir := range []string{p.ConfigDir, p.DataDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	return nil
}

// Write persists the configuration to a TOML file.
func Write(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create config file: %w", err)
	}
	defer f.Close()
	if err := toml.NewEncoder(f).Encode(cfg); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	return nil
}

// Validate checks that configuration values are acceptable.
func (c Config) Validate() error {
	if c.PollInterval() <= 0 {
		return fmt.Errorf("poll_interval must be positive")
	}
	if c.IdleThreshold() <= 0 {
		return fmt.Errorf("idle_threshold must be positive")
	}
	switch c.ActivityMode() {
	case ActivityModeIdleAndLock, ActivityModeLockOnly:
	case "":
		return fmt.Errorf("activity mode must not be empty")
	default:
		return fmt.Errorf("invalid activity mode %q: must be %q or %q", c.Activity.Mode, ActivityModeIdleAndLock, ActivityModeLockOnly)
	}
	if c.Server.ListenAddress == "" {
		return fmt.Errorf("listen_address must not be empty")
	}
	if c.App.Timezone != "" && c.App.Timezone != "local" {
		if _, err := time.LoadLocation(c.App.Timezone); err != nil {
			return fmt.Errorf("invalid timezone %q: %w", c.App.Timezone, err)
		}
	}
	if c.App.Language != "" && c.App.Language != "de" && c.App.Language != "en" {
		return fmt.Errorf("invalid language %q: must be 'de' or 'en'", c.App.Language)
	}
	if c.App.Navigation != "" && c.App.Navigation != "top" && c.App.Navigation != "side" {
		return fmt.Errorf("invalid navigation %q: must be 'top' or 'side'", c.App.Navigation)
	}
	if c.App.Theme != "" && c.App.Theme != "teal" && c.App.Theme != "aurora" {
		return fmt.Errorf("invalid theme %q: must be 'teal' or 'aurora'", c.App.Theme)
	}
	if c.CopilotInterval() <= 0 {
		return fmt.Errorf("copilot.fetch_interval must be positive")
	}
	if c.Copilot.DailyLimit < 0 {
		return fmt.Errorf("copilot.daily_limit must not be negative")
	}
	if c.App.TodayWeekdays != nil && len(c.App.TodayWeekdays) == 0 {
		return fmt.Errorf("today_weekdays must contain at least one valid weekday")
	}
	seen := make(map[time.Weekday]bool, len(c.App.TodayWeekdays))
	for _, value := range c.App.TodayWeekdays {
		weekday, ok := parseWeekday(value)
		if !ok {
			return fmt.Errorf("invalid today_weekdays value %q: use mon, tue, wed, thu, fri, sat, or sun", value)
		}
		if seen[weekday] {
			return fmt.Errorf("duplicate today_weekdays value %q", value)
		}
		seen[weekday] = true
	}
	return nil
}

func parseWeekday(value string) (time.Weekday, bool) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	for _, wd := range WeekdayOrder {
		if wd.Token == normalized {
			return wd.Weekday, true
		}
	}
	return time.Sunday, false
}
