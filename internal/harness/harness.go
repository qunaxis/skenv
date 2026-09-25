// Package harness generates and verifies the tooling of a skills repository
// ("repository harness"): git hooks, CI workflow, linter configs and the
// managed blocks of AGENTS.md and .gitignore. The templates of one harness
// version (Latest) are embedded; `skenv repo apply` moves a repository to
// it. Its settings are the [repo] section of the skenv file.
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
	"regexp"
	"strconv"
	"strings"
	"text/template"

	"github.com/qunaxis/skenv/internal/atomicfile"
	"github.com/qunaxis/skenv/internal/docedit"
	"github.com/qunaxis/skenv/internal/fileformat"
	"github.com/qunaxis/skenv/internal/skenvfile"
	"github.com/qunaxis/skenv/schemas"
)

// ConfigFile is the skenv file that `repo init` creates when the
// repository has none and --format is not given; errors about a file not
// read from disk name it too.
const ConfigFile = "skenv.toml"

// Latest is the harness version of the embedded templates; `repo init`
// and `repo apply` write it. The CI workflow installs this skenv release.
const Latest = "0.4.0"

// DefaultRunner is the runs-on of private repositories (the self-hosted
// Docker runner).
var DefaultRunner = []string{"self-hosted", "linux", "docker"}

//go:embed templates
var templates embed.FS

// Config is the [repo] section of the skenv file: the harness of a skills
// repository, the tooling that `skenv repo apply` generates.
//
// The first paragraph of this comment and the comments of the fields are
// the descriptions of the JSON Schema (`make schemas`): write them for
// users.
type Config struct {
	// Harness is the version of the harness templates, which is also the
	// skenv release the generated CI installs. It must not be newer than
	// the templates of the skenv that reads it; `skenv repo apply` moves it
	// to the version of the running skenv.
	Harness string `toml:"harness" yaml:"harness" json:"harness"`
	// Visibility is the visibility of the repository on GitHub: "private"
	// or "public". A public repository must not have [environment], and its
	// CI always runs on ubuntu-latest.
	Visibility string `toml:"visibility" yaml:"visibility" json:"visibility"`
	// Runner is the runs-on of the CI jobs, used only when visibility is
	// "private". Default: ["self-hosted", "linux", "docker"]; use
	// ["ubuntu-latest"] for GitHub-hosted runners.
	Runner []string `toml:"runner" yaml:"runner" json:"runner"`

	// File is the skenv file; HasEnvironment reports whether it also
	// carries the [environment] section (a manifest).
	File           string `toml:"-" yaml:"-" json:"-"`
	HasEnvironment bool   `toml:"-" yaml:"-" json:"-"`
}

// errNoRepo is returned when the repository has no [repo] section.
var errNoRepo = errors.New("no [repo] section in the skenv file; run `skenv repo init`")

// ReadRaw decodes the [repo] section of the skenv file in root without
// validating it; ok is false when there is no skenv file or no [repo].
func ReadRaw(root string) (*Config, bool, error) {
	file, err := skenvfile.Find(root)
	if err != nil || file == "" {
		return nil, false, err
	}
	doc, err := skenvfile.Read(file)
	if err != nil {
		return nil, true, err
	}
	if !doc.Has(skenvfile.Repo) {
		return nil, false, nil
	}
	c := &Config{File: file, HasEnvironment: doc.Has(skenvfile.Environment)}
	if err := doc.Decode(skenvfile.Repo, c); err != nil {
		return nil, true, fmt.Errorf("%s: %w", file, err)
	}
	return c, true, nil
}

// LoadConfig reads and validates the [repo] section of the skenv file in
// root.
func LoadConfig(root string) (*Config, error) {
	c, ok, err := ReadRaw(root)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errNoRepo
	}
	return c, c.validate()
}

// Version returns the harness version recorded in root without validating
// the rest; ok is false when root has no [repo] section.
func Version(root string) (version string, ok bool, err error) {
	c, ok, err := ReadRaw(root)
	if c == nil {
		return "", ok, err
	}
	return c.Harness, ok, err
}

// VersionPattern is the format of repo.harness.
const VersionPattern = `^[0-9]+\.[0-9]+\.[0-9]+$`

// VersionRe matches VersionPattern.
var VersionRe = regexp.MustCompile(VersionPattern)

// Parse decodes and validates the [repo] section of a skenv file in the
// format of ext, with the rule that a public repository has no
// [environment]. ok is false when there is no [repo].
func Parse(data []byte, ext string) (c *Config, ok bool, err error) {
	doc, err := skenvfile.Parse(data, ext)
	if err != nil || !doc.Has(skenvfile.Repo) {
		return nil, false, err
	}
	c = &Config{File: ConfigFile, HasEnvironment: doc.Has(skenvfile.Environment)}
	if err := doc.Decode(skenvfile.Repo, c); err != nil {
		return nil, true, err
	}
	if err := c.validate(); err != nil {
		return nil, true, err
	}
	if c.Visibility == "public" && c.HasEnvironment {
		return nil, true, errors.New(errPublicEnvironment)
	}
	return c, true, nil
}

// errPublicEnvironment explains why a public repository must not carry the
// manifest.
const errPublicEnvironment = "a public repository must not carry [environment]: the manifest is personal " +
	"(home paths, host names, which skills you use); keep it in a private repository"

func (c *Config) validate() error {
	name := filepath.Base(c.File)
	if c.File == "" {
		name = ConfigFile
	}
	switch c.Visibility {
	case "private":
		if len(c.Runner) == 0 {
			c.Runner = DefaultRunner
		}
	case "public":
	default:
		return fmt.Errorf("%s: repo.visibility must be \"private\" or \"public\", got %q", name, c.Visibility)
	}
	if c.Harness == "" {
		return fmt.Errorf("%s: repo.harness is required", name)
	}
	if !VersionRe.MatchString(c.Harness) {
		return fmt.Errorf("%s: repo.harness %q must be a version such as %s", name, c.Harness, Latest)
	}
	if Compare(c.Harness, Latest) > 0 {
		return fmt.Errorf("%s: harness %s is newer than %s of this skenv; upgrade skenv", name, c.Harness, Latest)
	}
	return nil
}

// repoHeader is the first comment of a skenv file that `repo init`
// creates.
const repoHeader = "# Repository harness: `skenv repo apply` regenerates the managed files.\n"

func (c *Config) encode() []byte {
	var b bytes.Buffer
	b.WriteString(repoHeader)
	b.WriteString("[repo]\n")
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
	// JSON has no comments: the template carries the header in "$comment".
	{Path: ".claude/settings.json", Template: "claude-settings.json", Kind: whole, Comment: jsonHeader},
}

// jsonHeader marks items whose template writes its own header.
const jsonHeader = "json"

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
	raw, err := templates.ReadFile(path.Join("templates", it.Template))
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
	if it.Comment == jsonHeader {
		return text, nil
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

// Check compares the managed files and blocks in root with the templates.
// Also reported: a harness older than Latest (the files are then not
// compared), CLAUDE.md in the root or .claude/ (it disables AGENTS.md in
// Claude Code's default mode) and [environment] in a public repository.
func Check(root string) ([]Drift, error) {
	c, err := LoadConfig(root)
	if err != nil {
		return nil, err
	}
	var out []Drift
	file := filepath.Base(c.File)
	if c.Visibility == "public" && c.HasEnvironment {
		out = append(out, Drift{Path: file, Reason: errPublicEnvironment})
	}
	if c.Harness != Latest {
		return append(out, Drift{Path: file, Reason: fmt.Sprintf("harness %s; this skenv generates %s: run `skenv repo apply`", c.Harness, Latest)}), nil
	}
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

// isManaged reports whether a file carries the skenv header in its first
// lines (a comment, or the "$comment" key of JSON files).
func isManaged(data []byte) bool {
	head := data
	if lines := bytes.SplitN(data, []byte("\n"), 4); len(lines) > 3 {
		head = bytes.Join(lines[:3], []byte("\n"))
	}
	return bytes.Contains(head, []byte("managed by skenv "))
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
// outside the managed blocks is kept. An existing file that skenv does not
// manage yet (no "managed by skenv" header) is only replaced with force.
// With dryRun nothing is written.
func Apply(root string, c *Config, dryRun, force bool) ([]Change, error) {
	if err := c.validate(); err != nil {
		return nil, err
	}
	if !force {
		var foreign []string
		for _, it := range items {
			if it.Kind != whole {
				continue
			}
			data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(it.Path)))
			if err == nil && !isManaged(data) {
				foreign = append(foreign, it.Path)
			}
		}
		if len(foreign) > 0 {
			return nil, fmt.Errorf("%s exist and are not managed by skenv; move what you need out of them (local Claude Code settings: .claude/settings.local.json), then rerun with --force to replace them", strings.Join(foreign, ", "))
		}
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

// Init adds the [repo] section and all managed files. Without a skenv file
// it creates skenv.<format> (format "" is TOML); an existing one (a
// manifest repository) gets the section added in its own format, and a
// format that disagrees with it is an error. It refuses when [repo] exists
// already.
func Init(root, visibility, format string, dryRun, force bool) (*Config, []Change, error) {
	file, err := skenvfile.Find(root)
	if err != nil {
		return nil, nil, err
	}
	target, err := fileformat.Choose(root, "skenv", file, format)
	if err != nil {
		return nil, nil, err
	}
	c := &Config{Harness: Latest, Visibility: visibility, File: file}
	var data []byte
	if file != "" {
		doc, err := skenvfile.Read(file)
		if err != nil {
			return nil, nil, err
		}
		if doc.Has(skenvfile.Repo) {
			return nil, nil, fmt.Errorf("%s already has [repo]; use `skenv repo apply` to regenerate the managed files", filepath.Base(file))
		}
		c.HasEnvironment = doc.Has(skenvfile.Environment)
		if data, err = os.ReadFile(file); err != nil {
			return nil, nil, err
		}
	} else {
		c.File = target
	}
	if err := c.validate(); err != nil {
		return nil, nil, err
	}
	if c.Visibility == "public" && c.HasEnvironment {
		return nil, nil, fmt.Errorf("%s: %s", filepath.Base(c.File), errPublicEnvironment)
	}
	if !force {
		// Refuse before writing the skenv file, so a rerun with --force works.
		if _, err := Apply(root, c, true, false); err != nil {
			return nil, nil, err
		}
	}
	out, err := addRepo(data, filepath.Ext(c.File), c)
	if err != nil {
		return nil, nil, err
	}
	if out, err = skenvfile.Stamp(out, filepath.Ext(c.File), true); err != nil {
		return nil, nil, err
	}
	action := "create"
	if file != "" {
		action = "add [repo] to"
	}
	changes := []Change{{Path: filepath.Base(c.File), Action: action}}
	if !dryRun {
		if err := writeKeepMode(c.File, out); err != nil {
			return nil, nil, err
		}
	}
	more, err := Apply(root, c, dryRun, true)
	return c, append(changes, more...), err
}

// addRepo returns data with the [repo] section of c: appended to TOML,
// at the top of YAML and JSON, as the docs show it.
func addRepo(data []byte, ext string, c *Config) ([]byte, error) {
	if ext == ".toml" {
		var b bytes.Buffer
		b.Write(data)
		if len(bytes.TrimSpace(data)) > 0 {
			if !bytes.HasSuffix(data, []byte("\n")) {
				b.WriteByte('\n')
			}
			b.WriteByte('\n')
		}
		b.Write(c.encode())
		return b.Bytes(), nil
	}
	d, err := docedit.Open(data, ext)
	if err != nil {
		return nil, err
	}
	repo := docedit.Map{{Key: "harness", Value: c.Harness}, {Key: "visibility", Value: c.Visibility}}
	if c.Visibility == "private" {
		repo = append(repo, docedit.Field{Key: "runner", Value: c.Runner})
	}
	if err := d.Put(nil, skenvfile.Repo, repo, true); err != nil {
		return nil, err
	}
	out := d.Bytes()
	// A new YAML file starts with the same comment as a TOML one; JSON has
	// no comments.
	if ext != ".json" && len(bytes.TrimSpace(data)) == 0 {
		out = append([]byte(repoHeader), out...)
	}
	return out, nil
}

var (
	repoHeaderRe = regexp.MustCompile(`^\s*\[\s*repo\s*\]\s*(#.*)?$`)
	tableRe      = regexp.MustCompile(`^\s*\[`)
	harnessKeyRe = regexp.MustCompile(`^(\s*harness\s*=\s*)"[^"]*"(.*)$`)
)

// Update sets repo.harness in the skenv file of root to version and moves
// its schema directive there (adding it when missing). Comments, key order
// and formatting stay in every format. It reports whether the file
// changes; with dryRun nothing is written.
func Update(root, version string, dryRun bool) (bool, error) {
	file, err := skenvfile.Find(root)
	if err != nil {
		return false, err
	}
	if file == "" {
		return false, errNoRepo
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return false, err
	}
	out, err := setHarness(data, filepath.Ext(file), version)
	if err != nil {
		return false, fmt.Errorf("%s: %w", filepath.Base(file), err)
	}
	if out, err = skenvfile.Stamp(out, filepath.Ext(file), true); err != nil {
		return false, err
	}
	if bytes.Equal(out, data) {
		return false, nil
	}
	if dryRun {
		return true, nil
	}
	return true, writeKeepMode(file, out)
}

func setHarness(data []byte, ext, version string) ([]byte, error) {
	doc, err := skenvfile.Parse(data, ext)
	if err != nil {
		return nil, err
	}
	if doc.Harness() == version {
		return data, nil
	}
	if ext != ".toml" {
		d, err := docedit.Open(data, ext)
		if err != nil {
			return nil, err
		}
		if err := d.SetString([]any{skenvfile.Repo, "harness"}, version); err != nil {
			return nil, err
		}
		return d.Bytes(), nil
	}
	lines := strings.SplitAfter(string(data), "\n")
	inRepo := false
	for i, l := range lines {
		trimmed := strings.TrimRight(l, "\r\n")
		switch {
		case repoHeaderRe.MatchString(trimmed):
			inRepo = true
			continue
		case tableRe.MatchString(trimmed):
			inRepo = false
			continue
		}
		if m := harnessKeyRe.FindStringSubmatch(trimmed); inRepo && m != nil {
			lines[i] = m[1] + strconv.Quote(version) + m[2] + l[len(trimmed):]
			return []byte(strings.Join(lines, "")), nil
		}
	}
	return nil, errors.New(`no harness = "..." line under [repo]`)
}

// DirectiveWarning describes a missing or outdated schema directive in the
// skenv file of c: it should name the schema of repo.harness. It is only a
// warning, because the directive does not change what skenv does.
func DirectiveWarning(c *Config) (string, error) {
	data, err := os.ReadFile(c.File)
	if err != nil {
		return "", err
	}
	return schemas.Check(data, filepath.Ext(c.File), schemas.Skenv, c.Harness), nil
}

func writeKeepMode(file string, data []byte) error {
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(file); err == nil {
		mode = fi.Mode().Perm()
	}
	return atomicfile.Write(file, data, mode)
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
