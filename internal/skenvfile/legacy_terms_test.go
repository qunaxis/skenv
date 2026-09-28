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
// trip it. It covers, in order: a bare TOML table header, a dotted key in
// inline code, a prose term, the dotted form of a bracketOnly key (not
// flagged), a table header written as inline code (not just a bare
// line), a [[double-bracket]] header inside a Markdown table row, the
// single-bracket form of a bracketOnly key (not flagged, unlike its
// double-bracket form), a persistent <!-- docs-check --> block (both
// entering and, on the line right after it, leaving), and a one-line
// inline <!-- docs-check --> ... <!-- /docs-check --> exemption (which
// must not leak into the following line).
func TestNoRetiredTermsDetection(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "docs/retired-terms.txt", "# comment\nharness\n")
	writeFile(t, root, "README.md", strings.Join([]string{
		"# skenv",                            // 1
		"",                                   // 2
		"Bracket header of a retired table:", // 3
		"",                                   // 4
		"[environment]",                      // 5
		"",                                   // 6
		"Retired dotted key in code: `environment.vendor`.", // 7
		"", // 8
		"The retired prose term Harness appears here.", // 9
		"", // 10
		"`project.from` still names the current project.from.<id> family.", // 11
		"", // 12
		"Inline code bracket form: `[environment]` in prose.", // 13
		"",                   // 14
		"| key | replaces |", // 15
		"| --- | --- |",      // 16
		"| `[[environment.vendor]]` | `[user.dependencies.<name>]` |", // 17
		"", // 18
		"`[project.from]` alone is not flagged (bracketOnly allows the single form).", // 19
		"", // 20
		"`[[project.from]]` is still flagged (the retired array syntax).", // 21
		"",                            // 22
		"<!-- docs-check: legacy -->", // 23
		"[environment]",               // 24
		"the harness term too",        // 25
		"<!-- /docs-check -->",        // 26
		"",                            // 27
		"After the exempt block, `[environment]` is flagged again.", // 28
		"", // 29
		"<!-- docs-check: legacy --> `repo.runner` <!-- /docs-check -->", // 30
		"", // 31
		"After an inline exemption, `repo.runner` is flagged again.", // 32
	}, "\n"))
	writeFile(t, root, "AGENTS.md", "no retired mentions here\n")

	got := scanRetiredTerms(t, root)
	want := []string{
		`README.md:5: retired skenv-file key "environment" (see internal/skenvfile.LegacyKeys)`,
		`README.md:7: retired skenv-file key "environment.vendor" (see internal/skenvfile.LegacyKeys)`,
		`README.md:9: retired term "harness" (see docs/retired-terms.txt)`,
		`README.md:13: retired skenv-file key "environment" (see internal/skenvfile.LegacyKeys)`,
		`README.md:17: retired skenv-file key "environment.vendor" (see internal/skenvfile.LegacyKeys)`,
		`README.md:21: retired skenv-file key "project.from" (see internal/skenvfile.LegacyKeys)`,
		`README.md:28: retired skenv-file key "environment" (see internal/skenvfile.LegacyKeys)`,
		`README.md:32: retired skenv-file key "repo.runner" (see internal/skenvfile.LegacyKeys)`,
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

// bracketOnly holds retired keys whose dotted-key form (unlike their
// bracket table-header form) is still valid prose in the 0.6 format: only
// the old array-of-tables syntax ([[project.from]]) is retired, while the
// dotted prefix "project.from" still names the project.from.<id> family
// in the current format (see docs/skenv-file.md's "Formats and editing"
// section). A bracketOnly key is therefore only checked as [[k]], never
// as bare [k] or as a dotted key: [project.from] on its own was never
// valid syntax in either format either, but flagging it would still be
// inconsistent with the "only the array form is retired" reasoning above.
var bracketOnly = map[string]bool{"project.from": true}

const (
	exemptStart = "<!-- docs-check: legacy -->"
	exemptEnd   = "<!-- /docs-check -->"
)

// scanRetiredTerms scans the files TestNoRetiredTerms covers under root
// for the skenv-file keys retired before 0.6 (LegacyKeys) and the prose
// terms retired since (docs/retired-terms.txt), skipping any
// <!-- docs-check: legacy --> ... <!-- /docs-check --> block or, for a
// single line, both markers on that one line. A key matches as a TOML
// table header ([k] or [[k]], the whole line before any trailing "#"
// comment, or the same wrapped in backticks anywhere in the line) or, for
// a dotted key, as the exact backtick-quoted text anywhere in the line.
// It returns one "path:line: message" string per mention found,
// root-relative paths with forward slashes.
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
			trimmed := strings.TrimSpace(line)
			if strings.Contains(trimmed, exemptStart) && strings.Contains(trimmed, exemptEnd) {
				continue // a single line wrapped in both markers
			}
			switch trimmed {
			case exemptStart:
				exempt = true
				continue
			case exemptEnd:
				exempt = false
				continue
			}
			if exempt {
				continue
			}
			lineNo := i + 1
			for _, k := range keys {
				if keyMatchesLine(k, line, trimmed) {
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

// keyMatchesLine reports whether the retired key k appears in line as a
// TOML table header ([k]/[[k]], possibly the sole bracketOnly form) or,
// for a dotted key not in bracketOnly, as a backtick-quoted dotted key.
func keyMatchesLine(k, line, trimmed string) bool {
	single, double := "["+k+"]", "[["+k+"]]"
	if !bracketOnly[k] && isBracketHeader(trimmed, single) {
		return true
	}
	if !bracketOnly[k] && strings.Contains(line, "`"+single+"`") {
		return true
	}
	if isBracketHeader(trimmed, double) || strings.Contains(line, "`"+double+"`") {
		return true
	}
	if strings.Contains(k, ".") && !bracketOnly[k] && strings.Contains(line, "`"+k+"`") {
		return true
	}
	return false
}

// isBracketHeader reports whether trimmed is exactly form, optionally
// followed by a "#" comment.
func isBracketHeader(trimmed, form string) bool {
	if trimmed == form {
		return true
	}
	before, _, ok := strings.Cut(trimmed, "#")
	return ok && strings.TrimSpace(before) == form
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
