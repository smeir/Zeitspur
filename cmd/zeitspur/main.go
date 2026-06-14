package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/smeir/zeitspur/internal/activity"
	"github.com/smeir/zeitspur/internal/booking"
	"github.com/smeir/zeitspur/internal/clock"
	"github.com/smeir/zeitspur/internal/config"
	"github.com/smeir/zeitspur/internal/database"
	"github.com/smeir/zeitspur/internal/systemd"
	"github.com/smeir/zeitspur/internal/timeline"
	"github.com/smeir/zeitspur/internal/timeutil"
	"github.com/smeir/zeitspur/web"
)

// version is set at link time via -ldflags for release builds.
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	switch cmd {
	case "version":
		fmt.Println(version)
	case "serve":
		if err := runServe(); err != nil {
			slog.Error("serve failed", "error", err)
			os.Exit(1)
		}
	case "status":
		if err := runStatus(); err != nil {
			slog.Error("status failed", "error", err)
			os.Exit(1)
		}
	case "open":
		if err := runOpen(); err != nil {
			slog.Error("open failed", "error", err)
			os.Exit(1)
		}
	case "install":
		if err := runInstall(); err != nil {
			slog.Error("install failed", "error", err)
			os.Exit(1)
		}
	case "uninstall":
		if err := runUninstall(); err != nil {
			slog.Error("uninstall failed", "error", err)
			os.Exit(1)
		}
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "Usage: zeitspur <serve|status|open|install|uninstall|version>\n")
}

func setup() (config.Config, config.Paths, *sql.DB, activity.ActivityProvider, error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return config.Config{}, config.Paths{}, nil, nil, err
	}
	if err := paths.EnsureDirs(); err != nil {
		return config.Config{}, config.Paths{}, nil, nil, err
	}

	cfg, err := config.Load(paths.ConfigFile)
	if err != nil {
		return config.Config{}, config.Paths{}, nil, nil, err
	}
	if err := cfg.Validate(); err != nil {
		return config.Config{}, config.Paths{}, nil, nil, err
	}

	db, err := database.Open(paths.DBFile)
	if err != nil {
		return config.Config{}, config.Paths{}, nil, nil, err
	}

	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		_ = db.Close()
		return config.Config{}, config.Paths{}, nil, nil, err
	}

	var provider activity.ActivityProvider
	provider, err = activity.NewGNOMEProvider(cfg.IdleThreshold())
	if err != nil {
		slog.Warn("GNOME D-Bus provider unavailable; trying freedesktop", "error", err)
		provider, err = activity.NewFreedesktopProvider(cfg.IdleThreshold())
		if err != nil {
			slog.Warn("freedesktop D-Bus provider unavailable; falling back to mock", "error", err)
			provider = activity.NewMockProvider(activity.ActivityUnknown)
		}
	}

	return cfg, paths, db, provider, nil
}

func runServe() error {
	cfg, paths, db, provider, err := setup()
	if err != nil {
		return err
	}
	defer db.Close()
	defer provider.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cleanup, err := ensureSingleInstance(paths.DBFile)
	if err != nil {
		return err
	}
	defer cleanup()

	clk := clock.System{}
	engine := activity.NewEngine(db, provider, clk, cfg.IdleThreshold(), cfg.PollInterval())

	go func() {
		if err := engine.Run(ctx); err != nil && err != context.Canceled {
			slog.Error("activity engine stopped", "error", err)
		}
	}()

	server, err := web.NewServer(db, cfg, paths, provider, clk, version)
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:         server.ListenAddress(),
		Handler:      server,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	slog.Info("zeitspur serving", "address", server.ListenAddress())
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func runStatus() error {
	_, paths, db, provider, err := setup()
	if err != nil {
		return err
	}
	defer db.Close()
	defer provider.Close()

	ctx := context.Background()
	date := time.Now().In(time.Local).Format("2006-01-02")

	state, err := provider.CurrentState(ctx)
	if err != nil {
		slog.Error("current state failed", "error", err)
		state = activity.ActivityUnknown
	}

	tsvc := timeline.NewService(db)
	sum, err := tsvc.Day(ctx, date)
	if err != nil {
		slog.Error("timeline day failed", "error", err)
	}

	br := booking.NewRepository(db)
	status, err := br.GetDay(ctx, date)
	if err != nil {
		slog.Error("booking status failed", "error", err)
		status = &booking.DayStatus{WorkDate: date}
	}

	fmt.Printf("Status:        %s\n", state)
	fmt.Printf("Today:         %s\n", date)
	fmt.Printf("Worked:        %s\n", timeutil.FormatMinutes(sum.WorkedMinutes))
	fmt.Printf("Pause:         %s\n", timeutil.FormatMinutes(sum.PauseMinutes))
	fmt.Printf("Booked:        %v\n", status.Booked)
	fmt.Printf("Database:      %s\n", paths.DBFile)
	fmt.Printf("Config:        %s\n", paths.ConfigFile)
	return nil
}

func runOpen() error {
	cfg, _, _, _, err := setup()
	if err != nil {
		return err
	}
	url := "http://" + cfg.Server.ListenAddress
	return exec.Command("xdg-open", url).Start()
}

func runInstall() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return systemd.Install(exe)
}

func runUninstall() error {
	return systemd.Uninstall()
}

func ensureSingleInstance(dbPath string) (func(), error) {
	// A simple pid file prevents multiple daemons from writing to the same DB.
	pidFile := dbPath + ".pid"
	data, err := os.ReadFile(pidFile)
	if err == nil {
		var pid int
		if _, err := fmt.Sscanf(string(data), "%d", &pid); err == nil {
			if processExists(pid) {
				return nil, fmt.Errorf("another zeitspur instance is already running (pid %d)", pid)
			}
		}
	}
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", os.Getpid())), 0o644); err != nil {
		return nil, err
	}
	return func() { _ = os.Remove(pidFile) }, nil
}

func processExists(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}
