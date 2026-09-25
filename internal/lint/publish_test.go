package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func denylist(t *testing.T, content string) *Denylist {
	t.Helper()
	p := filepath.Join(t.TempDir(), "denylist.txt")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := LoadDenylist("/nonexistent", func(k string) string {
		if k == "SKENV_DENYLIST" {
			return p
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func publishable(extra string) string {
	return "---\nname: demo\ndescription: Demo.\nlicense: MIT\nmetadata:\n  source: original\n" + extra + "---\n# Demo\n"
}

// Acceptance (PRD v0.3): lint --publish rejects metadata.source book and a
// missing license (here) and a stop-list phrase (TestScanDenylist); each has
// a passing counterpart.
func TestPublish(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  string // "" = must pass
	}{
		{"ok", map[string]string{"SKILL.md": publishable("")}, ""},
		{"source book", map[string]string{"SKILL.md": strings.Replace(publishable(""), "source: original", "source: book", 1)}, `metadata.source is "book"`},
		{"source internal", map[string]string{"SKILL.md": strings.Replace(publishable(""), "source: original", "source: Internal", 1)}, `metadata.source is "Internal"`},
		{"source third-party-copy", map[string]string{"SKILL.md": strings.Replace(publishable(""), "source: original", "source: third-party-copy", 1)}, "third-party-copy"},
		{"no license", map[string]string{"SKILL.md": strings.Replace(publishable(""), "license: MIT\n", "", 1)}, "no license"},
		{"LICENSE file", map[string]string{"SKILL.md": strings.Replace(publishable(""), "license: MIT\n", "", 1), "LICENSE": "MIT License\n"}, ""},
		{"empty LICENSE file", map[string]string{"SKILL.md": strings.Replace(publishable(""), "license: MIT\n", "", 1), "LICENSE": ""}, "no license"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Publish(skill(t, mergeDelete(c.files)))
			if c.want == "" {
				if len(got) != 0 {
					t.Fatalf("unexpected findings: %v", got)
				}
				return
			}
			if len(got) != 1 || got[0].Rule != "P1" || !strings.Contains(got[0].String(), c.want) {
				t.Fatalf("findings = %v, want one containing %q", got, c.want)
			}
		})
	}
}

func TestScanDenylist(t *testing.T) {
	deny := denylist(t, "# internal names\nProject Falcon\n\nacme-corp\n")
	root := t.TempDir()
	files := map[string]string{
		"skills/a/SKILL.md":           "---\nname: a\n---\nBuilt for project falcon.\n",
		"skills/a/references/wrap.md": "Written for Project\n  Falcon, wrapped.\n",
		"skills/a/references/nbsp.md": "x\nACME-CORP\u00a0inside and project\u00a0\u00a0falcon\n",
		"README.md":                   "top-level ACME-corp mention\n",
		"skills/acme-corp-kit/x.bin":  "\x00binary acme-corp",
		"skills/a/clean.md":           "project falconry is different? no: 'project falcon' is a substring\n",
		"skills/a/ok.md":              "nothing here\n",
	}
	var list []string
	for p, c := range files {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
		list = append(list, p)
	}
	got := map[string]bool{}
	for _, f := range ScanDenylist(root, list, deny) {
		s := f.String()
		if strings.Contains(strings.ToLower(s), "falcon") || strings.Contains(strings.ToLower(s), "acme") {
			t.Errorf("finding leaks the stop-list: %s", s)
		}
		got[strings.TrimPrefix(s, root+"/")] = true
	}
	for _, want := range []string{
		"skills/a/SKILL.md: P1: line 4 matches stop-list entry at line 2",
		"skills/a/references/wrap.md: P1: line 1 matches stop-list entry at line 2",
		"skills/a/references/nbsp.md: P1: line 2 matches stop-list entry at line 2",
		"skills/a/references/nbsp.md: P1: line 2 matches stop-list entry at line 4",
		"README.md: P1: line 1 matches stop-list entry at line 4",
		"skills/[redacted]/x.bin: P1: file path matches stop-list entry at line 4",
		"skills/a/clean.md: P1: line 1 matches stop-list entry at line 2",
	} {
		found := false
		for g := range got {
			if strings.HasPrefix(g, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("missing %q in %v", want, got)
		}
	}
	for g := range got {
		if strings.HasPrefix(g, "skills/a/ok.md") || strings.Contains(g, "x.bin: P1: line") {
			t.Errorf("unexpected %s", g)
		}
	}
}

func TestLoadDenylist(t *testing.T) {
	home := t.TempDir()
	none := func(string) string { return "" }
	if _, err := LoadDenylist(home, none); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("missing stop-list: %v", err)
	}
	p := filepath.Join(home, ".config", "skenv", "denylist.txt")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("# only comments\n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDenylist(home, none); err == nil || !strings.Contains(err.Error(), "no entries") {
		t.Errorf("empty stop-list: %v", err)
	}
	if err := os.WriteFile(p, []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := LoadDenylist(home, none)
	if err != nil || d.Len() != 2 {
		t.Errorf("default location: %v %v", d, err)
	}
}

func TestSkillOf(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := filepath.Join(root, "skills", "demo")
	if err := os.MkdirAll(filepath.Join(s, "references"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s, "SKILL.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, ok := SkillOf(filepath.Join(s, "references", "new.md")); !ok || got != s {
		t.Errorf("SkillOf = %q %v", got, ok)
	}
	if _, ok := SkillOf(filepath.Join(root, "README.md")); ok {
		t.Error("file outside skills matched")
	}
}

// mergeDelete drops the default guide and asset of skill() so each case
// controls its files.
func mergeDelete(files map[string]string) map[string]string {
	out := map[string]string{"references/guide.md": "<delete>", "assets/a b.png": "<delete>"}
	for k, v := range files {
		out[k] = v
	}
	return out
}
