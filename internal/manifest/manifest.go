// Package manifest parses and validates the manifest: the [environment]
// section of a skenv file (skenv.toml), the declarative list of skills that
// skenv keeps in sync on a machine.
package manifest

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/qunaxis/skenv/internal/skenvfile"
)

// DefaultSkillsDir is used when an [[own]] entry does not set skills_dir.
const DefaultSkillsDir = "skills"

// Manifest is the parsed [environment] section. Paths are kept as written (possibly with
// a leading "~"); callers expand them against the home directory.
type Manifest struct {
	Layout Layout          `toml:"layout" yaml:"layout" json:"layout"`
	Own    []Own           `toml:"own" yaml:"own" json:"own"`
	Vendor []Vendor        `toml:"vendor" yaml:"vendor" json:"vendor"`
	Host   map[string]Host `toml:"host" yaml:"host" json:"host"`
}

// Layout describes where skills are stored and linked.
type Layout struct {
	Store string `toml:"store" yaml:"store" json:"store"`
	// Targets overrides the built-in agent table when non-nil (A3). An
	// explicitly empty list means "no agent directories besides the store".
	Targets []string `toml:"targets" yaml:"targets" json:"targets"`
	// Ignore lists glob patterns (path.Match) of entry names in the store and
	// targets that belong to other tools: doctor does not report them and
	// sync/link never touch them, not even with --adopt.
	Ignore []string `toml:"ignore" yaml:"ignore" json:"ignore"`
}

// Ignored reports whether an entry name matches layout.ignore.
func (l Layout) Ignored(name string) bool {
	for _, pat := range l.Ignore {
		if ok, _ := path.Match(pat, name); ok {
			return true
		}
	}
	return false
}

// Own is a skills repository kept as a working copy.
type Own struct {
	Repo      string `toml:"repo" yaml:"repo" json:"repo"`
	Path      string `toml:"path" yaml:"path" json:"path"`
	SkillsDir string `toml:"skills_dir" yaml:"skills_dir" json:"skills_dir"`
}

// Vendor is a third-party skill pinned to a commit.
type Vendor struct {
	Name string `toml:"name" yaml:"name" json:"name"`
	Repo string `toml:"repo" yaml:"repo" json:"repo"`
	Path string `toml:"path" yaml:"path" json:"path"`
	Rev  string `toml:"rev" yaml:"rev" json:"rev"`
}

// Host holds per-machine overrides keyed by hostname.
type Host struct {
	Skip []string `toml:"skip" yaml:"skip" json:"skip"`
}

var (
	shaRe  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
)

// Locate returns the skenv file that path names: path itself, or the skenv
// file in path when it is a directory.
func Locate(path string) (string, error) {
	if filepath.Base(path) == "env.toml" {
		return "", skenvfile.OldManifestError(path)
	}
	fi, err := os.Stat(path)
	if err != nil || !fi.IsDir() {
		return path, nil //nolint:nilerr // Load reports a missing file
	}
	file, err := skenvfile.Find(path)
	if err != nil {
		return "", err
	}
	if file == "" {
		return "", fmt.Errorf("no skenv file (%s) in %s", strings.Join(skenvfile.Names, ", "), path)
	}
	return file, nil
}

// Load reads and validates the manifest in the skenv file at file.
func Load(file string) (*Manifest, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("read manifest %s: %w (set --manifest, $SKENV_MANIFEST or run `skenv init`)", file, err)
	}
	m, err := Parse(data, filepath.Ext(file))
	if err != nil {
		return nil, fmt.Errorf("manifest %s: %w", file, err)
	}
	return m, nil
}

// Parse decodes and validates the [environment] section of a skenv file in
// the format of ext (".toml", ".yaml", ".yml", ".json").
func Parse(data []byte, ext string) (*Manifest, error) {
	doc, err := skenvfile.Parse(data, ext)
	if err != nil {
		return nil, err
	}
	if !doc.Has(skenvfile.Environment) {
		return nil, errors.New("no [environment] section: this skenv file is not a manifest")
	}
	var m Manifest
	if err := doc.Decode(skenvfile.Environment, &m); err != nil {
		return nil, err
	}
	if doc.IsDefined(skenvfile.Environment, "layout", "targets") && m.Layout.Targets == nil {
		m.Layout.Targets = []string{}
	}
	for i := range m.Own {
		if m.Own[i].SkillsDir == "" {
			m.Own[i].SkillsDir = DefaultSkillsDir
		}
	}
	for i := range m.Vendor {
		if m.Vendor[i].Path == "" {
			m.Vendor[i].Path = "."
		}
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// Validate checks the rules that do not need the file system: required
// fields, full SHAs (M2) and unique vendor names (M1). Name clashes between
// own and vendor skills are checked by CheckNames once own repositories are
// listed.
func (m *Manifest) Validate() error {
	var errs []error
	for _, pat := range m.Layout.Ignore {
		if _, err := path.Match(pat, ""); err != nil || pat == "" || strings.Contains(pat, "/") {
			errs = append(errs, fmt.Errorf("layout.ignore: %q must be a glob over entry names (no \"/\")", pat))
		}
	}
	for i, o := range m.Own {
		if o.Repo == "" {
			errs = append(errs, fmt.Errorf("own[%d]: repo is required", i))
		}
		if o.Path == "" {
			errs = append(errs, fmt.Errorf("own[%d] (%s): path is required", i, o.Repo))
		}
		if !cleanRel(o.SkillsDir) {
			errs = append(errs, fmt.Errorf("own[%d] (%s): skills_dir %q must be a relative path inside the repository", i, o.Repo, o.SkillsDir))
		}
	}
	seen := map[string]bool{}
	for i, v := range m.Vendor {
		where := fmt.Sprintf("vendor[%d]", i)
		if v.Name != "" {
			where = fmt.Sprintf("vendor %q", v.Name)
		}
		if err := ValidName(v.Name); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", where, err))
		}
		if v.Repo == "" {
			errs = append(errs, fmt.Errorf("%s: repo is required", where))
		}
		if !cleanRel(v.Path) {
			errs = append(errs, fmt.Errorf("%s: path %q must be a relative path inside the repository (\".\" for the root)", where, v.Path))
		}
		if !shaRe.MatchString(v.Rev) {
			errs = append(errs, fmt.Errorf("%s: rev %q must be a full 40-character lowercase commit SHA", where, v.Rev))
		}
		if seen[v.Name] && v.Name != "" {
			errs = append(errs, fmt.Errorf("%s: duplicate skill name", where))
		}
		seen[v.Name] = true
	}
	return errors.Join(errs...)
}

// ValidName reports whether name can be used as a skill directory name.
func ValidName(name string) error {
	switch {
	case name == "":
		return errors.New("name is required")
	case !nameRe.MatchString(name):
		return fmt.Errorf("name %q must match [a-z0-9][a-z0-9._-]*", name)
	case name == "synced":
		return errors.New(`name "synced" is reserved (~/.claude/skills/synced is managed by Claude)`)
	}
	return nil
}

func cleanRel(p string) bool {
	if p == "" || path.IsAbs(p) {
		return false
	}
	c := path.Clean(p)
	return c == p && c != ".." && !strings.HasPrefix(c, "../")
}

// SkillRef names one skill and where it comes from.
type SkillRef struct {
	Name   string
	Own    *Own    // set for skills from an own repository
	Vendor *Vendor // set for vendored skills
}

// CheckNames enforces M1 across own and vendor skills: ownSkills maps each
// own repository index to the skill names found in it.
func (m *Manifest) CheckNames(ownSkills map[int][]string) ([]SkillRef, error) {
	origin := map[string]string{}
	var refs []SkillRef
	var errs []error
	add := func(name, from string, ref SkillRef) {
		if prev, ok := origin[name]; ok {
			errs = append(errs, fmt.Errorf("skill %q is defined twice: %s and %s", name, prev, from))
			return
		}
		origin[name] = from
		refs = append(refs, ref)
	}
	idx := make([]int, 0, len(ownSkills))
	for i := range ownSkills {
		idx = append(idx, i)
	}
	sort.Ints(idx)
	for _, i := range idx {
		for _, name := range ownSkills[i] {
			add(name, "own "+m.Own[i].Repo, SkillRef{Name: name, Own: &m.Own[i]})
		}
	}
	for i := range m.Vendor {
		v := &m.Vendor[i]
		add(v.Name, "vendor "+v.Repo, SkillRef{Name: v.Name, Vendor: v})
	}
	for _, r := range refs {
		if m.Layout.Ignored(r.Name) {
			errs = append(errs, fmt.Errorf("skill %q matches layout.ignore; rename it or change the pattern", r.Name))
		}
	}
	sort.Slice(refs, func(a, b int) bool { return refs[a].Name < refs[b].Name })
	return refs, errors.Join(errs...)
}

// FindVendor returns the vendor entry with the given name.
func (m *Manifest) FindVendor(name string) (*Vendor, bool) {
	for i := range m.Vendor {
		if m.Vendor[i].Name == name {
			return &m.Vendor[i], true
		}
	}
	return nil, false
}

// Skipped reports the skills skipped on host.
func (m *Manifest) Skipped(host string) map[string]bool {
	out := map[string]bool{}
	if h, ok := m.Host[host]; ok {
		for _, s := range h.Skip {
			out[s] = true
		}
	}
	return out
}
