package schemas

import (
	"strings"
	"testing"
)

func TestURL(t *testing.T) {
	cases := map[string]string{
		"v0.4.0":              Base + "v0.4.0/skenv.schema.json",
		"0.4.0":               Base + "v0.4.0/skenv.schema.json",
		"0.5.0-rc.1":          Base + "v0.5.0-rc.1/skenv.schema.json",
		"0.0.0-dev+abcdef012": Base + "skenv.schema.json",
		"":                    Base + "skenv.schema.json",
		"(devel)":             Base + "skenv.schema.json",
	}
	for v, want := range cases {
		if got := URL(Skenv, v); got != want {
			t.Errorf("URL(%q) = %s, want %s", v, got, want)
		}
		name, version, ok := ParseURL(want)
		if !ok || name != Skenv || version != Release(v) {
			t.Errorf("ParseURL(%s) = %q, %q, %v", want, name, version, ok)
		}
	}
	for _, u := range []string{"https://example.org/skenv.schema.json", Base + "latest/skenv.schema.json", Base + "vX/skenv.schema.json", Base + "skenv.schema.json?x=1", Base + "v0.4.0/a/skenv.schema.json"} {
		if _, _, ok := ParseURL(u); ok {
			t.Errorf("ParseURL(%s) must fail", u)
		}
	}
}

func TestStampAndCheck(t *testing.T) {
	old := URL(Skenv, "0.3.0")
	cases := []struct {
		name, in string
		add      bool
		want     string
	}{
		{"added", "[repo]\n", true, "#:schema " + URL(Skenv, "0.4.0") + "\n[repo]\n"},
		{"not added", "[repo]\n", false, "[repo]\n"},
		{"moved", "#:schema " + old + "\n[repo]\n", false, "#:schema " + URL(Skenv, "0.4.0") + "\n[repo]\n"},
		{"foreign kept", "#:schema ./my.json\n[repo]\n", true, "#:schema ./my.json\n[repo]\n"},
	}
	for _, c := range cases {
		out, err := Stamp([]byte(c.in), ".toml", Skenv, "0.4.0", c.add)
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != c.want {
			t.Errorf("%s: got %q, want %q", c.name, out, c.want)
		}
	}
	if msg := Check([]byte("[repo]\n"), ".toml", Skenv, "0.4.0"); !strings.Contains(msg, "no schema directive") {
		t.Errorf("missing: %q", msg)
	}
	if msg := Check([]byte("#:schema "+old+"\n"), ".toml", Skenv, "0.4.0"); !strings.Contains(msg, "expected "+URL(Skenv, "0.4.0")) {
		t.Errorf("outdated: %q", msg)
	}
	if msg := Check([]byte("#:schema "+URL(Skenv, "0.4.0")+"\n"), ".toml", Skenv, "0.4.0"); msg != "" {
		t.Errorf("current: %q", msg)
	}
	if msg := Check([]byte("#:schema ./my.json\n"), ".toml", Skenv, "0.4.0"); msg != "" {
		t.Errorf("foreign: %q", msg)
	}
}
