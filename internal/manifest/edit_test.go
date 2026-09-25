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
	out, err := AppendVendor([]byte(base), ".toml", "environment", Vendor{Name: "b", Repo: "x/b", Path: "skills/b", Rev: sha})
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
	if _, err := AppendVendor([]byte(base), ".toml", "environment", Vendor{Name: "a", Repo: "x/a", Path: ".", Rev: sha}); err == nil {
		t.Error("duplicate append must fail validation")
	}
}

func TestSetVendorRev(t *testing.T) {
	next := strings.Repeat("b", 40)
	out, err := SetVendorRev([]byte(base), ".toml", "environment", "a", next)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(base, sha, next, 1)
	if string(out) != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
	if _, err := SetVendorRev([]byte(base), ".toml", "environment", "missing", next); err == nil {
		t.Error("unknown vendor must fail")
	}
}

func TestRemoveVendor(t *testing.T) {
	with, err := AppendVendor([]byte(base), ".toml", "environment", Vendor{Name: "b", Repo: "x/b", Path: ".", Rev: sha})
	if err != nil {
		t.Fatal(err)
	}
	out, err := RemoveVendor(with, ".toml", "environment", "a")
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
	out, err = RemoveVendor(out, ".toml", "environment", "b")
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
			out, err := AppendVendor([]byte(text), ext, "environment", Vendor{Name: "b", Repo: "x/b", Path: ".", Rev: sha})
			if err != nil {
				t.Fatal(err)
			}
			if out, err = SetVendorRev(out, ext, "environment", "a", next); err != nil {
				t.Fatal(err)
			}
			if out, err = RemoveVendor(out, ext, "environment", "b"); err != nil {
				t.Fatal(err)
			}
			m, err := Parse(out, ext)
			if err != nil {
				t.Fatal(err)
			}
			if len(m.Vendor) != 1 || m.Vendor[0].Rev != next || len(m.Own) != 1 {
				t.Errorf("result:\n%s", out)
			}
			if _, err := SetVendorRev(out, ext, "environment", "missing", next); err == nil {
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

// A vendor table header may carry a comment and spaces.
func TestVendorHeaderWithComment(t *testing.T) {
	text := "[environment.layout]\nstore = \"~/s\"\n\n[[ environment . vendor ]]  # pinned\nname = \"b\"\nrepo = \"x/b\"\nrev  = \"" + sha + "\"\n"
	out, err := RemoveVendor([]byte(text), ".toml", "environment", "b")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), `name = "b"`) {
		t.Errorf("not removed:\n%s", out)
	}
}

func TestAppendOwn(t *testing.T) {
	out, err := AppendOwn([]byte(base), ".toml", Own{Repo: "me/more", Path: "~/src/more", SkillsDir: "agent/skills", Skills: []string{"a2", "b2"}})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.HasPrefix(s, base) || !strings.HasSuffix(s, "\n[[environment.own]]\nrepo = \"me/more\"\npath = \"~/src/more\"\nskills_dir = \"agent/skills\"\nskills = [\"a2\", \"b2\"]\n") {
		t.Errorf("appended:\n%s", s)
	}
	m, _ := Parse(out, ".toml")
	if len(m.Own) != 2 || len(m.Vendor) != 1 || len(m.Host) != 1 || m.Own[1].SkillsDir != "agent/skills" {
		t.Errorf("appended table breaks the structure: %+v", m)
	}
	// The default skills_dir and no selection write only repo and path.
	for _, ext := range []string{".toml", ".yaml", ".json"} {
		data := map[string]string{".toml": base, ".yaml": "# c\nenvironment:\n  own: []\n", ".json": `{"environment": {}}`}[ext]
		out, err := AppendOwn([]byte(data), ext, Own{Repo: "me/x", Path: "~/x", SkillsDir: DefaultSkillsDir})
		if err != nil {
			t.Fatalf("%s: %v", ext, err)
		}
		m, err := Parse(out, ext)
		if err != nil {
			t.Fatalf("%s: %v", ext, err)
		}
		last := m.Own[len(m.Own)-1]
		if last.Repo != "me/x" || last.Skills != nil || strings.Contains(string(out), "skills_dir") {
			t.Errorf("%s:\n%s", ext, out)
		}
	}
}

// Commented keys right under a table belong to it: a new vendor table goes
// after them, so uncommenting them later keeps them in their own table.
func TestAppendVendorKeepsTrailingComments(t *testing.T) {
	out, err := AddEnvironment(nil, ".toml", &Own{Repo: "me/skills", Path: "~/skills"})
	if err != nil {
		t.Fatal(err)
	}
	if out, err = AppendVendor(out, ".toml", "environment", Vendor{Name: "b", Repo: "x/b", Path: "skills/b", Rev: sha}); err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if i, j := strings.Index(s, "# exclude = "), strings.Index(s, "[[environment.vendor]]"); i < 0 || j < i {
		t.Fatalf("the vendor table splits the own table from its commented keys:\n%s", s)
	}
	uncommented := strings.Replace(s, `# exclude = ["experimental-*"]`, `exclude = ["experimental-*"]`, 1)
	m, err := Parse([]byte(uncommented), ".toml")
	if err != nil {
		t.Fatalf("uncommented exclude: %v\n%s", err, uncommented)
	}
	if len(m.Own) != 1 || len(m.Own[0].Exclude) != 1 || len(m.Vendor) != 1 {
		t.Errorf("parsed: %+v", m)
	}
}
