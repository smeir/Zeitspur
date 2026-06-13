package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoad_EmptyPathReturnsDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("load empty path: %v", err)
	}
	if cfg.PollInterval() != DefaultPollInterval {
		t.Fatalf("expected default poll interval %v, got %v", DefaultPollInterval, cfg.PollInterval())
	}
	if cfg.IdleThreshold() != DefaultIdleThreshold {
		t.Fatalf("expected default idle threshold %v, got %v", DefaultIdleThreshold, cfg.IdleThreshold())
	}
	if cfg.TailCredit() != DefaultTailCredit {
		t.Fatalf("expected default tail credit %v, got %v", DefaultTailCredit, cfg.TailCredit())
	}
	if cfg.Server.ListenAddress != DefaultListenAddress {
		t.Fatalf("expected default listen address %q, got %q", DefaultListenAddress, cfg.Server.ListenAddress)
	}
	if cfg.App.Timezone != DefaultTimezone {
		t.Fatalf("expected default timezone %q, got %q", DefaultTimezone, cfg.App.Timezone)
	}
}

func TestLoad_MissingFileReturnsDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "does-not-exist.toml"))
	if err != nil {
		t.Fatalf("load missing file: %v", err)
	}
	if cfg.PollInterval() != DefaultPollInterval {
		t.Fatalf("expected default poll interval, got %v", cfg.PollInterval())
	}
}

func TestLoad_ValidFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := `
[activity]
poll_interval = "10s"
idle_threshold = "2m"
tail_credit = "45s"

[server]
listen_address = "127.0.0.1:9999"

[app]
timezone = "UTC"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.PollInterval() != 10*time.Second {
		t.Fatalf("expected poll interval 10s, got %v", cfg.PollInterval())
	}
	if cfg.IdleThreshold() != 2*time.Minute {
		t.Fatalf("expected idle threshold 2m, got %v", cfg.IdleThreshold())
	}
	if cfg.TailCredit() != 45*time.Second {
		t.Fatalf("expected tail credit 45s, got %v", cfg.TailCredit())
	}
	if cfg.Server.ListenAddress != "127.0.0.1:9999" {
		t.Fatalf("expected listen address 127.0.0.1:9999, got %q", cfg.Server.ListenAddress)
	}
	if cfg.App.Timezone != "UTC" {
		t.Fatalf("expected timezone UTC, got %q", cfg.App.Timezone)
	}
}

func TestWriteAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := Config{
		Activity: ActivityConfig{
			PollInterval:  "7s",
			IdleThreshold: "90s",
			TailCredit:    "15s",
		},
		Server: ServerConfig{ListenAddress: "127.0.0.1:1234"},
		App:    AppConfig{Timezone: "Europe/Berlin"},
	}

	if err := Write(path, cfg); err != nil {
		t.Fatalf("write config: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if loaded.PollInterval() != cfg.PollInterval() {
		t.Fatalf("expected poll interval %v, got %v", cfg.PollInterval(), loaded.PollInterval())
	}
	if loaded.IdleThreshold() != cfg.IdleThreshold() {
		t.Fatalf("expected idle threshold %v, got %v", cfg.IdleThreshold(), loaded.IdleThreshold())
	}
	if loaded.TailCredit() != cfg.TailCredit() {
		t.Fatalf("expected tail credit %v, got %v", cfg.TailCredit(), loaded.TailCredit())
	}
	if loaded.Server.ListenAddress != cfg.Server.ListenAddress {
		t.Fatalf("expected listen address %q, got %q", cfg.Server.ListenAddress, loaded.Server.ListenAddress)
	}
	if loaded.App.Timezone != cfg.App.Timezone {
		t.Fatalf("expected timezone %q, got %q", cfg.App.Timezone, loaded.App.Timezone)
	}
}

func TestValidate_DefaultsValid(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected defaults to be valid: %v", err)
	}
}

func TestValidate_InvalidTimezone(t *testing.T) {
	cfg, _ := Load("")
	cfg.App.Timezone = "Mars/Phobos"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "timezone") {
		t.Fatalf("expected invalid timezone error, got %v", err)
	}
}

func TestValidate_InvalidDurations(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(*Config)
		errSub string
	}{
		{
			name: "zero poll interval",
			setup: func(c *Config) {
				c.Activity.PollInterval = "0s"
			},
			errSub: "poll_interval",
		},
		{
			name: "negative idle threshold",
			setup: func(c *Config) {
				c.Activity.IdleThreshold = "-1m"
			},
			errSub: "idle_threshold",
		},
		{
			name: "negative tail credit",
			setup: func(c *Config) {
				c.Activity.TailCredit = "-5s"
			},
			errSub: "tail_credit",
		},
		{
			name: "empty listen address",
			setup: func(c *Config) {
				c.Server.ListenAddress = ""
			},
			errSub: "listen_address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, _ := Load("")
			tt.setup(&cfg)
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.errSub) {
				t.Fatalf("expected error containing %q, got %v", tt.errSub, err)
			}
		})
	}
}

func TestDefaultPaths(t *testing.T) {
	paths, err := DefaultPaths()
	if err != nil {
		t.Fatalf("default paths: %v", err)
	}
	if !strings.Contains(paths.ConfigDir, AppName) {
		t.Fatalf("expected config dir to contain %q, got %q", AppName, paths.ConfigDir)
	}
	if !strings.Contains(paths.DataDir, AppName) {
		t.Fatalf("expected data dir to contain %q, got %q", AppName, paths.DataDir)
	}
}
