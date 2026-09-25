package fileformat

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestChoose(t *testing.T) {
	dir := "/r"
	for _, c := range []struct {
		existing, format, want, err string
	}{
		{"", "", "/r/skenv.toml", ""},
		{"", "toml", "/r/skenv.toml", ""},
		{"", "yaml", "/r/skenv.yaml", ""},
		{"", "json", "/r/skenv.json", ""},
		{"", "yml", "", "--format must be toml, yaml, json"},
		{"", "TOML", "", "--format must be"},
		{"/r/skenv.json", "", "/r/skenv.json", ""},
		{"/r/skenv.yml", "", "/r/skenv.yml", ""},
		{"/r/skenv.yml", "yaml", "/r/skenv.yml", ""},
		{"/r/skenv.toml", "toml", "/r/skenv.toml", ""},
		{"/r/skenv.toml", "yaml", "", "/r/skenv.toml exists and is TOML; --format yaml does not convert it"},
		{"/r/skenv.yaml", "json", "", "is YAML; --format json"},
	} {
		got, err := Choose(dir, "skenv", c.existing, c.format)
		switch {
		case c.err != "" && (err == nil || !strings.Contains(err.Error(), c.err)):
			t.Errorf("Choose(%q, %q): err = %v, want %q", c.existing, c.format, err, c.err)
		case c.err == "" && (err != nil || got != filepath.FromSlash(c.want)):
			t.Errorf("Choose(%q, %q) = %q, %v; want %q", c.existing, c.format, got, err, c.want)
		}
	}
}

func TestOf(t *testing.T) {
	for p, want := range map[string]string{"a.toml": "toml", "a.yml": "yaml", "a.yaml": "yaml", "a.json": "json", "a.ini": ""} {
		if got := Of(p); got != want {
			t.Errorf("Of(%q) = %q, want %q", p, got, want)
		}
	}
}
