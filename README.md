# Zeitspur

Zeitspur is a privacy-friendly local Linux application for automatically tracking working activity and managing whether individual workdays have already been booked in a company time-tracking system.

It is distributed as a single Go binary containing the complete web UI.
## Features

- Runs automatically in the background as a systemd user service.
- Detects user activity and inactivity using GNOME/Mutter or freedesktop D-Bus.
- Calculates likely working periods and pauses for each day.
- Provides local day, week, and month views.
- Allows each day to be marked as booked or not booked.
- Supports a manually maintained Booking Day.
- Allows manual booking-period closure with immutable snapshots.
- Stores all data locally in SQLite.

## Privacy model

Zeitspur is intentionally not a keylogger or activity monitor.

It **never** stores:

- pressed keys
- key codes
- typed text
- clipboard content
- active window titles
- application names
- document names
- browser URLs
- screenshots

Only high-level state transitions and timestamps are stored:
`active`, `idle`, `locked`, `unlocked`, `suspend`, `resume`, and `provider_error`.

The activity collector only knows whether the user is active, idle, locked, suspended, or unavailable.

## Architecture

```text
zeitspur/
├── cmd/zeitspur/          CLI entry point
├── internal/
│   ├── activity/          activity detection engine and providers
│   ├── booking/           day booking state and Booking Day
│   ├── closure/           booking-period closure and snapshots
│   ├── config/            TOML configuration
│   ├── database/          SQLite migrations and connection
│   ├── systemd/           user service installation
│   ├── timeline/          working-time calculations
│   └── clock/             testable clock abstraction
├── web/                   embedded HTML templates, CSS, and HTMX
├── migrations/            embedded SQLite schema migrations
├── packaging/             packaging helpers
├── go.mod
├── Makefile
├── config.example.toml
└── README.md
```

- Go 1.26+
- `net/http` with the `chi` router
- Server-rendered HTML with `html/template`
- HTMX for interactive UI actions
- SQLite via `modernc.org/sqlite` (pure Go, no CGO)
- D-Bus GNOME/Mutter and freedesktop providers

## Build

```bash
make build
```

This produces `zeitspur` with version injection:

```bash
CGO_ENABLED=0 go build -trimpath -ldflags "-X main.version=<version>" -o zeitspur ./cmd/zeitspur
```

## Install from release

Pre-built Linux binaries are available on the [releases page](https://github.com/smeir/Zeitspur/releases).

For `amd64`:

```bash
cd /tmp
curl -LO https://github.com/smeir/Zeitspur/releases/latest/download/zeitspur-linux-amd64.tar.gz
tar -xzf zeitspur-linux-amd64.tar.gz
chmod +x zeitspur
./zeitspur install
```

Replace `amd64` with `arm64` for ARM systems.

## Install or upgrade

Once you have the `zeitspur` binary, run:

```bash
./zeitspur install
```

This will:

1. Stop any already running `zeitspur` instance (for upgrades).
2. Copy the binary to `~/.local/bin/zeitspur`.
3. Write a systemd user unit to `~/.config/systemd/user/zeitspur.service`.
4. Run `systemctl --user daemon-reload`.
5. Enable and start the service.

No root privileges are required. The same command upgrades an existing installation to a new binary.

```bash
systemctl --user status zeitspur
journalctl --user -u zeitspur -f
```

To remove the service without deleting user data:

```bash
zeitspur uninstall
```

## Usage

```bash
zeitspur serve     # start activity collection and the local web server
zeitspur status    # print current state and today's tracked time
zeitspur open      # open the local web UI in the default browser
zeitspur install   # install and enable the systemd user service
zeitspur uninstall # disable and remove the systemd user service
zeitspur version   # print the binary version
```

The web UI listens on `127.0.0.1:8787` by default.

## Configuration

Copy `config.example.toml` to `~/.config/zeitspur/config.toml`:

```bash
mkdir -p ~/.config/zeitspur
cp config.example.toml ~/.config/zeitspur/config.toml
```

Available settings:

```toml
[activity]
poll_interval = "5s"
idle_threshold = "3m"
tail_credit = "30s"

[server]
listen_address = "127.0.0.1:8787"

[app]
timezone = "local" # or an IANA timezone such as "Europe/Berlin"
```

Changes to `listen_address` and `timezone` require a service restart to take full effect.

## Data location

```text
~/.config/zeitspur/config.toml
~/.local/share/zeitspur/zeitspur.db
```

To back up your data, copy those two paths. The database uses SQLite WAL mode, so you may also see:

```text
zeitspur.db-wal
zeitspur.db-shm
```

## Troubleshooting

### D-Bus / GNOME

If the status shows `unknown`, Zeitspur could not connect to the session D-Bus or no supported desktop provider was available. Check that:

- You are running inside a graphical session that exposes `org.gnome.Mutter.IdleMonitor`, `org.freedesktop.ScreenSaver`, or `org.kde.screensaver`.
- The `DBUS_SESSION_BUS_ADDRESS` environment variable is set.
- `d-feet` or `busctl --user` can see the relevant D-Bus service.

If no supported provider is available, the collector falls back to a mock provider and does not record activity automatically.

### Service fails to start

```bash
systemctl --user status zeitspur
journalctl --user -u zeitspur -f
```

Common causes:

- The binary was moved or deleted after installation.
- The graphical session target was not reached yet.
- Another `zeitspur serve` process is already running (a PID file protects the database).

## Development

```bash
make deps    # download dependencies
make build   # build the binary
make test    # run tests
make lint    # run go vet and go fmt
make run     # build and run serve
```

## Attribution

- **Architecture:** designed with GPT 5.5
- **Implementation:** Kimi 2.7 Code
- **Code review:** Opus 4.8

## License

MIT License. See [LICENSE](LICENSE).
