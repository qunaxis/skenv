package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qunaxis/skenv/internal/config"
	"github.com/qunaxis/skenv/internal/manifest"
)

func machineManifest(t *testing.T, machines string) *manifest.Manifest {
	t.Helper()
	m, err := manifest.ParseIn([]byte("[user.checkouts.skills]\nrepo = \"me/skills\"\ncheckout_dir = \"~/src/skills\"\n"+machines), ".toml", "/m")
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// The machine is the tool config `machine`, else $SKENV_MACHINE, else the
// hostname: the rules of the full hostname, else of the short one, never
// both merged.
func TestMachineOf(t *testing.T) {
	home := t.TempDir()
	vars := map[string]string{}
	env := Env{Home: home, Hostname: "laptop.example.net", Getenv: func(k string) string { return vars[k] }}
	both := machineManifest(t, "[user.machines.\"laptop.example.net\"]\nexclude = [\"a\"]\n[user.machines.laptop]\nexclude = [\"b\"]\n")
	mc, err := machineOf(env, both)
	if err != nil || mc.name != "laptop.example.net" || mc.from != "hostname" || !mc.has {
		t.Fatalf("full hostname: %+v %v", mc, err)
	}
	if !mc.rules.Selects("b") || mc.rules.Selects("a") {
		t.Errorf("the rules of the full and the short hostname were merged: %+v", mc.rules)
	}
	short := machineManifest(t, "[user.machines.laptop]\nexclude = [\"b\"]\n")
	if mc, err := machineOf(env, short); err != nil || mc.name != "laptop" || mc.from != "short hostname" || mc.rules.Selects("b") {
		t.Errorf("short hostname: %+v %v", mc, err)
	}
	none := machineManifest(t, "")
	if mc, err := machineOf(env, none); err != nil || mc.has || mc.name != "laptop.example.net" {
		t.Errorf("a hostname without rules is fine: %+v %v", mc, err)
	}

	// An explicit name must have an entry, which may be empty.
	vars["SKENV_MACHINE"] = "work"
	if _, err := machineOf(env, short); err == nil || !strings.Contains(err.Error(), `machine "work" ($SKENV_MACHINE) has no user.machines.work entry`) ||
		!strings.Contains(err.Error(), "known: laptop") {
		t.Errorf("unknown explicit machine: %v", err)
	}
	withWork := machineManifest(t, "[user.machines.work]\n[user.machines.laptop]\nexclude = [\"b\"]\n")
	if mc, err := machineOf(env, withWork); err != nil || mc.name != "work" || !mc.has || !mc.rules.Selects("b") {
		t.Errorf("explicit machine: %+v %v", mc, err)
	}
	delete(vars, "SKENV_MACHINE")
	if _, err := config.Set(home, "machine", "work"); err != nil {
		t.Fatal(err)
	}
	if mc, err := machineOf(env, withWork); err != nil || mc.from != "tool config `machine`" {
		t.Errorf("machine from the tool config: %+v %v", mc, err)
	}
	if _, err := machineOf(env, none); err == nil || !strings.Contains(err.Error(), "known: none") {
		t.Errorf("explicit machine, manifest without machines: %v", err)
	}
}

// checkout_dir resolves against the directory of the skenv file, and a
// machine may move a checkout.
func TestCheckoutPathOf(t *testing.T) {
	home := t.TempDir()
	env := Env{Home: home, Hostname: "h", Getenv: func(string) string { return "" }}
	m, err := manifest.ParseIn([]byte(`[user.checkouts.here]
repo = "me/a"
checkout_dir = "."
[user.checkouts.sibling]
repo = "me/b"
checkout_dir = "../b"
[user.checkouts.moved]
repo = "me/c"
checkout_dir = "~/src/c"
[user.machines.h.checkout_dirs]
moved = "~/work/c"
`), ".toml", "/m/skills")
	if err != nil {
		t.Fatal(err)
	}
	mc, err := machineOf(env, m)
	if err != nil {
		t.Fatal(err)
	}
	pathOf := checkoutPathOf(env, m, mc)
	for id, want := range map[string]string{"here": "/m/skills", "sibling": "/m/b", "moved": filepath.Join(home, "work/c")} {
		c := m.Checkouts[id]
		if got := pathOf(&c); got != want {
			t.Errorf("%s: %s, want %s", id, got, want)
		}
	}
	// The working directory plays no part.
	t.Chdir(os.TempDir())
	if c := m.Checkouts["sibling"]; pathOf(&c) != "/m/b" {
		t.Errorf("the path depends on the working directory")
	}
}
