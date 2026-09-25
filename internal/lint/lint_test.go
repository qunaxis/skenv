package lint

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const goodSkill = `---
name: demo
description: Does demo things.
license: MIT
metadata:
  source: original
---
# Demo

See [the guide](references/guide.md#top), [site](https://example.com), [anchor](#x)
and ![diagram](assets/a%20b.png). Code ` + "`[not](a-link.md)`" + ` is ignored.

` + "```" + `
[also not](missing.md)
` + "```" + `

[ref]: references/guide.md
`

// skill writes a skill named demo with the given files (path → content;
// a path ending in "*" is made executable).
func skill(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "demo")
	base := map[string]string{
		"SKILL.md":            goodSkill,
		"references/guide.md": "# Guide\n\nBack to [skill](../SKILL.md).\n",
		"assets/a b.png":      "png",
	}
	for k, v := range files {
		base[k] = v
	}
	for p, content := range base {
		mode := os.FileMode(0o644)
		if strings.HasSuffix(p, "*") {
			p, mode = strings.TrimSuffix(p, "*"), 0o755
		}
		if content == "<delete>" {
			continue
		}
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func rules(fs []Finding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Rule)
	}
	return out
}

func fm(body string) string { return "---\n" + body + "---\n# x\n" }

func TestGoodSkill(t *testing.T) {
	if got := Skill(skill(t, nil)); len(got) != 0 {
		t.Fatalf("findings on a valid skill: %v", got)
	}
}

func TestRules(t *testing.T) {
	long := strings.Repeat("a", MaxDescriptionLen+1)
	cases := []struct {
		name  string
		files map[string]string
		want  []string // expected rules, in order
		msg   string
	}{
		// L1
		{"L1 no frontmatter", map[string]string{"SKILL.md": "# Demo\n"}, []string{"L1"}, "no YAML frontmatter"},
		{"L1 unterminated", map[string]string{"SKILL.md": "---\nname: demo\n"}, []string{"L1"}, "no YAML frontmatter"},
		{"L1 invalid yaml", map[string]string{"SKILL.md": fm("name: [demo\ndescription: x\n")}, []string{"L1"}, "not valid YAML"},
		{"L1 no name", map[string]string{"SKILL.md": fm("description: x\n")}, []string{"L1"}, "no name"},
		{"L1 no description", map[string]string{"SKILL.md": fm("name: demo\n")}, []string{"L1"}, "no description"},
		// L2
		{"L2 name differs from dir", map[string]string{"SKILL.md": fm("name: other\ndescription: x\n")}, []string{"L2"}, `must equal the directory name "demo"`},
		{"L2 bad characters", map[string]string{"SKILL.md": fm("name: Demo_1\ndescription: x\n")}, []string{"L2", "L2"}, "lowercase letters"},
		// L3
		{"L3 empty description", map[string]string{"SKILL.md": fm("name: demo\ndescription: \"  \"\n")}, []string{"L3"}, "description is empty"},
		{"L3 long description", map[string]string{"SKILL.md": fm("name: demo\ndescription: " + long + "\n")}, []string{"L3"}, "the limit is 1024"},
		{"L3 license type", map[string]string{"SKILL.md": fm("name: demo\ndescription: x\nlicense: [MIT]\n")}, []string{"L3"}, "license must be a string"},
		{"L3 metadata not a map", map[string]string{"SKILL.md": fm("name: demo\ndescription: x\nmetadata: book\n")}, []string{"L3"}, "map of strings"},
		{"L3 metadata value", map[string]string{"SKILL.md": fm("name: demo\ndescription: x\nmetadata:\n  version: 1\n")}, []string{"L3"}, "metadata.version must be a string"},
		// L4
		{"L4 missing file", map[string]string{"SKILL.md": fm("name: demo\ndescription: x\n") + "[x](scripts/run.py)\n"}, []string{"L4"}, "does not exist"},
		{"L4 in references", map[string]string{"references/guide.md": "[x](nope.md)\n"}, []string{"L4"}, "references/guide.md"},
		{"L4 outside skill", map[string]string{"SKILL.md": fm("name: demo\ndescription: x\n") + "[x](../other/SKILL.md)\n"}, []string{"L4"}, "outside the skill"},
		// L5
		{"L5 big file", map[string]string{"data.bin": strings.Repeat("x", MaxFileSize+1)}, []string{"L5"}, "the limit is 10 MB"},
		{"L5 .env", map[string]string{".env": "A=1\n"}, []string{"L5"}, "looks like a secret"},
		{"L5 key", map[string]string{"certs/server.key": "k"}, []string{"L5"}, "looks like a secret"},
		{"L5 pem", map[string]string{"server.pem": "k"}, []string{"L5"}, "looks like a secret"},
		{"L5 credentials", map[string]string{".credentials.json": "{}"}, []string{"L5"}, "looks like a secret"},
		// L6
		{"L6 no shebang", map[string]string{"scripts/run*": "echo hi\n"}, []string{"L6"}, "no shebang"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Skill(skill(t, c.files))
			if !reflect.DeepEqual(rules(got), c.want) {
				t.Fatalf("rules = %v, want %v: %v", rules(got), c.want, got)
			}
			if !strings.Contains(got[len(got)-1].String(), c.msg) {
				t.Errorf("message %q lacks %q", got[len(got)-1].String(), c.msg)
			}
		})
	}
}

// Positive cases for the rules that the good skill does not exercise.
func TestRulesPass(t *testing.T) {
	cases := map[string]map[string]string{
		"L1 CRLF and BOM":       {"SKILL.md": "\ufeff---\r\nname: demo\r\ndescription: x\r\n---\r\n"},
		"L3 max lengths":        {"SKILL.md": fm("name: demo\ndescription: " + strings.Repeat("я", MaxDescriptionLen) + "\n")},
		"L3 no license/meta":    {"SKILL.md": fm("name: demo\ndescription: x\n")},
		"L4 external and code":  {"SKILL.md": fm("name: demo\ndescription: x\n") + "[a](mailto:x@example.com) `[b](c.md)`\n"},
		"L5 just under 10 MB":   {"data.bin": strings.Repeat("x", MaxFileSize)},
		"L5 env example":        {".env.example": "A=\n", "keys.md": "about keys\n"},
		"L6 shebang":            {"scripts/run*": "#!/bin/sh\necho hi\n", "check*": "#!/usr/bin/env bash\n"},
		"L6 non-exec no bang":   {"scripts/lib.sh": "echo lib\n"},
		"skip .venv and nested": {".venv/bin/python*": "ELF", "node_modules/x/.env": "A=1"},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			if got := Skill(skill(t, files)); len(got) != 0 {
				t.Fatalf("unexpected findings: %v", got)
			}
		})
	}
}

func TestFindAndForFiles(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"skills/a/SKILL.md", "skills/b/SKILL.md", "skills/b/nested/SKILL.md", ".hidden/c/SKILL.md", "README.md"} {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Find(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(root, "skills/a"), filepath.Join(root, "skills/b")}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Find = %v", got)
	}
	if got, _ := Find(filepath.Join(root, "skills/a")); len(got) != 1 {
		t.Errorf("Find(skill) = %v", got)
	}
	files := ForFiles(root, []string{"skills/a/SKILL.md", "skills/a/refs/x.md", "skills/b/nested/deep/f", "README.md", "skills/gone/x"})
	want = []string{filepath.Join(root, "skills/a"), filepath.Join(root, "skills/b/nested")}
	if !reflect.DeepEqual(files, want) {
		t.Errorf("ForFiles = %v", files)
	}
}

// Inside git only what would be committed counts: ignored local files never
// fail lint, untracked but not ignored ones do.
func TestGitIgnoredFiles(t *testing.T) {
	dir := skill(t, map[string]string{
		".env":             "A=1\n",
		"cache/tool*":      "binary",
		"notes/secret.key": "k",
		"SKILL.md":         goodSkill + "\n[local](cache/tool)\n",
	})
	root := filepath.Dir(dir)
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "t@example.invalid"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git: %v %s", err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".env\ncache/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Skill(dir)
	if len(got) != 2 || got[0].Rule != "L4" || !strings.Contains(got[0].Msg, "ignored by git") || got[1].Path != "notes/secret.key" {
		t.Fatalf("findings = %v", got)
	}
}
