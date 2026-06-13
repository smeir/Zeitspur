You are a senior Go engineer. Build Version 1 of a local Linux application for automatically tracking working activity and managing whether individual workdays have already been booked in a company time-tracking system.

The application should be called **Zeitspur** for now. Keep naming easy to change later.

## Primary goals

Build a privacy-friendly Linux tool that:

1. Runs automatically in the background.
2. Detects user activity and inactivity.
3. Calculates likely working periods and pauses for each day.
4. Provides local day, week, and month views.
5. Allows each day to be marked as booked or not booked.
6. Supports a manually maintained Booking Day.
7. Allows the user to manually close a booking period on the Booking Day.
8. Is distributed as a single Go binary containing the complete web UI.

The target environment is Ubuntu Linux, initially with GNOME.

---

# Technical stack

Use:

* Go, using the latest stable Go version
* `net/http` or the lightweight `chi` router
* Server-rendered HTML using `html/template`
* HTMX for interactive UI actions
* Small amounts of vanilla JavaScript where necessary
* SQLite
* A pure-Go SQLite driver that does not require CGO
* `go:embed` for all templates, JavaScript, CSS, migrations, and static assets
* D-Bus for GNOME activity, screen-lock, suspend, and resume integration
* `log/slog` for structured logging
* A systemd user service
* TOML or YAML for optional configuration

The application must build with:

```bash
CGO_ENABLED=0 go build -trimpath -o zeitspur ./cmd/zeitspur
```

Do not require Node.js, npm, Bun, or another frontend build tool.

Do not use React, Vue, Svelte, Electron, or Tauri.

---

# Deployment model

The complete application must be delivered as one executable binary.

Runtime data may be stored separately:

```text
~/.config/zeitspur/config.toml
~/.local/share/zeitspur/zeitspur.db
```

The embedded web application must listen only on localhost by default:

```text
127.0.0.1:8787
```

The binary should support at least:

```bash
zeitspur serve
zeitspur status
zeitspur open
zeitspur install
zeitspur uninstall
```

Expected behavior:

* `serve` starts activity collection and the local web server.
* `status` prints the current activity state and today's tracked time.
* `open` opens the local web UI in the default browser.
* `install` installs and enables a systemd user service.
* `uninstall` disables and removes the systemd user service without deleting user data.

---

# Privacy requirements

This application must not behave like a keylogger.

Never store:

* pressed keys
* key codes
* typed text
* clipboard content
* active window titles
* application names
* document names
* browser URLs
* screenshots

Only store activity state changes and timestamps.

Prefer querying the desktop idle duration through GNOME/Mutter over D-Bus.

The collector only needs to know whether the user is active, idle, locked, suspended, or unavailable.

Design the activity source behind an interface so other providers can be implemented later.

Example:

```go
type ActivityProvider interface {
    Name() string
    CurrentState(ctx context.Context) (ActivityState, error)
}
```

Possible states:

```go
type ActivityState string

const (
    ActivityActive    ActivityState = "active"
    ActivityIdle      ActivityState = "idle"
    ActivityLocked    ActivityState = "locked"
    ActivitySuspended ActivityState = "suspended"
    ActivityUnknown   ActivityState = "unknown"
)
```

Implement:

1. A GNOME/Mutter D-Bus provider.
2. A mock provider for automated tests.

Do not implement direct `/dev/input` or evdev access in Version 1.

---

# Activity and working-time model

Activity detection is only an approximation of actual working time.

Use configurable defaults:

```text
poll interval: 5 seconds
idle threshold: 3 minutes
tail credit after last activity: 30 seconds
timezone: system timezone
```

Expected behavior:

* Active samples form working blocks.
* Short gaps below the idle threshold remain part of the current working block.
* When inactivity reaches the configured threshold, a pause starts.
* Locking the screen immediately starts a pause.
* Suspend immediately ends the active block.
* Resume does not automatically start working time until new activity is detected.
* Application restarts must not produce unrealistic continuous working periods.
* Crossing midnight must split blocks into separate calendar days.
* Daylight-saving-time transitions must be handled safely.

Store state transitions instead of inserting one database row every five seconds.

Example event types:

```text
active
idle
locked
unlocked
suspend
resume
provider_error
service_started
service_stopped
```

Calculated working blocks should be stored or reproducibly derived from activity events.

The UI must make a clear distinction between:

* automatically detected working time
* manually added working time
* manually edited working time
* pauses
* ignored time

Manual corrections must not destroy the original activity events.

---

# Manual corrections

For every day, the user must be able to:

* add a work block
* edit start and end times
* delete or ignore a calculated work block
* convert a detected pause into working time
* split a work block
* merge adjacent work blocks
* add an optional note explaining a correction
* reset calculated blocks from the underlying activity events

All manual changes must be auditable.

Store:

* what was changed
* when it was changed
* the previous value
* the new value
* an optional reason

Do not build user accounts or multi-user support.

---

# Daily booking status

Booking in the company system is always done for a complete calendar day.

A single boolean per day is sufficient.

Each calendar day has:

```text
booked = true or false
```

Do not implement partial-day booking.

The UI must provide:

* “Mark day as booked”
* “Mark day as not booked”
* bulk selection of multiple days
* “Mark selected days as booked”
* clear visual indication of booked and unbooked days

A day with no tracked or manually added working time should not normally need booking, but the user may still manually change its booking state.

When the tracked time of an already booked day is changed later, show a warning that the booked day has changed after booking.

Do not automatically reset the booked flag.

---

# Booking Day

The Booking Day is maintained manually.

There are no recurrence rules and no automatic calculations such as:

* last Friday of the month
* fixed day of the month
* last working day
* holiday adjustments

The user enters a concrete date manually, for example:

```text
2026-06-19
```

Store the current Booking Day as a date.

The UI must allow the user to:

* set or replace the current Booking Day
* remove the current Booking Day
* see how many days remain until the Booking Day
* see all unbooked workdays in the open booking period
* see a warning when the Booking Day has passed and the period is still open

Do not automatically close anything when the Booking Day is reached.

---

# Booking-period closure

A booking period is closed manually through the web UI.

The closure happens on or for the Booking Day, not automatically at the end of a calendar month.

The user must explicitly press a button such as:

```text
Close booking period
```

Before closing, show a confirmation screen containing:

* the Booking Day
* the start and end date of the period
* number of tracked workdays
* number of booked workdays
* number of unbooked workdays
* total tracked working time
* a list of unbooked days
* warnings for booked days that were changed after being marked booked

Default period definition:

* For the first closure, include all existing tracked days up to and including the Booking Day.
* For later closures, include all days after the previous closure date up to and including the current Booking Day.

A period may be closed even if some workdays are still unbooked, but this must require an explicit confirmation acknowledging the warning.

When a period is closed, create an immutable closure record containing at least:

```text
id
period_start
period_end
booking_day
closed_at
tracked_workday_count
booked_workday_count
unbooked_workday_count
tracked_minutes_snapshot
```

Also store a snapshot of the included days and their booking states at the time of closure.

After closure:

* the historical closure record remains immutable
* the user may still edit old workdays
* later edits must not rewrite the historical snapshot
* the UI must indicate that the current data differs from the closed snapshot
* closing a period clears the current Booking Day
* the user can then manually enter the next Booking Day

Do not call this a “monthly close”, because booking periods do not necessarily align with calendar months.

---

# Suggested database model

Use SQLite migrations embedded in the binary.

A possible schema is:

```sql
CREATE TABLE activity_events (
    id INTEGER PRIMARY KEY,
    occurred_at TEXT NOT NULL,
    event_type TEXT NOT NULL,
    provider TEXT NOT NULL,
    metadata_json TEXT,
    created_at TEXT NOT NULL
);

CREATE TABLE work_blocks (
    id INTEGER PRIMARY KEY,
    work_date TEXT NOT NULL,
    started_at TEXT NOT NULL,
    ended_at TEXT NOT NULL,
    source TEXT NOT NULL,
    status TEXT NOT NULL,
    note TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE day_status (
    work_date TEXT PRIMARY KEY,
    booked INTEGER NOT NULL DEFAULT 0,
    booked_at TEXT,
    booking_revision INTEGER NOT NULL DEFAULT 0,
    current_revision INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL
);

CREATE TABLE booking_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    current_booking_day TEXT,
    updated_at TEXT NOT NULL
);

CREATE TABLE booking_closures (
    id INTEGER PRIMARY KEY,
    period_start TEXT NOT NULL,
    period_end TEXT NOT NULL,
    booking_day TEXT NOT NULL,
    closed_at TEXT NOT NULL,
    tracked_workday_count INTEGER NOT NULL,
    booked_workday_count INTEGER NOT NULL,
    unbooked_workday_count INTEGER NOT NULL,
    tracked_minutes_snapshot INTEGER NOT NULL
);

CREATE TABLE booking_closure_days (
    id INTEGER PRIMARY KEY,
    closure_id INTEGER NOT NULL,
    work_date TEXT NOT NULL,
    booked_snapshot INTEGER NOT NULL,
    tracked_minutes_snapshot INTEGER NOT NULL,
    day_revision_snapshot INTEGER NOT NULL,
    FOREIGN KEY (closure_id) REFERENCES booking_closures(id)
);

CREATE TABLE audit_log (
    id INTEGER PRIMARY KEY,
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    action TEXT NOT NULL,
    previous_value_json TEXT,
    new_value_json TEXT,
    reason TEXT,
    occurred_at TEXT NOT NULL
);
```

You may improve this schema, but preserve the specified behavior.

Use UTC timestamps internally where appropriate and calendar dates in the configured local timezone.

---

# Web UI

Build a clean, responsive local web interface.

It must work well on desktop and remain usable on a mobile browser.

Use semantic HTML, accessible forms, and minimal JavaScript.

## Navigation

Provide:

```text
Today
Week
Month
Booking
Closures
Settings
```

## Today view

Show:

* current status: active, idle, locked, suspended, or unknown
* start of the current working block
* tracked time today
* pause time today
* booking status for today
* a horizontal timeline
* calculated and manual work blocks
* controls for manual corrections
* button to mark the day booked or not booked

## Week view

Show:

* one row per day
* tracked working time
* pause time
* booked status
* warning if a booked day changed later
* total tracked time
* booked and unbooked workday counts

Include a simple bar visualization using CSS or embedded JavaScript.

## Month view

Show a calendar grid.

Each day should visually communicate:

* tracked working time
* booked
* unbooked
* no work
* changed after booking
* part of a closed booking period

Clicking a day opens the day detail view.

## Booking view

Show:

* current manually entered Booking Day
* remaining days or overdue status
* current open booking period
* all workdays in the period
* booking status for each day
* bulk booking controls
* closure preview
* manual “Close booking period” action

## Closures view

Show a list of historical closures with:

* period
* Booking Day
* closure timestamp
* tracked workdays
* booked workdays
* unbooked workdays
* total tracked time
* whether current data differs from the historical snapshot

A closure detail page must show the immutable day snapshot.

## Settings view

Allow configuration of:

* idle threshold
* poll interval
* tail credit
* listen address
* timezone or “use system timezone”
* current Booking Day

Configuration changes should be validated.

---

# HTMX usage

Use HTMX for:

* marking days booked or unbooked
* bulk booking actions
* adding and editing work blocks
* deleting or ignoring blocks
* setting the Booking Day
* loading closure previews
* confirming period closure
* refreshing current status

Return HTML fragments from the server.

Do not create a JSON-only SPA architecture.

Use normal HTML form submissions as a fallback where practical.

---

# systemd integration

Include an embedded or generated systemd user unit similar to:

```ini
[Unit]
Description=Zeitspur local working activity tracker
After=graphical-session.target

[Service]
Type=simple
ExecStart=%h/.local/bin/zeitspur serve
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=default.target
```

The `install` command should:

1. Copy or install the current binary to `~/.local/bin/zeitspur`.
2. Write the unit to `~/.config/systemd/user/zeitspur.service`.
3. Run `systemctl --user daemon-reload`.
4. Enable and start the service.
5. Print useful status and log commands.

Do not require root privileges.

---

# Reliability requirements

The application must:

* use database transactions for multi-step changes
* enable SQLite foreign keys
* use a sensible SQLite busy timeout
* prevent concurrent period-closure transactions
* handle abrupt shutdown without corrupting data
* recover after D-Bus provider failures
* continue serving the UI when the activity provider is temporarily unavailable
* log errors without writing sensitive information
* gracefully shut down on SIGINT and SIGTERM
* prevent multiple daemon instances from writing to the same database
* validate that work blocks have an end after their start
* prevent impossible overlapping manual blocks, or handle them deterministically
* split work blocks at midnight
* include CSRF protection for state-changing web requests
* bind only to loopback addresses unless explicitly overridden

---

# Testing

Add automated tests for at least:

1. Idle-threshold transitions.
2. Short inactivity gaps.
3. Screen lock.
4. Suspend and resume.
5. Service restart.
6. Splitting at midnight.
7. Daylight-saving-time transitions.
8. Manual work-block corrections.
9. Booking and unbooking a complete day.
10. A booked day changing later.
11. First booking-period closure.
12. Subsequent booking-period closure.
13. Closing with unbooked days after explicit confirmation.
14. Immutable closure snapshots.
15. Detection of differences between current data and closure snapshots.
16. Database migrations.
17. HTTP handler behavior.
18. CSRF rejection.
19. Concurrent closure attempts.

Use table-driven Go tests where appropriate.

The activity engine should be testable without sleeping in real time. Inject a clock abstraction.

Example:

```go
type Clock interface {
    Now() time.Time
}
```

---

# Project structure

Use a maintainable structure similar to:

```text
zeitspur/
├── cmd/
│   └── zeitspur/
│       └── main.go
├── internal/
│   ├── activity/
│   ├── booking/
│   ├── closure/
│   ├── config/
│   ├── database/
│   ├── timeline/
│   ├── web/
│   └── systemd/
├── migrations/
├── web/
│   ├── templates/
│   └── static/
├── packaging/
├── go.mod
├── README.md
└── LICENSE
```

Avoid unnecessary abstraction, but keep activity collection, time calculation, booking, closure, persistence, and HTTP delivery clearly separated.

---

# Version 1 scope exclusions

Do not implement:

* cloud synchronization
* multiple users
* authentication for remote access
* direct integration with the company time-tracking system
* project-level time tracking
* partial-day booking
* automatic Booking Day rules
* automatic booking-period closure
* monthly closure semantics
* active-window tracking
* application-name tracking
* browser tracking
* keyboard-event logging
* mouse-event logging
* calendar integration
* tray icons
* native desktop windows
* external APIs
* automatic updates

---

# Deliverables

Produce:

1. A complete compilable Go project.
2. Embedded templates and static assets.
3. Embedded SQLite migrations.
4. GNOME/Mutter activity-provider implementation.
5. Mock activity provider.
6. systemd user-service installation support.
7. Day, week, month, booking, closure, and settings pages.
8. Automated tests.
9. A README containing:

   * architecture overview
   * privacy model
   * build instructions
   * installation instructions
   * systemd commands
   * database location
   * backup instructions
   * troubleshooting for D-Bus and GNOME
10. A sample configuration file.
11. A Makefile with at least:

```bash
make build
make test
make lint
make run
```

12. A `.gitignore`.
13. A sensible initial Git commit structure if the environment supports Git.

---

# Implementation order

Work incrementally in this order:

1. Create the project skeleton and buildable CLI.
2. Implement configuration and directory handling.
3. Add SQLite migrations and repositories.
4. Implement the activity engine with a mock provider.
5. Add extensive activity-engine tests.
6. Implement the GNOME/Mutter D-Bus provider.
7. Implement booking-day status and daily booking.
8. Implement manual booking-period closure and immutable snapshots.
9. Add the HTTP server and base templates.
10. Add Today, Week, and Month views.
11. Add Booking and Closures workflows.
12. Add manual time corrections.
13. Add systemd installation commands.
14. Add hardening, CSRF protection, locking, graceful shutdown, and documentation.
15. Run all tests and fix all static-analysis issues.

After every major step:

* run `go test ./...`
* run `go vet ./...`
* keep the application compilable
* avoid leaving placeholder implementations in completed features

When details are ambiguous, choose the simplest privacy-friendly behavior consistent with this specification and document the decision in the README.
