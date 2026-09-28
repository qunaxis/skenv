package cli

import (
	"errors"
	"strings"
	"testing"
)

// jobPath is exercised at the package level (#56): the CLI has no seam to
// tell it whether git is on PATH, and covering that from a black-box test
// would depend on the machine running the tests. execLookPath is the seam
// production code already exposes.
func TestJobPath(t *testing.T) {
	orig := execLookPath
	t.Cleanup(func() { execLookPath = orig })
	const fixed = "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin"

	execLookPath = func(string) (string, error) { return "", errors.New("not found") }
	if got := jobPath(); got != fixed {
		t.Errorf("without git: %q, want %q", got, fixed)
	}

	execLookPath = func(string) (string, error) { return "/custom/bin/git", nil }
	if got, want := jobPath(), "/custom/bin:"+fixed; got != want {
		t.Errorf("with git: %q, want %q", got, want)
	}

	// git's own directory is already one of the fixed ones: no duplicate,
	// but it still comes first since add() records it before the fixed list.
	execLookPath = func(string) (string, error) { return "/usr/bin/git", nil }
	if got, want := jobPath(), "/usr/bin:/opt/homebrew/bin:/usr/local/bin:/bin"; got != want {
		t.Errorf("git alongside a fixed dir: %q, want %q", got, want)
	}
}

// `autostart status` never installs or loads a job: it only reads. Under
// the world's temporary $HOME the unit file never exists, so the report is
// "disabled" regardless of what launchctl/systemctl says about this label
// on the real machine (#56). `enable`/`disable` are not exercised here:
// runAutostart hardcodes autostart.ExecRunner (no seam to fake it), so
// driving them would install or unload a real LaunchAgent/systemd unit on
// whatever machine runs the tests; that is out of scope for a
// behavior-pinning change (see AGENTS.md/#56: no production code changes).
func TestAutostartStatusReadOnly(t *testing.T) {
	w := newWorld(t)
	code, out, _ := w.run("autostart", "status")
	if code != 1 || !strings.Contains(out, "autostart: disabled") {
		t.Errorf("autostart status: exit %d\n%s", code, out)
	}
}
