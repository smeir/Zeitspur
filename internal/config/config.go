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
	DefaultListenAddress = "127.0.0.1:8787"
	DefaultTimezone      = "local"
	DefaultLanguage      = "de"
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
}

// ActivityConfig holds activity-detection settings.
type ActivityConfig struct {
	PollInterval  string `toml:"poll_interval"`
	IdleThreshold string `toml:"idle_threshold"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	ListenAddress string `toml:"listen_address"`
}

// AppConfig holds general application settings.
type AppConfig struct {
	Timezone      string   `toml:"timezone"`
	Language      string   `toml:"language"`
	TodayWeekdays []string `toml:"today_weekdays"`
}

// Duration helpers after parsing.
func (c Config) PollInterval() time.Duration {
	return parseDuration(c.Activity.PollInterval, DefaultPollInterval)
}
func (c Config) IdleThreshold() time.Duration {
	return parseDuration(c.Activity.IdleThreshold, DefaultIdleThreshold)
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
		},
		Server: ServerConfig{
			ListenAddress: DefaultListenAddress,
		},
		App: AppConfig{
			Timezone:      DefaultTimezone,
			Language:      DefaultLanguage,
			TodayWeekdays: append([]string(nil), defaultTodayWeekdays...),
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
