// Package harness generates and verifies the tooling of a skills repository
// ("repository harness"): git hooks, CI workflow, linter configs and the
// managed blocks of AGENTS.md and .gitignore. Templates are embedded per
// harness version so a repository can stay on the version it was set up
// with until it is upgraded explicitly.
package harness

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"github.com/BurntSushi/toml"

	"github.com/qunaxis/skenv/internal/atomicfile"
)

// ConfigFile is the harness description in the repository root.
const ConfigFile = "skenv.toml"

// Latest is the newest harness version this skenv binary ships templates
// for; `repo init` uses it and `repo apply --upgrade` moves to it.
const Latest = "0.2.0"

// DefaultRunner is the runs-on of private repositories (the self-hosted
// Docker runner).
var DefaultRunner = []string{"self-hosted", "linux", "docker"}

//go:embed templates
var templates embed.FS

// Config is the content of skenv.toml.
type Config struct {
	Harness    string   `toml:"harness"`
	Visibility string   `toml:"visibility"`
	Runner     []string `toml:"runner"`
}

// LoadConfig reads skenv.toml in root.
func LoadConfig(root string) (*Config, error) {
	var c Config
	md, err := toml.DecodeFile(filepath.Join(root, ConfigFile), &c)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ConfigFile, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return nil, fmt.Errorf("%s: unknown key %s", ConfigFile, undecoded[0])
	}
	return &c, c.validate()
}

// Version returns the harness version recorded in root/skenv.toml without
// validating the rest; ok is false when there is no skenv.toml.
func Version(root string) (version string, ok bool, err error) {
	var c Config
	if _, err := toml.DecodeFile(filepath.Join(root, ConfigFile), &c); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", false, nil
		}
		return "", true, fmt.Errorf("%s: %w", ConfigFile, err)
	}
	return c.Harness, true, nil
}

func (c *Config) validate() error {
	switch c.Visibility {
	case "private":
		if len(c.Runner) == 0 {
			c.Runner = DefaultRunner
		}
	case "public":
	default:
		return fmt.Errorf("%s: visibility must be \"private\" or \"public\", got %q", ConfigFile, c.Visibility)
	}
	if c.Harness == "" {
		return fmt.Errorf("%s: harness is required", ConfigFile)
	}
	if _, err := fs.Stat(templates, "templates/"+c.Harness); err != nil {
		return fmt.Errorf("%s: harness %s is unknown to this skenv (it knows up to %s); upgrade skenv", ConfigFile, c.Harness, Latest)
	}
	return nil
}

func (c *Config) encode() []byte {
	var b bytes.Buffer
	b.WriteString("# Repository harness: `skenv repo apply` regenerates the managed files.\n")
	fmt.Fprintf(&b, "harness    = %q\n", c.Harness)
	fmt.Fprintf(&b, "visibility = %q\n", c.Visibility)
	if c.Visibility == "private" {
		quoted := make([]string, len(c.Runner))
		for i, r := range c.Runner {
			quoted[i] = strconv.Quote(r)
		}
		fmt.Fprintf(&b, "runner     = [%s]  # runs-on of the CI jobs\n", strings.Join(quoted, ", "))
	}
	return b.Bytes()
}

// kind of a managed item.
type kind int

const (
	whole kind = iota // the whole file is generated
	block             // only the text between markers is generated
)

// item is one managed file or block.
type item struct {
	Path     string // relative to the repository root
	Template string // file name under templates/<version>/
	Kind     kind
	Comment  string // line comment prefix for the header ("#", "//") or block markers ("<!--", "#")
}

var items = []item{
	{Path: "lefthook.yml", Template: "lefthook.yml", Kind: whole, Comment: "#"},
	{Path: ".github/workflows/check.yml", Template: "check.yml", Kind: whole, Comment: "#"},
	{Path: "ruff.toml", Template: "ruff.toml", Kind: whole, Comment: "#"},
	{Path: "pyrightconfig.json", Template: "pyrightconfig.json", Kind: whole, Comment: "//"},
	{Path: ".editorconfig", Template: "editorconfig", Kind: whole, Comment: "#"},
	{Path: ".markdownlint.yaml", Template: "markdownlint.yaml", Kind: whole, Comment: "#"},
	{Path: "AGENTS.md", Template: "AGENTS.md.block", Kind: block, Comment: "<!--"},
	{Path: ".gitignore", Template: "gitignore.block", Kind: block, Comment: "#"},
}

// Header is the first line of a managed file without the comment prefix.
func Header(version string) string { return "managed by skenv " + version + " — do not edit" }

type tmplData struct {
	Harness string
	Private bool
	RunsOn  string
}

// render returns the expected content of it: the whole file for managed
// files, the block including its markers for managed blocks.
func render(c *Config, it item) (string, error) {
	raw, err := templates.ReadFile(path.Join("templates", c.Harness, it.Template))
	if err != nil {
		return "", err
	}
	t, err := template.New(it.Template).Delims("[[", "]]").Option("missingkey=error").Parse(string(raw))
	if err != nil {
		return "", err
	}
	data := tmplData{Harness: c.Harness, Private: c.Visibility == "private", RunsOn: "ubuntu-latest"}
	if data.Private {
		data.RunsOn = "[" + strings.Join(c.Runner, ", ") + "]"
	}
	var body bytes.Buffer
	if err := t.Execute(&body, data); err != nil {
		return "", err
	}
	text := strings.TrimRight(body.String(), "\n") + "\n"
	begin, end := markers(it, c.Harness)
	if it.Kind == block {
		return begin + "\n" + text + end + "\n", nil
	}
	return it.Comment + " " + Header(c.Harness) + "\n" + text, nil
}

// markers returns the begin and end lines of a managed block.
func markers(it item, version string) (string, string) {
	if it.Comment == "<!--" {
		return "<!-- skenv:begin " + Header(version) + " -->", "<!-- skenv:end -->"
	}
	return it.Comment + " skenv:begin " + Header(version), it.Comment + " skenv:end"
}

func isBegin(it item, line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), it.Comment+" skenv:begin")
}

func isEnd(it item, line string) bool {
	_, end := markers(it, "")
	return strings.TrimSpace(line) == end
}

// findBlock returns the line range [start, end] of the managed block, or
// ok=false when there is none.
func findBlock(it item, lines []string) (start, end int, ok bool, err error) {
	start, end = -1, -1
	for i, l := range lines {
		switch {
		case isBegin(it, l):
			if start >= 0 {
				return 0, 0, false, errors.New("more than one skenv:begin marker")
			}
			start = i
		case isEnd(it, l):
			if start < 0 || end >= 0 {
				return 0, 0, false, errors.New("skenv:end without a matching skenv:begin")
			}
			end = i
		}
	}
	if start < 0 {
		return 0, 0, false, nil
	}
	if end < 0 {
		return 0, 0, false, errors.New("skenv:begin without skenv:end")
	}
	return start, end, true, nil
}

// Drift describes one managed item that differs from its template.
type Drift struct {
	Path   string
	Reason string
}

// Check compares the managed files and blocks in root with the templates of
// the configured harness version. CLAUDE.md in the root or .claude/ is
// reported as well: it disables AGENTS.md in Claude Code's default mode.
func Check(root string) ([]Drift, error) {
	c, err := LoadConfig(root)
	if err != nil {
		return nil, err
	}
	var out []Drift
	for _, p := range []string{"CLAUDE.md", filepath.Join(".claude", "CLAUDE.md")} {
		if _, err := os.Lstat(filepath.Join(root, p)); err == nil {
			out = append(out, Drift{Path: filepath.ToSlash(p), Reason: "must not exist: it disables loading of AGENTS.md in Claude Code; move its content to AGENTS.md"})
		}
	}
	for _, it := range items {
		want, err := render(c, it)
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(it.Path)))
		if errors.Is(err, fs.ErrNotExist) {
			out = append(out, Drift{Path: it.Path, Reason: "missing"})
			continue
		}
		if err != nil {
			return nil, err
		}
		got := normalize(string(data))
		if it.Kind == whole {
			if got != want {
				out = append(out, Drift{Path: it.Path, Reason: "differs from the harness " + c.Harness + " template"})
			}
			continue
		}
		lines := strings.SplitAfter(got, "\n")
		start, end, ok, err := findBlock(it, lines)
		switch {
		case err != nil:
			out = append(out, Drift{Path: it.Path, Reason: "managed block: " + err.Error()})
		case !ok:
			out = append(out, Drift{Path: it.Path, Reason: "managed block is missing"})
		case strings.Join(lines[start:end+1], "") != want:
			out = append(out, Drift{Path: it.Path, Reason: "managed block differs from the harness " + c.Harness + " template"})
		}
	}
	return out, nil
}

func normalize(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if s != "" && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return s
}

// Change is a file written by Apply.
type Change struct {
	Path   string
	Action string // "create" or "update"
}

// Apply regenerates every managed file and block in root for c. Text
// outside the managed blocks is kept. With dryRun nothing is written.
func Apply(root string, c *Config, dryRun bool) ([]Change, error) {
	if err := c.validate(); err != nil {
		return nil, err
	}
	var changes []Change
	for _, it := range items {
		want, err := render(c, it)
		if err != nil {
			return nil, err
		}
		file := filepath.Join(root, filepath.FromSlash(it.Path))
		data, err := os.ReadFile(file)
		exists := err == nil
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		next := want
		if it.Kind == block {
			next, err = mergeBlock(it, string(data), want, exists)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", it.Path, err)
			}
		}
		if exists && string(data) == next {
			continue
		}
		action := "update"
		if !exists {
			action = "create"
		}
		changes = append(changes, Change{Path: it.Path, Action: action})
		if dryRun {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			return nil, err
		}
		if err := atomicfile.Write(file, []byte(next), 0o644); err != nil {
			return nil, err
		}
	}
	return changes, nil
}

// mergeBlock replaces the managed block in data with want, or appends it.
func mergeBlock(it item, data, want string, exists bool) (string, error) {
	if !exists {
		if it.Path == "AGENTS.md" {
			return "# AGENTS.md\n\nRepository-specific instructions go here, outside the managed block.\n\n" + want, nil
		}
		return want, nil
	}
	// Text outside the block is kept byte for byte (line endings included);
	// only a missing final newline is added before appending.
	if data != "" && !strings.HasSuffix(data, "\n") {
		data += "\n"
	}
	lines := strings.SplitAfter(data, "\n")
	start, end, ok, err := findBlock(it, lines)
	if err != nil {
		return "", fmt.Errorf("%w; fix the markers by hand", err)
	}
	if !ok {
		if strings.TrimSpace(data) == "" {
			return want, nil
		}
		return data + "\n" + want, nil
	}
	return strings.Join(lines[:start], "") + want + strings.Join(lines[end+1:], ""), nil
}

// Init creates skenv.toml and all managed files. It refuses to run when
// skenv.toml already exists.
func Init(root, visibility string, dryRun bool) (*Config, []Change, error) {
	if _, err := os.Stat(filepath.Join(root, ConfigFile)); err == nil {
		return nil, nil, fmt.Errorf("%s already exists; use `skenv repo apply` to regenerate the managed files", ConfigFile)
	}
	c := &Config{Harness: Latest, Visibility: visibility}
	if err := c.validate(); err != nil {
		return nil, nil, err
	}
	changes := []Change{{Path: ConfigFile, Action: "create"}}
	if !dryRun {
		if err := atomicfile.Write(filepath.Join(root, ConfigFile), c.encode(), 0o644); err != nil {
			return nil, nil, err
		}
	}
	more, err := Apply(root, c, dryRun)
	return c, append(changes, more...), err
}

// SetHarness rewrites the harness version in skenv.toml, keeping the rest
// of the file.
func SetHarness(root, version string, dryRun bool) error {
	file := filepath.Join(root, ConfigFile)
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	lines := strings.SplitAfter(string(data), "\n")
	done := false
	for i, l := range lines {
		key, _, ok := strings.Cut(l, "=")
		if ok && strings.TrimSpace(key) == "harness" {
			nl := l[len(strings.TrimRight(l, "\r\n")):]
			lines[i] = "harness    = " + strconv.Quote(version) + nl
			done = true
			break
		}
	}
	if !done {
		return fmt.Errorf("%s has no harness key", ConfigFile)
	}
	if dryRun {
		return nil
	}
	return atomicfile.Write(file, []byte(strings.Join(lines, "")), 0o644)
}

// Compare compares dotted numeric versions ("0.2.0", "v0.10.1"): -1, 0, 1.
// Pre-release or build suffixes are ignored.
func Compare(a, b string) int {
	pa, pb := parts(a), parts(b)
	for i := 0; i < 3; i++ {
		switch {
		case pa[i] < pb[i]:
			return -1
		case pa[i] > pb[i]:
			return 1
		}
	}
	return 0
}

func parts(v string) [3]int {
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	var out [3]int
	for i, s := range strings.SplitN(v, ".", 3) {
		n, _ := strconv.Atoi(s)
		out[i] = n
	}
	return out
}
