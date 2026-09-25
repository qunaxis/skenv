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
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/qunaxis/skenv/internal/skenvfile"
	"github.com/qunaxis/skenv/internal/skillname"
)

// DefaultSkillsDir is used when an [[own]] entry does not set skills_dir.
const DefaultSkillsDir = "skills"

// Manifest is the [environment] section of the skenv file: the skills every
// machine that runs `skenv sync` should have. Paths may start with "~".
//
// Paths are kept as written; callers expand them against the home
// directory. The first paragraph of the doc comments of these types and
// the comments of their fields are the descriptions of the JSON Schema
// (`make schemas`): write them for users.
type Manifest struct {
	// Layout is where skills are stored and linked.
	Layout Layout `toml:"layout" yaml:"layout" json:"layout"`
	// Hosts declares git servers by alias, usually self-hosted ones:
	// "<alias>:group/repo" in repo is a repository on that server. They
	// live in the manifest so it resolves the same on every machine.
	Hosts Hosts `toml:"hosts" yaml:"hosts" json:"hosts"`
	// Own lists your skills repositories, kept as git working copies: every
	// skill directory in them is linked.
	Own []Own `toml:"own" yaml:"own" json:"own"`
	// Vendor lists third-party skills, each pinned to a commit and copied
	// into the store. Skill names are unique across own and vendor skills.
	Vendor []Vendor `toml:"vendor" yaml:"vendor" json:"vendor"`
	// Host holds per-machine overrides, keyed by the full or the short
	// hostname.
	Host map[string]Host `toml:"host" yaml:"host" json:"host"`
}

// Layout is where skills are stored and linked.
type Layout struct {
	// Store is the directory that holds every skill: links to own skills,
	// copies of vendored ones. Codex reads it directly. Default:
	// "~/.agents/skills".
	Store string `toml:"store" yaml:"store" json:"store"`
	// Targets are the agent directories that get a link per skill. When set
	// (even to an empty list) it replaces the built-in table: Claude Code
	// ($CLAUDE_CONFIG_DIR/skills, else ~/.claude/skills) and pi
	// (~/.pi/agent/skills), each only if the agent is installed.
	Targets []string `toml:"targets" yaml:"targets" json:"targets"`
	// Ignore lists glob patterns over entry names (no "/") in the store and
	// the agent directories that belong to other tools: doctor does not
	// report them and sync never touches them, not even with --adopt. A
	// manifest skill must not match a pattern.
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

// Own is a skills repository of yours, kept as a git working copy so that
// edits show up immediately.
type Own struct {
	// Repo is the repository to clone: "owner/repo" on github.com,
	// "gitlab:group/sub/repo", "codeberg:owner/repo", "<alias>:path" of a
	// host in hosts, or a full git URL.
	Repo string `toml:"repo" yaml:"repo" json:"repo"`
	// Path is where the working copy lives ("~" allowed). Point it at the
	// existing clone, or sync clones a second one there.
	Path string `toml:"path" yaml:"path" json:"path"`
	// SkillsDir is the directory inside the repository whose
	// subdirectories with a SKILL.md are the skills. Default: "skills".
	SkillsDir string `toml:"skills_dir" yaml:"skills_dir" json:"skills_dir"`
	// Skills lists the skills of this repository to install; without it
	// every skill is installed, and a skill added to the repository later
	// is too. A name that is not a skill of the repository is an error. It
	// must not be empty: remove the entry instead.
	Skills []string `toml:"skills" yaml:"skills" json:"skills"`
	// Exclude lists glob patterns over skill names (no "/") that are not
	// installed, applied after skills; host.<name>.skip applies after both.
	// A pattern that matches nothing is fine.
	Exclude []string `toml:"exclude" yaml:"exclude" json:"exclude"`
}

// Select returns the names in found (the skills of the repository) that o
// installs: skills (all when unset), minus exclude. A name in skills that
// is not in found is an error.
func (o *Own) Select(found []string) ([]string, error) {
	in := make(map[string]bool, len(found))
	for _, n := range found {
		in[n] = true
	}
	var missing []string
	for _, n := range o.Skills {
		if !in[n] {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("own %s: skills lists %s, not found in %s/%s (a directory with SKILL.md)",
			o.Repo, quoteAll(missing), o.Path, o.SkillsDir)
	}
	var out []string
	for _, n := range found {
		if o.Selects(n) {
			out = append(out, n)
		}
	}
	return out, nil
}

// Excluded reports whether name matches a pattern of exclude.
func (o *Own) Excluded(name string) bool {
	for _, pat := range o.Exclude {
		if ok, _ := path.Match(pat, name); ok {
			return true
		}
	}
	return false
}

// Selects reports whether o installs the skill name of its repository.
func (o *Own) Selects(name string) bool {
	return (o.Skills == nil || slices.Contains(o.Skills, name)) && !o.Excluded(name)
}

func quoteAll(names []string) string {
	q := make([]string, len(names))
	for i, n := range names {
		q[i] = strconv.Quote(n)
	}
	return strings.Join(q, ", ")
}

// Vendor is a third-party skill pinned to a commit; `skenv vendor
// add|update|remove` edit these entries.
type Vendor struct {
	// Name is the skill name: 1 to 64 lowercase letters, digits and single
	// hyphens, with no hyphen at the start or end; "synced" is reserved.
	Name string `toml:"name" yaml:"name" json:"name"`
	// Repo is the repository: "owner/repo" on github.com,
	// "gitlab:group/sub/repo", "codeberg:owner/repo", "<alias>:path" of a
	// host in hosts, or a full git URL.
	Repo string `toml:"repo" yaml:"repo" json:"repo"`
	// Path is the directory with SKILL.md inside the repository, relative
	// to its root; "." for the root. Default: ".".
	Path string `toml:"path" yaml:"path" json:"path"`
	// Rev is the full 40-character lowercase commit SHA. Branches, tags and
	// short SHAs are rejected: a vendored skill changes only when you update
	// it.
	Rev string `toml:"rev" yaml:"rev" json:"rev"`
}

// Host holds the overrides of one machine.
type Host struct {
	// Skip lists skills that are neither stored nor linked on this host.
	Skip []string `toml:"skip" yaml:"skip" json:"skip"`
}

var (
	shaRe = regexp.MustCompile(RevPattern)
)

// RevPattern is a full lowercase commit SHA (M2).
const RevPattern = `^[0-9a-f]{40}$`

// Reserved is the skill name that skenv never uses:
// ~/.claude/skills/synced is managed by Claude.
const Reserved = "synced"

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
	for a, h := range m.Hosts {
		if h.Type == "" {
			h.Type = TypeGeneric
			m.Hosts[a] = h
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
	errs = append(errs, m.Hosts.validate()...)
	for i, o := range m.Own {
		if o.Repo == "" {
			errs = append(errs, fmt.Errorf("own[%d]: repo is required", i))
		} else if _, err := m.Hosts.Resolve(o.Repo); err != nil {
			errs = append(errs, fmt.Errorf("own[%d]: %w", i, err))
		}
		if o.Path == "" {
			errs = append(errs, fmt.Errorf("own[%d] (%s): path is required", i, o.Repo))
		}
		if !cleanRel(o.SkillsDir) {
			errs = append(errs, fmt.Errorf("own[%d] (%s): skills_dir %q must be a relative path inside the repository", i, o.Repo, o.SkillsDir))
		}
		if o.Skills != nil && len(o.Skills) == 0 {
			errs = append(errs, fmt.Errorf("own[%d] (%s): skills is empty; list the skills to install, or remove the entry (without skills, every skill is installed)", i, o.Repo))
		}
		seenSkill := map[string]bool{}
		for _, n := range o.Skills {
			if err := ValidName(n); err != nil {
				errs = append(errs, fmt.Errorf("own[%d] (%s): skills: %w", i, o.Repo, err))
			} else if seenSkill[n] {
				errs = append(errs, fmt.Errorf("own[%d] (%s): skills lists %q twice", i, o.Repo, n))
			}
			seenSkill[n] = true
		}
		for _, pat := range o.Exclude {
			if _, err := path.Match(pat, ""); err != nil || pat == "" || strings.Contains(pat, "/") {
				errs = append(errs, fmt.Errorf("own[%d] (%s): exclude: %q must be a glob over skill names (no \"/\")", i, o.Repo, pat))
			}
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
		} else if _, err := m.Hosts.Resolve(v.Repo); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", where, err))
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

// ValidName reports whether name can be used as a skill name: the shared
// rule of package skillname, and not Reserved.
func ValidName(name string) error {
	if err := skillname.Check(name); err != nil {
		return err
	}
	if name == Reserved {
		return fmt.Errorf("name %q is reserved (~/.claude/skills/%s is managed by Claude)", Reserved, Reserved)
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
