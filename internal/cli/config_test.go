package cli

import (
	"strings"
	"testing"
)

// An existing config.yaml is read and updated by init in place; a second
// config file is an error.
func TestInitKeepsYAMLConfig(t *testing.T) {
	w := newWorld(t)
	w.standard("")
	writeFile(t, w.path(".config/skenv/config.yaml"), "# mine\nother: 1\n")
	w.mustRun(0, "init", "me/skills", "--path", "~/"+ownPath)
	if w.exists(".config/skenv/config.toml") {
		t.Fatal("init wrote config.toml next to config.yaml")
	}
	cfg := readFile(t, w.path(".config/skenv/config.yaml"))
	if !strings.Contains(cfg, "manifest: ~/"+ownPath+"/env.toml") || !strings.Contains(cfg, "other: 1") {
		t.Fatalf("config.yaml:\n%s", cfg)
	}
	w.mustRun(0, "doctor")
	writeFile(t, w.path(".config/skenv/config.json"), "{}")
	_, errOut := w.mustRun(2, "doctor")
	if !strings.Contains(errOut, "several config files: ~/.config/skenv/config.yaml, ~/.config/skenv/config.json; keep one") {
		t.Errorf("stderr = %q", errOut)
	}
}
