// Package lint checks skills (directories with SKILL.md) against the Agent
// Skills format and the repository rules L1-L6.
package lint

import (
	"bufio"
	"bytes"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

// Limits from the Agent Skills specification (agentskills.io).
const (
	MaxNameLen        = 64
	MaxDescriptionLen = 1024
	MaxFileSize       = 10 << 20 // L5
)

// Finding is one rule violation.
type Finding struct {
	Skill string // skill directory as given
	Rule  string // L1..L6
	Path  string // file inside the skill, relative, or "" for the skill itself
	Msg   string
}

func (f Finding) String() string {
	where := f.Skill
	if f.Path != "" {
		where = filepath.Join(f.Skill, f.Path)
	}
	return fmt.Sprintf("%s: %s: %s", where, f.Rule, f.Msg)
}

var (
	nameRe = regexp.MustCompile(`^[a-z0-9-]+$`)
	// [text](target) and ![alt](target); the target ends at whitespace or ")".
	inlineLinkRe = regexp.MustCompile(`!?\[[^\]]*\]\(\s*<?([^)\s>]+)>?(?:\s+"[^"]*")?\s*\)`)
	// [label]: target
	refLinkRe  = regexp.MustCompile(`^\s{0,3}\[[^\]]+\]:\s*<?(\S+?)>?(?:\s+.*)?$`)
	codeSpanRe = regexp.MustCompile("`[^`]*`")
	schemeRe   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)
)

// skipDirs are never walked: VCS data and local tool environments.
var skipDirs = map[string]bool{".git": true, "node_modules": true, ".venv": true, "__pycache__": true}

// Skill checks the skill in dir.
//
// Inside a git work tree only the files git would commit are considered
// (tracked plus untracked-but-not-ignored), so ignored local files such as
// .env or caches never block a commit; outside git every file is checked.
func Skill(dir string) []Finding {
	var out []Finding
	add := func(rule, path, format string, args ...any) {
		out = append(out, Finding{Skill: dir, Rule: rule, Path: path, Msg: fmt.Sprintf(format, args...)})
	}
	files := gitFiles(dir)
	checkFrontmatter(dir, add)
	checkLinks(dir, files, add)
	checkFiles(dir, files, add)
	return out
}

// fileSet is the set of skill-relative, slash-separated file paths git
// sees; nil means "not in a git work tree, use the file system".
type fileSet map[string]bool

func gitFiles(dir string) fileSet {
	cmd := exec.Command("git", "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--full-name", "--", ".")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	top := exec.Command("git", "rev-parse", "--show-prefix")
	top.Dir = dir
	prefix, err := top.Output()
	if err != nil {
		return nil
	}
	pre := strings.TrimSpace(string(prefix))
	set := fileSet{}
	for _, f := range strings.Split(string(out), "\x00") {
		if rel, ok := strings.CutPrefix(f, pre); ok && rel != "" {
			set[rel] = true
		}
	}
	return set
}

// has reports whether rel (a file or directory) is part of the set.
func (s fileSet) has(rel string) bool {
	rel = filepath.ToSlash(rel)
	if s[rel] {
		return true
	}
	for f := range s {
		if strings.HasPrefix(f, rel+"/") {
			return true
		}
	}
	return false
}

type addFunc func(rule, path, format string, args ...any)

// checkFrontmatter implements L1-L3.
func checkFrontmatter(dir string, add addFunc) {
	data, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		add("L1", "SKILL.md", "cannot read: %v", err)
		return
	}
	raw, ok := frontmatter(data)
	if !ok {
		add("L1", "SKILL.md", "no YAML frontmatter: the file must start with a line \"---\" and the frontmatter must end with \"---\"")
		return
	}
	var fm map[string]any
	if err := yaml.Unmarshal(raw, &fm); err != nil {
		add("L1", "SKILL.md", "frontmatter is not valid YAML: %v", err)
		return
	}
	name, nameOK := fm["name"].(string)
	if _, present := fm["name"]; !present {
		add("L1", "SKILL.md", "frontmatter has no name")
	} else if !nameOK {
		add("L1", "SKILL.md", "name must be a string")
	}
	desc, descOK := fm["description"].(string)
	if _, present := fm["description"]; !present {
		add("L1", "SKILL.md", "frontmatter has no description")
	} else if !descOK {
		add("L1", "SKILL.md", "description must be a string")
	}

	if nameOK {
		base := filepath.Base(filepath.Clean(dir))
		if abs, err := filepath.Abs(dir); err == nil {
			base = filepath.Base(abs)
		}
		if name != base {
			add("L2", "SKILL.md", "name %q must equal the directory name %q", name, base)
		}
		if !nameRe.MatchString(name) {
			add("L2", "SKILL.md", "name %q may contain only lowercase letters, digits and \"-\"", name)
		}
		if n := utf8.RuneCountInString(name); n > MaxNameLen {
			add("L3", "SKILL.md", "name is %d characters, the limit is %d", n, MaxNameLen)
		}
	}
	if descOK {
		switch n := utf8.RuneCountInString(strings.TrimSpace(desc)); {
		case n == 0:
			add("L3", "SKILL.md", "description is empty")
		case n > MaxDescriptionLen:
			add("L3", "SKILL.md", "description is %d characters, the limit is %d", n, MaxDescriptionLen)
		}
	}
	if v, ok := fm["license"]; ok {
		if _, isStr := v.(string); !isStr {
			add("L3", "SKILL.md", "license must be a string (a top-level frontmatter field)")
		}
	}
	if v, ok := fm["metadata"]; ok {
		m, isMap := v.(map[string]any)
		if !isMap {
			add("L3", "SKILL.md", "metadata must be a map of strings")
		} else {
			for _, k := range sortedKeys(m) {
				if _, isStr := m[k].(string); !isStr {
					add("L3", "SKILL.md", "metadata.%s must be a string (quote it)", k)
				}
			}
		}
	}
}

// frontmatter returns the YAML between the leading "---" lines.
func frontmatter(data []byte) ([]byte, bool) {
	data = bytes.TrimPrefix(data, []byte("\ufeff"))
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		return nil, false
	}
	rest := s[len("---\n"):]
	if strings.HasPrefix(rest, "---\n") || rest == "---" {
		return []byte{}, true
	}
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		if !strings.HasSuffix(rest, "\n---") {
			return nil, false
		}
		end = len(rest) - len("\n---")
	}
	return []byte(rest[:end]), true
}

// checkLinks implements L4 for SKILL.md and references/*.md.
func checkLinks(dir string, files fileSet, add addFunc) {
	docs := []string{"SKILL.md"}
	refs, _ := filepath.Glob(filepath.Join(dir, "references", "*.md"))
	for _, r := range refs {
		docs = append(docs, filepath.Join("references", filepath.Base(r)))
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return
	}
	for _, rel := range docs {
		data, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			continue
		}
		for _, l := range links(data) {
			target := l.target
			if schemeRe.MatchString(target) || strings.HasPrefix(target, "#") || strings.HasPrefix(target, "//") {
				continue
			}
			if i := strings.IndexAny(target, "#?"); i >= 0 {
				target = target[:i]
			}
			if target == "" {
				continue
			}
			if dec, err := url.PathUnescape(target); err == nil {
				target = dec
			}
			if filepath.IsAbs(target) {
				add("L4", rel, "line %d: link %q is absolute; use a path relative to the file", l.line, l.target)
				continue
			}
			abs := filepath.Join(root, filepath.Dir(rel), filepath.FromSlash(target))
			if r, err := filepath.Rel(root, abs); err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
				add("L4", rel, "line %d: link %q points outside the skill", l.line, l.target)
				continue
			}
			inSkill, _ := filepath.Rel(root, abs)
			if _, err := os.Stat(abs); err != nil {
				add("L4", rel, "line %d: link %q: file does not exist", l.line, l.target)
			} else if files != nil && inSkill != "." && !files.has(inSkill) {
				add("L4", rel, "line %d: link %q: file is ignored by git and would not be committed", l.line, l.target)
			}
		}
	}
}

type link struct {
	line   int
	target string
}

// links extracts link targets outside fenced code blocks and code spans.
func links(data []byte) []link {
	var out []link
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	fence := ""
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fence = trimmed[:3]
			continue
		}
		if m := refLinkRe.FindStringSubmatch(line); m != nil {
			out = append(out, link{n, m[1]})
			continue
		}
		line = codeSpanRe.ReplaceAllString(line, "")
		for _, m := range inlineLinkRe.FindAllStringSubmatch(line, -1) {
			out = append(out, link{n, m[1]})
		}
	}
	return out
}

// checkFiles implements L5 and L6.
func checkFiles(dir string, files fileSet, add addFunc) {
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		rel, _ := filepath.Rel(dir, p)
		if err != nil {
			// Unreadable entries are reported; the walk goes on.
			add("L5", rel, "cannot read: %v", err)
			return nil //nolint:nilerr
		}
		if d.IsDir() {
			if p != dir && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if files != nil && !files[filepath.ToSlash(rel)] {
			return nil // ignored by git: never committed
		}
		name := d.Name()
		switch {
		case name == ".env", strings.HasSuffix(name, ".pem"), strings.HasSuffix(name, ".key"), strings.HasPrefix(name, ".credentials"):
			add("L5", rel, "file name looks like a secret (.env, *.pem, *.key, .credentials*); remove it from the skill")
		}
		info, err := d.Info()
		if err != nil {
			add("L5", rel, "cannot read: %v", err)
			return nil //nolint:nilerr // reported above; keep walking
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if info.Size() > MaxFileSize {
			add("L5", rel, "file is %.1f MB, the limit is 10 MB", float64(info.Size())/(1<<20))
		}
		if info.Mode().Perm()&0o111 != 0 && !hasShebang(p) {
			add("L6", rel, "executable file has no shebang (#!); add one or remove the executable bit")
		}
		return nil
	})
}

func hasShebang(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, 2)
	n, _ := f.Read(buf)
	return n == 2 && string(buf) == "#!"
}

// Find returns the skill directories under path: path itself when it holds
// SKILL.md, otherwise every directory below it that does (not descending
// into skills).
func Find(path string) ([]string, error) {
	if _, err := os.Stat(filepath.Join(path, "SKILL.md")); err == nil {
		return []string{path}, nil
	}
	if fi, err := os.Stat(path); err != nil {
		return nil, err
	} else if !fi.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", path)
	}
	var out []string
	err := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if p != path && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
			return filepath.SkipDir
		}
		if p != path {
			if _, err := os.Stat(filepath.Join(p, "SKILL.md")); err == nil {
				out = append(out, p)
				return filepath.SkipDir
			}
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

// ForFiles maps changed files (relative to root) to the skills containing
// them: the nearest ancestor directory with SKILL.md.
func ForFiles(root string, files []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range files {
		dir := filepath.Dir(filepath.Join(root, filepath.FromSlash(f)))
		for {
			rel, err := filepath.Rel(root, dir)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				break
			}
			if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err == nil {
				if !seen[dir] {
					seen[dir] = true
					out = append(out, dir)
				}
				break
			}
			if rel == "." {
				break
			}
			dir = filepath.Dir(dir)
		}
	}
	sort.Strings(out)
	return out
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
