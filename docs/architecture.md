# Architecture

Zeitspur is a privacy-friendly, local Linux application for automatically tracking working activity. It is shipped as a single Go binary that includes the web UI, SQLite persistence, activity detection, and systemd user-service management. There is no Node.js or frontend build step.

## System Overview

```mermaid
flowchart LR
  user[User / systemd]
  cli[zeitspur CLI]
  provider[ActivityProvider]
  engine[activity.Engine]
  reconciler[activity.Reconciler]
  web[web.Server]
  handlers[HTTP handlers]
  templates[html/template + HTMX]
  static[Embedded static assets]
  db[(SQLite)]
  config[TOML config]

  user --> cli
  cli --> engine
  cli --> web
  cli --> config
  engine --> provider
  engine --> db
  engine --> reconciler
  reconciler --> db
  web --> handlers
  handlers --> db
  handlers --> templates
  handlers --> static
  web --> config
```

The `zeitspur` binary has five subcommands: `serve`, `status`, `open`, `install`, and `uninstall`. `serve` is the runtime command used by the systemd user service. It starts the activity collection engine and the local HTTP server in the same process.

Key principles:

- **Single binary:** templates, CSS, HTMX, SQLite migrations, and the systemd unit template are embedded via `//go:embed`.
- **No CGO:** `CGO_ENABLED=0`, SQLite is pure Go via `modernc.org/sqlite`.
- **Local only:** the web UI binds to `127.0.0.1:8787` by default.
- **Event sourcing:** raw activity events are the source of truth; `work_blocks` are derived by the reconciler.
- **Privacy first:** only high-level state transitions are stored.

## Layer Boundaries

```mermaid
flowchart TB
  subgraph cmd[cmd/zeitspur]
    main[main.go]
  end

  subgraph activity[internal/activity]
    provider[ActivityProvider]
    engine[Engine]
    reconciler[Reconciler]
  end

  subgraph domain[Domain]
    booking[booking.Repository]
    closure[closure.Repository]
    timeline[timeline.Service]
  end

  subgraph infra[Infrastructure]
    db[database]
    config[config]
    clock[clock.Clock]
    systemd[systemd]
    i18n[i18n]
  end

  subgraph web[web]
    server[Server]
    handlers[handlers.go]
    templates[templates/*.html]
    static[static/*]
  end

  main --> engine
  main --> web
  main --> config
  main --> db
  main --> systemd

  engine --> provider
  engine --> reconciler
  engine --> clock
  engine --> db

  web --> handlers
  handlers --> booking
  handlers --> closure
  handlers --> timeline
  handlers --> config
  handlers --> clock
  handlers --> i18n
  handlers --> db

  booking --> db
  closure --> db
  timeline --> db
  db --> migrations[migrations/*.sql]
```

The codebase keeps three layers distinct:

1. **Capture (`internal/activity`)** polls the `ActivityProvider` and writes raw events.
2. **Domain (`booking`, `closure`, `timeline`)** reads raw events and derived blocks and implements business rules.
3. **Presentation (`web`)** renders HTML and handles form posts; it never contains capture or calculation logic.

`scripts/boundaries.go` enforces these rules by checking every internal import. The only current exception is `web` importing `internal/activity` because the `ActivityProvider` contract still lives in the capture package. When that contract is moved to a dedicated package, the exception should be removed.

## Activity Detection And Event Flow

```mermaid
sequenceDiagram
  participant Engine as activity.Engine
  participant Provider as ActivityProvider
  participant DB as SQLite
  participant Reconciler as activity.Reconciler

  loop every poll_interval
    Engine->>Provider: CurrentState()
    Provider-->>Engine: active | idle | locked | suspended | unknown
    opt state changed
      Engine->>DB: INSERT activity_events
      Engine->>Reconciler: RebuildDay(loc, day)
      Reconciler->>DB: SELECT activity_events
      Reconciler->>Reconciler: computeBlocks
      Reconciler->>DB: DELETE/INSERT work_blocks
    end
  end
```

`ActivityProvider` is the seam between the engine and the desktop environment:

| Provider | File | Behaviour |
| --- | --- | --- |
| GNOME/Mutter | `internal/activity/gnome_provider.go` | Queries `org.gnome.Mutter.IdleMonitor` for idle time and `org.gnome.desktop.screensaver` / `org.freedesktop.ScreenSaver` for lock state. |
| Freedesktop | `internal/activity/freedesktop_provider.go` | Generic fallback that queries `org.freedesktop.ScreenSaver` and `org.kde.screensaver` for idle time and lock state, covering KDE and other freedesktop-compatible desktops. |
| Mock | `internal/activity/mock.go` | Programmable state for tests and fallback when D-Bus is unavailable. |

At startup the CLI tries providers in the order GNOME → Freedesktop → Mock, so KDE and other freedesktop-compatible desktops are used automatically when the GNOME-specific interfaces are not present.

The engine (`internal/activity/engine.go`) runs a ticker at `poll_interval`. On each tick it asks the provider for the current state and records an event only when the state changes:

- `active` is emitted whenever the user becomes active after `idle`, `locked`, or `suspended`.
- `idle` is emitted once `idle_threshold` has passed since the last activity; the timestamp is back-dated to the real idle start.
- `locked` / `unlocked` and `suspend` / `resume` map directly to lock and power events.
- `service_started` and `service_stopped` bracket the engine lifecycle.
- `provider_error` is recorded once per consecutive failure with error metadata.

The reconciler (`internal/activity/reconcile.go`) computes `work_blocks` from the event stream:

- A block starts on `active`, `unlocked`, or `resume`.
- A block ends on `idle`, `locked`, `suspend`, `service_started`, or `service_stopped`.
- Idle events add `tail_credit` to the end of the block so small pauses do not split work time.
- Blocks that cross midnight are split per calendar day.
- Only `detected`/`active` blocks are replaced on each rebuild; manual blocks and ignored/deleted blocks are preserved.

All state names are defined in `internal/activity/types.go`.

## Timeline And Booking Flow

```mermaid
flowchart LR
  today[Today / Day view] --> tsvc[timeline.Service]
  week[Week view] --> tsvc
  month[Month view] --> tsvc
  booking[Booking view] --> tsvc
  booking --> br[booking.Repository]
  tsvc --> db[(work_blocks + day_status)]
  br --> db

  booking --> cr[closure.Repository]
  cr --> db
```

`timeline.Service` (`internal/timeline/timeline.go`) aggregates stored work blocks into `DaySummary` structs:

- `WorkedMinutes` is the sum of active block durations for the day.
- `PauseMinutes` is the sum of gaps between active blocks.
- `TotalMinutes` is `WorkedMinutes + PauseMinutes`.
- `ChangedAfterBooking` is true when the day is booked but the current revision is newer than the booking revision.

`booking.Repository` (`internal/booking/repository.go`) manages `day_status` and the singleton `booking_settings` row:

- `SetBooked` toggles the booked flag, records `booked_at`, and bumps both `booking_revision` and `current_revision`.
- `BumpRevision` increments `current_revision` when a work block is manually added or ignored, so the UI can warn that data changed after booking.
- `SetBookingDay` / `ClearBookingDay` configure the manual Booking Day that drives the open booking period.

## Closure Lifecycle

```mermaid
flowchart TD
  booking[Booking Day set] --> preview[/booking/close-preview]
  preview --> cr[closure.Repository]
  cr --> periodStart[PeriodStart]
  periodStart --> lastClosure{Last closure?}
  lastClosure -- yes --> dayAfter[day after last period end]
  lastClosure -- no --> earliestBlock[earliest work block]
  earliestBlock --> fallback[booking day]
  dayAfter --> range
  fallback --> range
  range --> tsvc[timeline.Range]
  tsvc --> summaries[DaySummary list]
  summaries --> confirm{unbooked days?}
  confirm -- yes --> ack[confirm=true]
  ack --> close[/booking/close]
  confirm -- no --> close
  close --> cr.Create
  cr.Create --> snapshot[Immutable closure snapshot]
  snapshot --> clear[Clear Booking Day]
```

`closure.Repository` (`internal/closure/repository.go`) implements manual booking-period closure:

- `PeriodStart` determines the first day of the next open period from the last closure, the earliest work block, or the Booking Day.
- `Create` writes an immutable `booking_closures` record plus one `booking_closure_days` snapshot row per day.
- `HasDifferenceSinceClosure` compares current `day_status` revisions and booked flags against the snapshot.
- A simple advisory lock in `closure_lock` prevents concurrent closures.

Closing a period requires confirmation when unbooked days remain. After a successful closure the Booking Day is cleared.

## Web UI And Security

```mermaid
flowchart LR
  browser[Browser] --> chi[chi router]
  chi --> csrf[CSRF middleware]
  csrf --> handlers[HTTP handlers]
  handlers --> templates[html/template]
  templates --> layout[layout.html]
  templates --> page[today.html, week.html, ...]
  handlers --> static[htmx.min.js, style.css]
  handlers --> db[(SQLite)]
```

`web.Server` (`web/server.go`) configures the chi router and renders embedded templates. It supports German and English via `internal/i18n`. Templates are parsed once per locale at startup and share `layout.html` plus a content template.

CSRF protection applies to every non-GET/HEAD request:

- A random token is stored in an `HttpOnly`, `SameSite=Strict` cookie named `csrf_token`.
- State-changing forms must include the same token in `csrf_token`.
- HTMX requests are protected the same way because they are submitted as normal form posts.

The web UI never exposes the database directly; all access goes through the domain repositories.

## Persistence Model

```mermaid
erDiagram
  activity_events ||--o{ work_blocks : "derived via Reconciler"
  work_blocks }o--o| day_status : "referenced by work_date"
  booking_settings ||--o| booking_closures : "Booking Day drives period"
  booking_closures ||--|{ booking_closure_days : "contains snapshot"

  activity_events {
    INTEGER id PK
    TEXT occurred_at
    TEXT event_type
    TEXT provider
    TEXT metadata_json
    TEXT created_at
  }

  work_blocks {
    INTEGER id PK
    TEXT work_date
    TEXT started_at
    TEXT ended_at
    TEXT source
    TEXT status
    TEXT note
    TEXT created_at
    TEXT updated_at
  }

  day_status {
    TEXT work_date PK
    INTEGER booked
    TEXT booked_at
    INTEGER booking_revision
    INTEGER current_revision
    TEXT updated_at
  }

  booking_settings {
    INTEGER id PK
    TEXT current_booking_day
    TEXT updated_at
  }

  booking_closures {
    INTEGER id PK
    TEXT period_start
    TEXT period_end
    TEXT booking_day
    TEXT closed_at
    INTEGER tracked_workday_count
    INTEGER booked_workday_count
    INTEGER unbooked_workday_count
    INTEGER tracked_minutes_snapshot
  }

  booking_closure_days {
    INTEGER id PK
    INTEGER closure_id FK
    TEXT work_date
    INTEGER booked_snapshot
    INTEGER tracked_minutes_snapshot
    INTEGER day_revision_snapshot
  }

  audit_log {
    INTEGER id PK
    TEXT entity_type
    TEXT entity_id
    TEXT action
    TEXT previous_value_json
    TEXT new_value_json
    TEXT reason
    TEXT occurred_at
  }
```

Schema details:

- Timestamps are stored as `time.RFC3339Nano` strings.
- `work_date`, `period_start`, `period_end`, and `booking_day` are stored as `2006-01-02` strings.
- `work_blocks.source` is either `detected` or `manual`.
- `work_blocks.status` is `active`, `ignored`, or `deleted`.
- `activity_events` is the source of truth; `work_blocks` can be recomputed from it.
- `audit_log` is defined but currently unused; it exists for future accountability features.

Migrations live in `internal/database/migrations/` and are embedded into the binary. They are **append-only**: never modify an existing migration, always add a new higher-numbered file.

## Configuration And Deployment

```mermaid
flowchart LR
  binary[zeitspur binary] --> install[zeitspur install]
  install --> systemd[systemd --user]
  systemd --> serve[zeitspur serve]
  serve --> config[config.toml]
  serve --> db[zeitspur.db]
  config --> xdgConfig["~/.config/zeitspur"]
  db --> xdgData["~/.local/share/zeitspur"]
```

Configuration is loaded from `~/.config/zeitspur/config.toml`:

| Section | Key | Default | Purpose |
| --- | --- | --- | --- |
| `activity` | `poll_interval` | `5s` | How often the engine polls the provider. |
| `activity` | `idle_threshold` | `3m` | Time before the user is considered idle. |
| `activity` | `tail_credit` | `30s` | Grace period added after idle to keep short pauses inside a block. |
| `server` | `listen_address` | `127.0.0.1:8787` | Web UI bind address. |
| `app` | `timezone` | `local` | `local` or an IANA timezone. |
| `app` | `language` | `de` | UI language: `de` or `en`. |

`internal/config/config.go` loads TOML via `github.com/BurntSushi/toml`, provides defaults, and validates values. `internal/systemd/systemd.go` installs a user unit that waits for `graphical-session.target`, runs the binary from `~/.local/bin/zeitspur`, and uses `NoNewPrivileges=true` and `PrivateTmp=true`.

## Privacy Constraints

Zeitspur is intentionally **not** a keylogger or activity monitor. The following data is never captured or stored:

- Keystrokes, key codes, or typed text.
- Clipboard content.
- Window titles, application names, document names, or browser URLs.
- Screenshots.
- Any pixel, input, or content data.

Only these high-level state transitions with timestamps are persisted:

```text
active, idle, locked, unlocked, suspend, resume,
service_started, service_stopped, provider_error
```

Any change that would capture finer-grained data must be rejected or explicitly confirmed with the user. The privacy model takes precedence over features.

## Testability

Two abstractions keep the code testable:

1. `clock.Clock` (`internal/clock/clock.go`) replaces direct calls to `time.Now()`. Tests use `clock.Fixed`.
2. `activity.ActivityProvider` allows tests to inject a `MockProvider` instead of opening a real D-Bus connection.

Unit tests cover:

- `internal/activity/reconcile_test.go` and `reconcile_dst_test.go` for block computation including DST transitions.
- `internal/activity/engine_test.go` for polling and event insertion.
- `internal/timeline/timeline_test.go` for aggregation.
- `internal/booking/repository_test.go` and `internal/booking/changed_test.go` for booking state.
- `internal/closure/repository_test.go` for closure snapshots.
- `internal/config/config_test.go` for configuration loading and validation.
- `internal/i18n/catalog_test.go` for translation keys.
- `web/server_test.go` for CSRF, booking, and localization.

## Build And Test

Use the `Makefile` at the repository root:

```bash
make deps        # download dependencies
make build       # CGO_ENABLED=0 go build -trimpath -o zeitspur ./cmd/zeitspur
make test        # go test ./...
make lint        # go fmt ./... && go vet ./... && go run scripts/boundaries.go
make boundaries  # run the architectural import-boundary check only
make run         # build and run 'serve'
make clean       # remove the binary and clear the test cache
```

Before committing:

1. Run `make lint`; the code must be `gofmt`-clean, `go vet`-clean, and pass the boundary check.
2. Run `make test`.
3. For concurrency-related changes, also run `go test -race ./...`.
