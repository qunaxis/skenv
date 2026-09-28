package skenvfile

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestNoRetiredTerms keeps README.md, docs/**/*.md (except docs/commands/
// and docs/adr/, which describe every release including old ones),
// AGENTS.md and skills/skenv/** free of the skenv-file keys retired
// before 0.6 (LegacyKeys) and of the prose terms retired since
// (docs/retired-terms.txt). A rename or a retired term must update every
// mention in the same PR; wrap a deliberate historical mention (a
// migration section, for example) in a <!-- docs-check: legacy -->
// ... <!-- /docs-check --> block to exempt it.
func TestNoRetiredTerms(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range scanRetiredTerms(t, root) {
		t.Error(v)
	}
}

// TestNoRetiredTermsDetection exercises the scan itself against a
// throwaway tree, independent of the current state of docs/, so a change
// to the matching rules is caught here even if no real doc happens to
// trip it.
func TestNoRetiredTermsDetection(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "docs/retired-terms.txt", "# comment\nharness\n")
	writeFile(t, root, "README.md", strings.Join([]string{
		"# skenv",
		"",
		"Bracket header of a retired table:",
		"",
		"[environment]",
		"",
		"Retired dotted key in code: `environment.vendor`.",
		"",
		"The retired prose term Harness appears here.",
		"",
		"`project.from` still names the current project.from.<id> family.",
		"",
		"<!-- docs-check: legacy -->",
		"[environment]",
		"the harness term too",
		"<!-- /docs-check -->",
		"",
		"After the exempt block, clean again.",
	}, "\n"))
	writeFile(t, root, "AGENTS.md", "no retired mentions here\n")

	got := scanRetiredTerms(t, root)
	want := []string{
		"README.md:5: retired skenv-file key \"environment\" (see internal/skenvfile.LegacyKeys)",
		"README.md:7: retired skenv-file key \"environment.vendor\" (see internal/skenvfile.LegacyKeys)",
		"README.md:9: retired term \"harness\" (see docs/retired-terms.txt)",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("violations =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// bracketOnly holds retired keys whose dotted-key form (unlike its bracket
// table-header form) is still valid prose in the 0.6 format: only the old
// array-of-tables syntax ([[project.from]]) is retired, while the dotted
// prefix "project.from" still names the project.from.<id> family in the
// current format (see docs/skenv-file.md's "Formats and editing" section).
var bracketOnly = map[string]bool{"project.from": true}

// scanRetiredTerms scans the files TestNoRetiredTerms covers under root
// for the skenv-file keys retired before 0.6 (LegacyKeys) and the prose
// terms retired since (docs/retired-terms.txt), skipping any
// <!-- docs-check: legacy --> ... <!-- /docs-check --> block. It returns
// one "path:line: message" string per mention found, root-relative paths
// with forward slashes.
func scanRetiredTerms(t *testing.T, root string) []string {
	t.Helper()

	keys := LegacyKeys()
	terms := retiredTerms(t, root)
	termRE := make(map[string]*regexp.Regexp, len(terms))
	for _, term := range terms {
		termRE[term] = regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(term) + `\b`)
	}

	var violations []string
	for _, path := range retiredCheckFiles(t, root) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatal(err)
		}
		rel = filepath.ToSlash(rel)

		exempt := false
		for i, line := range strings.Split(string(data), "\n") {
			switch strings.TrimSpace(line) {
			case "<!-- docs-check: legacy -->":
				exempt = true
				continue
			case "<!-- /docs-check -->":
				exempt = false
				continue
			}
			if exempt {
				continue
			}
			lineNo := i + 1
			trimmed := strings.TrimSpace(line)
			for _, k := range keys {
				switch {
				case trimmed == "["+k+"]", trimmed == "[["+k+"]]":
					violations = append(violations, fmtViolation(rel, lineNo, "retired skenv-file key %q (see internal/skenvfile.LegacyKeys)", k))
				case strings.Contains(k, ".") && !bracketOnly[k] && strings.Contains(line, "`"+k+"`"):
					violations = append(violations, fmtViolation(rel, lineNo, "retired skenv-file key %q (see internal/skenvfile.LegacyKeys)", k))
				}
			}
			for _, term := range terms {
				if termRE[term].MatchString(line) {
					violations = append(violations, fmtViolation(rel, lineNo, "retired term %q (see docs/retired-terms.txt)", term))
				}
			}
		}
	}
	return violations
}

func fmtViolation(rel string, line int, format string, args ...any) string {
	return fmt.Sprintf("%s:%d: %s", rel, line, fmt.Sprintf(format, args...))
}

// retiredTerms reads docs/retired-terms.txt: one term per line, blank
// lines and lines starting with # ignored.
func retiredTerms(t *testing.T, root string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "docs", "retired-terms.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var terms []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		terms = append(terms, line)
	}
	return terms
}

// retiredCheckFiles lists the files TestNoRetiredTerms scans.
func retiredCheckFiles(t *testing.T, root string) []string {
	t.Helper()
	files := []string{
		filepath.Join(root, "README.md"),
		filepath.Join(root, "AGENTS.md"),
	}

	docsRoot := filepath.Join(root, "docs")
	skip := map[string]bool{
		filepath.Join(docsRoot, "commands"): true,
		filepath.Join(docsRoot, "adr"):      true,
	}
	if err := filepath.WalkDir(docsRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			if skip[path] {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".md") {
			files = append(files, path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	skillRoot := filepath.Join(root, "skills", "skenv")
	if err := filepath.WalkDir(skillRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !d.IsDir() {
			files = append(files, path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	return files
}
