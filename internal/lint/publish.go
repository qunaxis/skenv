package lint

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

// ForbiddenSources are metadata.source values that must not be published
// (P1): content from books, internal material and copies of third-party
// work.
var ForbiddenSources = []string{"book", "internal", "third-party-copy"}

// Denylist is the publication stop-list: one case-insensitive phrase per
// line, "#" starts a comment. It lives outside public repositories, so
// findings name the entry by line number, never by its text.
type Denylist struct {
	Path    string
	entries []denyEntry
}

type denyEntry struct {
	line   int
	phrase string // lowercased
}

// LoadDenylist reads the stop-list from $SKENV_DENYLIST (getenv) or
// ~/.config/skenv/denylist.txt. A missing stop-list is an error: the
// publication check must not pass silently without it.
func LoadDenylist(home string, getenv func(string) string) (*Denylist, error) {
	p := getenv("SKENV_DENYLIST")
	if p == "" {
		p = filepath.Join(home, ".config", "skenv", "denylist.txt")
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("stop-list %s not found: create it (one phrase per line) or point $SKENV_DENYLIST to it; in CI set the SKENV_DENYLIST secret (GitHub) or the SKENV_DENYLIST_B64 variable (GitLab)", p)
		}
		return nil, fmt.Errorf("stop-list %s: %w", p, err)
	}
	d := &Denylist{Path: p}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for n := 1; sc.Scan(); n++ {
		t := strings.TrimSpace(sc.Text())
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		d.entries = append(d.entries, denyEntry{line: n, phrase: normalizePhrase(t)})
	}
	if len(d.entries) == 0 {
		return nil, fmt.Errorf("stop-list %s has no entries", p)
	}
	return d, nil
}

// Len is the number of phrases.
func (d *Denylist) Len() int { return len(d.entries) }

// Publish runs the per-skill publication checks of P1: license and
// metadata.source. The stop-list (ScanDenylist) and gitleaks (Gitleaks) are
// repository-wide.
func Publish(dir string) []Finding {
	var out []Finding
	add := func(rule, path, format string, args ...any) {
		out = append(out, Finding{Skill: dir, Rule: rule, Path: path, Msg: fmt.Sprintf(format, args...)})
	}
	fm := readFrontmatter(dir)
	license, _ := fm["license"].(string)
	if strings.TrimSpace(license) == "" && !hasLicenseFile(dir) {
		add("P1", "", "no license: add a LICENSE file to the skill or a license field to the frontmatter")
	}
	if meta, ok := fm["metadata"].(map[string]any); ok {
		if src, ok := meta["source"].(string); ok {
			for _, bad := range ForbiddenSources {
				if strings.EqualFold(strings.TrimSpace(src), bad) {
					add("P1", "SKILL.md", "metadata.source is %q: content from %s must not be published", src, bad)
				}
			}
		}
	}
	return out
}

// RepoFiles lists the files git would publish from the repository at root
// (tracked plus untracked-but-not-ignored), relative to root.
func RepoFiles(root string) ([]string, error) {
	set := gitFiles(root)
	if set == nil {
		return nil, fmt.Errorf("%s is not a git work tree", root)
	}
	out := make([]string, 0, len(set))
	for f := range set {
		out = append(out, f)
	}
	sort.Strings(out)
	return out, nil
}

// ScanDenylist looks for stop-list phrases in every file of the repository
// (names and text). Matching ignores case and treats any run of whitespace
// (line breaks and no-break spaces included) as one space, so a phrase
// wrapped across lines is still found. Findings never contain the phrase:
// they cite the stop-list line and redact matching path components.
func ScanDenylist(root string, files []string, deny *Denylist) []Finding {
	var out []Finding
	for _, rel := range files {
		shown := deny.redact(rel)
		for _, e := range deny.matchText([]byte(rel)) {
			out = append(out, Finding{Skill: root, Rule: "P1", Path: shown,
				Msg: fmt.Sprintf("file path matches stop-list entry at line %d of %s", e.entry, deny.Path)})
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil || bytes.IndexByte(data, 0) >= 0 {
			continue // unreadable or binary
		}
		for _, m := range deny.matchText(data) {
			out = append(out, Finding{Skill: root, Rule: "P1", Path: shown,
				Msg: fmt.Sprintf("line %d matches stop-list entry at line %d of %s", m.line, m.entry, deny.Path)})
		}
	}
	return out
}

type denyMatch struct{ line, entry int }

// normalize lowercases s and collapses whitespace runs to one space; lines
// maps each output byte to its 1-based source line.
func normalize(data []byte) (string, []int) {
	var b strings.Builder
	lines := make([]int, 0, len(data))
	line, space := 1, false
	for len(data) > 0 {
		r, size := utf8.DecodeRune(data)
		if r == utf8.RuneError && size == 1 {
			r = rune(data[0]) // not UTF-8: compare bytes as Latin-1
		}
		startLine := line
		if r == '\n' {
			line++
		}
		data = data[size:]
		if unicode.IsSpace(r) || r == '\u00a0' {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
			lines = append(lines, startLine)
		}
		space = false
		n := b.Len()
		b.WriteString(strings.ToLower(string(r)))
		for i := n; i < b.Len(); i++ {
			lines = append(lines, startLine)
		}
	}
	return b.String(), lines
}

func normalizePhrase(p string) string {
	s, _ := normalize([]byte(p))
	return s
}

func (d *Denylist) matchText(data []byte) []denyMatch {
	text, lines := normalize(data)
	var out []denyMatch
	seen := map[denyMatch]bool{}
	for _, e := range d.entries {
		for from := 0; ; {
			i := strings.Index(text[from:], e.phrase)
			if i < 0 {
				break
			}
			m := denyMatch{line: lines[from+i], entry: e.line}
			if !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
			from += i + len(e.phrase)
		}
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].line != out[b].line {
			return out[a].line < out[b].line
		}
		return out[a].entry < out[b].entry
	})
	return out
}

// redact replaces path components that contain a stop-list phrase.
func (d *Denylist) redact(rel string) string {
	parts := strings.Split(rel, "/")
	for i, p := range parts {
		if len(d.matchText([]byte(p))) > 0 {
			parts[i] = "[redacted]"
		}
	}
	return strings.Join(parts, "/")
}

func readFrontmatter(dir string) map[string]any {
	data, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return nil
	}
	raw, ok := frontmatter(data)
	if !ok {
		return nil
	}
	var fm map[string]any
	_ = yaml.Unmarshal(raw, &fm)
	return fm
}

func hasLicenseFile(dir string) bool {
	matches, _ := filepath.Glob(filepath.Join(dir, "LICENSE*"))
	for _, m := range matches {
		if fi, err := os.Stat(m); err == nil && fi.Mode().IsRegular() && fi.Size() > 0 {
			return true
		}
	}
	return false
}

// Gitleaks scans the whole history of the repository at root (P1). Its
// report is redacted, so no secret reaches the output.
func Gitleaks(root string) (string, error) {
	bin, err := exec.LookPath("gitleaks")
	if err != nil {
		return "", errors.New("gitleaks is required for --publish (brew install gitleaks)")
	}
	cmd := exec.Command(bin, "git", "--redact", "--verbose", "--no-banner", "--log-level", "warn", ".")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 {
		return string(out), nil // leaks found: the report is the finding
	}
	if err != nil {
		return "", fmt.Errorf("gitleaks: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return "", nil
}
