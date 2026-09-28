// Package autostart installs a periodic `skenv sync --quiet`: a LaunchAgent
// on macOS and a systemd user timer on Linux.
package autostart

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
)

// Label is the LaunchAgent label.
const Label = "com.qunaxis.skenv"

// Runner runs service manager commands (launchctl, systemctl); tests replace
// it.
type Runner func(ctx context.Context, name string, args ...string) (string, error)

// ExecRunner runs commands for real.
func ExecRunner(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// Config describes one installation.
type Config struct {
	GOOS string
	Home string
	Exe  string // absolute path to the skenv binary
	Log  string // ~/.local/state/skenv/autostart.log
	Path string // PATH for the job, so it can find git
	UID  int
	Run  Runner
}

// Files returns the unit files for c.GOOS, keyed by path.
func (c Config) Files() (map[string]string, error) {
	switch c.GOOS {
	case "darwin":
		var b bytes.Buffer
		if err := plistTmpl.Execute(&b, c); err != nil {
			return nil, err
		}
		return map[string]string{c.plistPath(): b.String()}, nil
	case "linux":
		var svc, tmr bytes.Buffer
		if err := serviceTmpl.Execute(&svc, c); err != nil {
			return nil, err
		}
		if err := timerTmpl.Execute(&tmr, c); err != nil {
			return nil, err
		}
		dir := c.systemdDir()
		return map[string]string{
			filepath.Join(dir, "skenv.service"): svc.String(),
			filepath.Join(dir, "skenv.timer"):   tmr.String(),
		}, nil
	}
	return nil, fmt.Errorf("autostart is not supported on %s (only darwin and linux)", c.GOOS)
}

func (c Config) plistPath() string {
	return filepath.Join(c.Home, "Library", "LaunchAgents", Label+".plist")
}

func (c Config) systemdDir() string { return filepath.Join(c.Home, ".config", "systemd", "user") }

func (c Config) domain() string { return "gui/" + strconv.Itoa(c.UID) }

// Enable writes the unit files and (re)loads them; it is idempotent.
func (c Config) Enable(ctx context.Context) error {
	files, err := c.Files()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.Log), 0o755); err != nil {
		return err
	}
	for p, content := range files {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			return err
		}
	}
	switch c.GOOS {
	case "darwin":
		_, _ = c.Run(ctx, "launchctl", "bootout", c.domain()+"/"+Label)
		_, err = c.Run(ctx, "launchctl", "bootstrap", c.domain(), c.plistPath())
	case "linux":
		if _, err = c.Run(ctx, "systemctl", "--user", "daemon-reload"); err == nil {
			_, err = c.Run(ctx, "systemctl", "--user", "enable", "--now", "skenv.timer")
		}
		if err == nil {
			_, err = c.Run(ctx, "systemctl", "--user", "restart", "skenv.timer")
		}
	}
	return err
}

// Disable unloads and removes the unit files.
func (c Config) Disable(ctx context.Context) error {
	files, err := c.Files()
	if err != nil {
		return err
	}
	switch c.GOOS {
	case "darwin":
		_, _ = c.Run(ctx, "launchctl", "bootout", c.domain()+"/"+Label)
	case "linux":
		_, _ = c.Run(ctx, "systemctl", "--user", "disable", "--now", "skenv.timer")
	}
	for p := range files {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	if c.GOOS == "linux" {
		_, _ = c.Run(ctx, "systemctl", "--user", "daemon-reload")
	}
	return nil
}

// Status describes the installation.
type Status struct {
	Installed bool   // unit files present
	Loaded    bool   // known to launchd / timer active
	Current   bool   // unit files match what Enable would write
	Detail    string // human-readable summary
}

// Status inspects the installation without changing it.
func (c Config) Status(ctx context.Context) (Status, error) {
	files, err := c.Files()
	if err != nil {
		return Status{}, err
	}
	st := Status{Installed: true, Current: true}
	for p, want := range files {
		got, err := os.ReadFile(p)
		if err != nil {
			st.Installed = false
			st.Current = false
			continue
		}
		if string(got) != want {
			st.Current = false
		}
	}
	switch c.GOOS {
	case "darwin":
		_, err := c.Run(ctx, "launchctl", "print", c.domain()+"/"+Label)
		st.Loaded = err == nil
	case "linux":
		out, err := c.Run(ctx, "systemctl", "--user", "is-active", "skenv.timer")
		st.Loaded = err == nil && strings.TrimSpace(out) == "active"
	}
	switch {
	case !st.Installed:
		st.Detail = "disabled"
	case !st.Loaded:
		st.Detail = "installed but not loaded; run `skenv autostart enable`"
	case !st.Current:
		st.Detail = "enabled, but the unit differs from this skenv binary; run `skenv autostart enable`"
	default:
		st.Detail = "enabled: `skenv sync --quiet` at load and every hour, log " + c.Log
	}
	return st, nil
}

var funcs = template.FuncMap{"xml": func(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}, "sdq": strconv.Quote} // systemd accepts C-style quoted arguments

var plistTmpl = template.Must(template.New("plist").Funcs(funcs).Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<!-- managed by skenv: skenv autostart enable|disable -->
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>` + Label + `</string>
  <key>ProgramArguments</key>
  <array>
    <string>{{xml .Exe}}</string>
    <string>sync</string>
    <string>--quiet</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>HOME</key>
    <string>{{xml .Home}}</string>
    <key>PATH</key>
    <string>{{xml .Path}}</string>
  </dict>
  <key>RunAtLoad</key>
  <true/>
  <key>StartInterval</key>
  <integer>3600</integer>
  <key>StandardOutPath</key>
  <string>{{xml .Log}}</string>
  <key>StandardErrorPath</key>
  <string>{{xml .Log}}</string>
</dict>
</plist>
`))

var serviceTmpl = template.Must(template.New("service").Funcs(funcs).Parse(`# managed by skenv: skenv autostart enable|disable
[Unit]
Description=skenv sync

[Service]
Type=oneshot
Environment={{sdq (printf "PATH=%s" .Path)}}
ExecStart={{sdq .Exe}} sync --quiet
StandardOutput=append:{{.Log}}
StandardError=append:{{.Log}}
`))

var timerTmpl = template.Must(template.New("timer").Parse(`# managed by skenv: skenv autostart enable|disable
[Unit]
Description=Run skenv sync hourly

[Timer]
OnStartupSec=1min
OnUnitActiveSec=1h
Persistent=true

[Install]
WantedBy=timers.target
`))
