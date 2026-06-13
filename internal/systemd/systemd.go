// Package systemd installs and removes a user service for Zeitspur.
package systemd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const unitTemplate = `[Unit]
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
`

// Paths holds the filesystem paths for the systemd unit.
type Paths struct {
	BinaryDir  string
	BinaryPath string
	UnitDir    string
	UnitPath   string
}

// DefaultPaths returns the standard user paths.
func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("home dir: %w", err)
	}
	return Paths{
		BinaryDir:  filepath.Join(home, ".local", "bin"),
		BinaryPath: filepath.Join(home, ".local", "bin", "zeitspur"),
		UnitDir:    filepath.Join(home, ".config", "systemd", "user"),
		UnitPath:   filepath.Join(home, ".config", "systemd", "user", "zeitspur.service"),
	}, nil
}

// Install copies the binary and enables the user service.
func Install(currentBinary string) error {
	paths, err := DefaultPaths()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(paths.BinaryDir, 0o755); err != nil {
		return fmt.Errorf("mkdir binary dir: %w", err)
	}
	if err := os.MkdirAll(paths.UnitDir, 0o755); err != nil {
		return fmt.Errorf("mkdir unit dir: %w", err)
	}

	src, err := os.Open(currentBinary)
	if err != nil {
		return fmt.Errorf("open current binary: %w", err)
	}
	defer src.Close()

	dst, err := os.OpenFile(paths.BinaryPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("open destination binary: %w", err)
	}
	defer dst.Close()

	buf := make([]byte, 4096)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return fmt.Errorf("write binary: %w", werr)
			}
		}
		if err != nil {
			break
		}
	}

	if err := os.WriteFile(paths.UnitPath, []byte(unitTemplate), 0o644); err != nil {
		return fmt.Errorf("write unit file: %w", err)
	}

	if err := run("systemctl", "--user", "daemon-reload"); err != nil {
		return err
	}
	if err := run("systemctl", "--user", "enable", "zeitspur.service"); err != nil {
		return err
	}
	if err := run("systemctl", "--user", "start", "zeitspur.service"); err != nil {
		return err
	}

	fmt.Println("Zeitspur installed and started.")
	fmt.Println("Status: systemctl --user status zeitspur")
	fmt.Println("Logs:   journalctl --user -u zeitspur -f")
	return nil
}

// Uninstall disables and removes the user service without deleting data.
func Uninstall() error {
	paths, err := DefaultPaths()
	if err != nil {
		return err
	}

	_ = run("systemctl", "--user", "stop", "zeitspur.service")
	_ = run("systemctl", "--user", "disable", "zeitspur.service")
	_ = os.Remove(paths.UnitPath)

	if err := run("systemctl", "--user", "daemon-reload"); err != nil {
		return err
	}

	fmt.Println("Zeitspur user service removed.")
	fmt.Println("Binary and data were not deleted.")
	return nil
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
