package engine

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qunaxis/skenv/internal/platform/gitx"
)

// checkManifestEngine opens a read-only UserScope on a manifest with the given
// text, for a package-level test of checkManifest (#56): the CLI never
// drives it into a parse error or a resolveMachine error (importUser only
// appends text it built itself, and the machine that opened successfully
// stays resolvable after an import only adds checkouts/dependencies), and a
// name collision with user.unmanaged is filtered out of import's own
// candidate list before it ever reaches checkManifest. Pinning the three
// error returns here keeps them from silently changing meaning once N1/N2
// move this code.
func checkManifestEngine(t *testing.T, manifestText string) *UserScope {
	t.Helper()
	home := t.TempDir()
	mp := filepath.Join(home, "skenv.toml")
	if err := os.WriteFile(mp, []byte(manifestText), 0o644); err != nil {
		t.Fatal(err)
	}
	env := Env{
		Home:     home,
		Getenv:   func(string) string { return "" },
		Hostname: "test-host",
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Git:      gitx.Git{},
		Now:      time.Now,
	}
	e, err := OpenUser(context.Background(), env, Options{Manifest: mp, ReadOnly: true})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(e.Close)
	return e
}

// checkManifest parses and validates the candidate text, restoring the
// engine's previous manifest on any error: a parse error, a manifest that
// no longer resolves on this machine, or a skill selection error.
func TestCheckManifestParseError(t *testing.T) {
	e := checkManifestEngine(t, "[user]\n")
	prev := e.manifest
	if err := e.checkManifest([]byte("not = [valid toml")); err == nil {
		t.Fatal("want a parse error")
	}
	if e.manifest != prev {
		t.Error("a parse error must not change the engine's manifest")
	}
}

func TestCheckManifestMachineError(t *testing.T) {
	e := checkManifestEngine(t, "[user]\n")
	prev := e.manifest
	e.env.Getenv = func(k string) string {
		if k == "SKENV_MACHINE" {
			return "office"
		}
		return ""
	}
	candidate := "[user]\n[user.machines.laptop]\n"
	if err := e.checkManifest([]byte(candidate)); err == nil || !strings.Contains(err.Error(), `machine "office"`) {
		t.Fatalf("want an unresolvable machine, got %v", err)
	}
	if e.manifest != prev {
		t.Error("a machine error must not change the engine's manifest")
	}
}

func TestCheckManifestSkillsError(t *testing.T) {
	e := checkManifestEngine(t, "[user]\n")
	prev := e.manifest
	// A dependency named after a user.unmanaged pattern: valid TOML, a
	// machine that resolves, but skills() rejects the selection.
	candidate := "[user]\nunmanaged = [\"foo\"]\n\n[user.dependencies.foo]\nrepo = \"ext/tools\"\nskill_dir = \"tools/foo\"\ncommit = \"" +
		strings.Repeat("0", 40) + "\"\n"
	if err := e.checkManifest([]byte(candidate)); err == nil || !strings.Contains(err.Error(), `"foo" matches user.unmanaged`) {
		t.Fatalf("want a selection error, got %v", err)
	}
	if e.manifest != prev {
		t.Error("a selection error must not change the engine's manifest")
	}
}

func TestCheckManifestOK(t *testing.T) {
	e := checkManifestEngine(t, "[user]\n")
	candidate := "[user]\n[user.dependencies.foo]\nrepo = \"ext/tools\"\nskill_dir = \"tools/foo\"\ncommit = \"" +
		strings.Repeat("0", 40) + "\"\n"
	if err := e.checkManifest([]byte(candidate)); err != nil {
		t.Fatalf("valid manifest: %v", err)
	}
	if len(e.manifest.DependencyList()) != 1 {
		t.Error("checkManifest did not adopt the valid candidate")
	}
}
