// Package config loads and validates application configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

const (
	AppName = "zeitspur"

	DefaultPollInterval  = 5 * time.Second
	DefaultIdleThreshold = 3 * time.Minute
	DefaultTailCredit    = 30 * time.Second
	DefaultListenAddress = "127.0.0.1:8787"
	DefaultTimezone      = "local"
)

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
	TailCredit    string `toml:"tail_credit"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	ListenAddress string `toml:"listen_address"`
}

// AppConfig holds general application settings.
type AppConfig struct {
	Timezone string `toml:"timezone"`
}

// Duration helpers after parsing.
func (c Config) PollInterval() time.Duration {
	return parseDuration(c.Activity.PollInterval, DefaultPollInterval)
}
func (c Config) IdleThreshold() time.Duration {
	return parseDuration(c.Activity.IdleThreshold, DefaultIdleThreshold)
}
func (c Config) TailCredit() time.Duration {
	return parseDuration(c.Activity.TailCredit, DefaultTailCredit)
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
			TailCredit:    DefaultTailCredit.String(),
		},
		Server: ServerConfig{
			ListenAddress: DefaultListenAddress,
		},
		App: AppConfig{
			Timezone: DefaultTimezone,
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
	if c.TailCredit() < 0 {
		return fmt.Errorf("tail_credit must be non-negative")
	}
	if c.Server.ListenAddress == "" {
		return fmt.Errorf("listen_address must not be empty")
	}
	if c.App.Timezone != "" && c.App.Timezone != "local" {
		if _, err := time.LoadLocation(c.App.Timezone); err != nil {
			return fmt.Errorf("invalid timezone %q: %w", c.App.Timezone, err)
		}
	}
	return nil
}
