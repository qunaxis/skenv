package manifest

import (
	"strings"
	"testing"
)

const base = `# my skills
[[own]]
repo = "me/skills"   # own
path = "~/skills"

[[vendor]]
name = "a"
repo = "x/a"
path = "."
rev  = "` + sha + `" # pinned on purpose

# the host table
[host."mbp"]
skip = []
`

func TestAppendVendor(t *testing.T) {
	out, err := AppendVendor([]byte(base), Vendor{Name: "b", Repo: "x/b", Path: "skills/b", Rev: sha})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.HasPrefix(s, base) {
		t.Fatal("existing text must be kept verbatim")
	}
	if !strings.HasSuffix(s, "\n[[vendor]]\nname = \"b\"\nrepo = \"x/b\"\npath = \"skills/b\"\nrev  = \""+sha+"\"\n") {
		t.Errorf("appended:\n%s", s)
	}
	m, _ := Parse(out)
	if len(m.Vendor) != 2 || len(m.Host) != 1 {
		t.Error("appended table breaks the structure")
	}
	if _, err := AppendVendor([]byte(base), Vendor{Name: "a", Repo: "x/a", Path: ".", Rev: sha}); err == nil {
		t.Error("duplicate append must fail validation")
	}
}

func TestSetVendorRev(t *testing.T) {
	next := strings.Repeat("b", 40)
	out, err := SetVendorRev([]byte(base), "a", next)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(base, sha, next, 1)
	if string(out) != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
	if _, err := SetVendorRev([]byte(base), "missing", next); err == nil {
		t.Error("unknown vendor must fail")
	}
}

func TestRemoveVendor(t *testing.T) {
	with, err := AppendVendor([]byte(base), Vendor{Name: "b", Repo: "x/b", Path: ".", Rev: sha})
	if err != nil {
		t.Fatal(err)
	}
	out, err := RemoveVendor(with, "a")
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
	out, err = RemoveVendor(out, "b")
	if err != nil {
		t.Fatal(err)
	}
	if m, _ := Parse(out); len(m.Vendor) != 0 {
		t.Error("vendor b not removed")
	}
}

func TestQuote(t *testing.T) {
	if got := quote("a\"b\\c"); got != `"a\"b\\c"` {
		t.Errorf("quote = %s", got)
	}
}
