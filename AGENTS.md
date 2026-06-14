# AGENTS.md

Guidance for AI agents and developers working on **Zeitspur**. This file describes what the project is, how it is structured, and the rules that apply when changing the code.

## Project overview

Zeitspur is a **privacy-friendly, local Linux application** for automatically tracking working activity. It runs as a systemd user service in the background, detects activity/inactivity via GNOME/Mutter over D-Bus, and provides a local web UI to mark workdays as "booked".

- Shipped as a **single Go binary** including the web UI — no Node.js, no npm, no frontend build.
- All data is stored locally in **SQLite**.
- The web UI listens on `127.0.0.1:8787` by default.

## Privacy is non-negotiable

This is the project's most important principle. Zeitspur is **not** a keylogger and **not** an activity monitor.

It **never** stores: keystrokes, key codes, typed text, clipboard content, window titles, application names, document names, browser URLs, or screenshots.

Only high-level state transitions with timestamps are stored:
`active`, `idle`, `locked`, `unlocked`, `suspend`, `resume`, `provider_error`.

> **Rule for agents:** Any change that would capture or store finer-grained data must be rejected or explicitly confirmed with the user. The privacy model takes precedence over features.

## Tech stack

- **Go 1.26+**, `CGO_ENABLED=0` (pure Go)
- `net/http` with the **chi** router (`github.com/go-chi/chi/v5`)
- Server-rendered HTML with `html/template`, **HTMX** for interactive actions
- **SQLite** via `modernc.org/sqlite` (pure Go, no CGO)
- D-Bus integration via `github.com/godbus/dbus/v5`
- TOML configuration via `github.com/BurntSushi/toml`
- Templates, CSS, HTMX, and SQL migrations are embedded into the binary via `//go:embed`.

## Project structure

```text
zeitspur/
├── cmd/zeitspur/          CLI entry point (serve|status|open|install|uninstall)
├── internal/
│   ├── activity/          activity detection: engine, reconciler, providers (GNOME/Mock)
│   ├── booking/           per-day booking state and the manual Booking Day
│   ├── closure/           booking-period closure with immutable snapshots
│   ├── config/            TOML configuration, XDG paths, validation
│   ├── database/          SQLite connection and embedded migrations
│   ├── systemd/           user service installation
│   ├── timeline/          working-time calculation (day/range summaries)
│   └── clock/             testable clock abstraction
├── web/                   embedded HTML templates, CSS, HTMX, HTTP handlers
└── docs/                  documentation and reports
```

### Architecture principles

- **Respect the layering:** `activity` (capture) → `booking`/`closure`/`timeline` (business logic) → `web` (presentation). Do not pull UI logic into the domain packages.
- **Event sourcing:** Raw events live in `activity_events`. The `Reconciler` reproducibly computes `work_blocks` from them. Never treat derived data as the source of truth.
- **Use the testable abstractions:** Always access time via `clock.Clock` (never `time.Now()` directly in logic), and activity sources via the `ActivityProvider` interface. This keeps tests deterministic.
- **Migrations are append-only:** Never modify existing migration files in `internal/database/migrations/`. Add new schema changes as new, higher-numbered files (`002_*.sql`, …).

## Architecture documentation

The canonical architecture documentation lives in `docs/architecture.md`. It describes the system overview, layer boundaries, activity/event flow, timeline and booking flow, closure lifecycle, web UI security, persistence model, configuration/deployment, privacy constraints, and testability.

When making changes that affect any of those areas, update `docs/architecture.md` in the same commit so it stays accurate. This includes:

- Adding, removing, or renaming packages under `internal/` or `cmd/`.
- Changing the database schema or migration strategy.
- Modifying the activity detection/event model or the reconciler algorithm.
- Changing booking, closure, or timeline behavior.
- Adding state-changing HTTP endpoints or altering security boundaries.
- Changing the privacy model or what data is captured/stored.

## Build, test & lint

All standard tasks run through the `Makefile`:

```bash
make deps    # download dependencies
make build   # build the binary: CGO_ENABLED=0 go build -trimpath -o zeitspur ./cmd/zeitspur
make test    # go test ./...
make lint    # go fmt ./... && go vet ./... && go run scripts/boundaries.go
make run     # build and run 'serve'
make clean   # remove the binary, clear the test cache
```

**Before every commit:**

1. Run `make lint` — the code must be `gofmt`-clean and `go vet`-clean.
2. `make test` must pass.
3. For concurrent code, also run `go test -race ./...`.

## Code conventions

- **Idiomatic Go:** standard formatting (`gofmt`), clear error wrapping with `fmt.Errorf("...: %w", err)`.
- **Doc comments** on all exported symbols, starting with the symbol name.
- **Do not swallow errors:** use the `_ =` pattern sparingly; in CLI and render paths, log errors (`slog`) or return them as an HTTP status.
- **Logging** via `log/slog` with structured key/value pairs.
- **Always parameterize SQL** (`?` placeholders) — never interpolate values into query strings.
- **Timestamps** are stored as `time.RFC3339Nano` strings in SQLite; date fields (`work_date`) as `2006-01-02`.

## Security

- Bind the web UI to **localhost** only (default `127.0.0.1:8787`). No external exposure without explicit instruction.
- **CSRF protection** is active for all non-GET requests (double-submit cookie, `SameSite=Strict`). New state-changing endpoints must carry the CSRF token in the form.
- No root privileges; the systemd unit uses `NoNewPrivileges` and `PrivateTmp`.

## Data and path conventions

```text
~/.config/zeitspur/config.toml          configuration
~/.local/share/zeitspur/zeitspur.db      SQLite database (WAL mode)
~/.config/systemd/user/zeitspur.service  systemd unit
```

## Pull request expectations

- Focused, reviewable commits; commit messages in the style of the history (`feat:`, `fix:`, `style:` …).
- `make lint` and `make test` must pass before submitting.
- Back behavioral changes to capture, booking, or closure with appropriate tests.
- Never weaken the privacy model (see above).
