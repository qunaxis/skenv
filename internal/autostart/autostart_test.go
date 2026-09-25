package autostart

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeRunner struct {
	calls  []string
	loaded bool
}

func (f *fakeRunner) run(_ context.Context, name string, args ...string) (string, error) {
	call := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, call)
	switch {
	case strings.Contains(call, "bootstrap"), strings.Contains(call, "enable --now"):
		f.loaded = true
	case strings.Contains(call, "bootout"), strings.Contains(call, "disable --now"):
		f.loaded = false
	case strings.Contains(call, "print"), strings.Contains(call, "is-active"):
		if !f.loaded {
			return "inactive", errors.New("not loaded")
		}
		return "active\n", nil
	}
	return "", nil
}

func config(t *testing.T, goos string, f *fakeRunner) Config {
	home := t.TempDir()
	return Config{GOOS: goos, Home: home, Exe: "/usr/local/bin/skenv", Log: filepath.Join(home, ".local/state/skenv/autostart.log"),
		Path: "/usr/bin:/bin", UID: 501, Run: f.run}
}

func TestDarwin(t *testing.T) {
	f := &fakeRunner{}
	c := config(t, "darwin", f)
	ctx := context.Background()
	for range 2 { // idempotent
		if err := c.Enable(ctx); err != nil {
			t.Fatal(err)
		}
	}
	plist, err := os.ReadFile(filepath.Join(c.Home, "Library/LaunchAgents/com.qunaxis.skenv.plist"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<string>com.qunaxis.skenv</string>", "<key>RunAtLoad</key>\n  <true/>",
		"<key>StartInterval</key>\n  <integer>3600</integer>", "<string>/usr/local/bin/skenv</string>\n    <string>sync</string>\n    <string>--quiet</string>",
		c.Log} {
		if !strings.Contains(string(plist), want) {
			t.Errorf("plist lacks %q", want)
		}
	}
	if f.calls[len(f.calls)-1] != "launchctl bootstrap gui/501 "+filepath.Join(c.Home, "Library/LaunchAgents/com.qunaxis.skenv.plist") {
		t.Errorf("calls = %v", f.calls)
	}
	st, _ := c.Status(ctx)
	if !st.Installed || !st.Loaded || !st.Current {
		t.Errorf("status = %+v", st)
	}
	if err := c.Disable(ctx); err != nil {
		t.Fatal(err)
	}
	if st, _ := c.Status(ctx); st.Installed || st.Loaded || st.Detail != "disabled" {
		t.Errorf("status after disable = %+v", st)
	}
}

func TestLinux(t *testing.T) {
	f := &fakeRunner{}
	c := config(t, "linux", f)
	ctx := context.Background()
	if err := c.Enable(ctx); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(c.Home, ".config/systemd/user")
	svc, _ := os.ReadFile(filepath.Join(dir, "skenv.service"))
	tmr, _ := os.ReadFile(filepath.Join(dir, "skenv.timer"))
	if !strings.Contains(string(svc), `ExecStart="/usr/local/bin/skenv" sync --quiet`) || !strings.Contains(string(svc), "append:"+c.Log) {
		t.Errorf("service:\n%s", svc)
	}
	if !strings.Contains(string(tmr), "OnUnitActiveSec=1h") {
		t.Errorf("timer:\n%s", tmr)
	}
	if st, _ := c.Status(ctx); !st.Loaded {
		t.Errorf("status = %+v", st)
	}
	if err := c.Disable(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "skenv.timer")); !os.IsNotExist(err) {
		t.Error("timer not removed")
	}
}

func TestUnsupported(t *testing.T) {
	if err := config(t, "windows", &fakeRunner{}).Enable(context.Background()); err == nil {
		t.Error("windows must be rejected")
	}
}
