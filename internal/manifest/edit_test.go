package manifest

import (
	"strings"
	"testing"
)

const base = `# my skills
[[environment.own]]
repo = "me/skills"   # own
path = "~/skills"

[[environment.vendor]]
name = "a"
repo = "x/a"
path = "."
rev  = "` + sha + `" # pinned on purpose

# the host table
[environment.host."mbp"]
skip = []
`

func TestAppendVendor(t *testing.T) {
	out, err := AppendVendor([]byte(base), ".toml", Vendor{Name: "b", Repo: "x/b", Path: "skills/b", Rev: sha})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.HasPrefix(s, base) {
		t.Fatal("existing text must be kept verbatim")
	}
	if !strings.HasSuffix(s, "\n[[environment.vendor]]\nname = \"b\"\nrepo = \"x/b\"\npath = \"skills/b\"\nrev  = \""+sha+"\"\n") {
		t.Errorf("appended:\n%s", s)
	}
	m, _ := Parse(out, ".toml")
	if len(m.Vendor) != 2 || len(m.Host) != 1 {
		t.Error("appended table breaks the structure")
	}
	if _, err := AppendVendor([]byte(base), ".toml", Vendor{Name: "a", Repo: "x/a", Path: ".", Rev: sha}); err == nil {
		t.Error("duplicate append must fail validation")
	}
}

func TestSetVendorRev(t *testing.T) {
	next := strings.Repeat("b", 40)
	out, err := SetVendorRev([]byte(base), ".toml", "a", next)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(base, sha, next, 1)
	if string(out) != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
	if _, err := SetVendorRev([]byte(base), ".toml", "missing", next); err == nil {
		t.Error("unknown vendor must fail")
	}
}

func TestRemoveVendor(t *testing.T) {
	with, err := AppendVendor([]byte(base), ".toml", Vendor{Name: "b", Repo: "x/b", Path: ".", Rev: sha})
	if err != nil {
		t.Fatal(err)
	}
	out, err := RemoveVendor(with, ".toml", "a")
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, keep := range []string{"# my skills", `repo = "me/skills"   # own`, "# the host table", `name = "b"`} {
		if !strings.Contains(s, keep) {
			t.Errorf("lost %q:\n%s", keep, s)
		}
	}
	if strings.Contains(s, `name = "a"`) || strings.Contains(s, "pinned on purpose") {
		t.Errorf("vendor a not removed:\n%s", s)
	}
	out, err = RemoveVendor(out, ".toml", "b")
	if err != nil {
		t.Fatal(err)
	}
	if m, _ := Parse(out, ".toml"); len(m.Vendor) != 0 {
		t.Error("vendor b not removed")
	}
}

func TestQuote(t *testing.T) {
	if got := quote("a\"b\\c"); got != `"a\"b\\c"` {
		t.Errorf("quote = %s", got)
	}
}

// YAML and JSON manifests are edited through their data.
func TestEditOtherFormats(t *testing.T) {
	for ext, text := range map[string]string{
		".yaml": "environment:\n  own:\n    - repo: me/skills\n      path: ~/skills\n  vendor:\n    - name: a\n      repo: x/a\n      rev: " + sha + "\n",
		".json": `{"environment": {"own": [{"repo": "me/skills", "path": "~/skills"}], "vendor": [{"name": "a", "repo": "x/a", "rev": "` + sha + `"}]}}`,
	} {
		t.Run(ext, func(t *testing.T) {
			next := strings.Repeat("b", 40)
			out, err := AppendVendor([]byte(text), ext, Vendor{Name: "b", Repo: "x/b", Path: ".", Rev: sha})
			if err != nil {
				t.Fatal(err)
			}
			if out, err = SetVendorRev(out, ext, "a", next); err != nil {
				t.Fatal(err)
			}
			if out, err = RemoveVendor(out, ext, "b"); err != nil {
				t.Fatal(err)
			}
			m, err := Parse(out, ext)
			if err != nil {
				t.Fatal(err)
			}
			if len(m.Vendor) != 1 || m.Vendor[0].Rev != next || len(m.Own) != 1 {
				t.Errorf("result:\n%s", out)
			}
			if _, err := SetVendorRev(out, ext, "missing", next); err == nil {
				t.Error("unknown vendor must fail")
			}
		})
	}
}

func TestParseNeedsEnvironment(t *testing.T) {
	if _, err := Parse([]byte("[repo]\nharness = \"0.4.0\"\nvisibility = \"public\"\n"), ".toml"); err == nil || !strings.Contains(err.Error(), "no [environment] section") {
		t.Errorf("err = %v", err)
	}
}
