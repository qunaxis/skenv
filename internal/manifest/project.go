package manifest

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/qunaxis/skenv/internal/skenvfile"
)

// Defaults of the [project] section.
const (
	DefaultProjectDir = ".agents/skills"
	MirrorSymlink     = "symlink"
	MirrorCopy        = "copy"
)

// MirrorModes are the values of project.mirrors_mode, the default first.
var MirrorModes = []string{MirrorSymlink, MirrorCopy}

// Project is the [project] section of a skenv file: the skills a project
// repository carries, committed with it, so that everyone who clones it
// (and CI, and cloud agents) gets them. `skenv sync` inside the repository
// copies the pinned skills into dir and keeps the mirrors in line.
//
// The section is independent of [repository] and [user]: a user-level
// sync never reads it. Paths are relative to the repository root.
type Project struct {
	// Dir is the directory, relative to the repository root, that holds
	// the project's skills: the copies skenv makes and the skills authored
	// there, which skenv never changes. Default: ".agents/skills".
	Dir string `toml:"dir" yaml:"dir" json:"dir"`
	// Mirrors are other agent directories, relative to the repository
	// root, that get every skill of dir, for example ".claude/skills" for
	// Claude Code. Default: none.
	Mirrors []string `toml:"mirrors" yaml:"mirrors" json:"mirrors"`
	// MirrorsMode is how a mirror gets a skill: "symlink" makes
	// <mirror>/<name> a relative symlink to <dir>/<name>, "copy" a full
	// copy for tools that do not follow symlinks (or Windows checkouts).
	// Default: "symlink".
	MirrorsMode string `toml:"mirrors_mode" yaml:"mirrors_mode" json:"mirrors_mode"`
	// GitHosts declares git servers by alias for the repo values of this
	// section, as user.git_hosts does for the manifest: the project
	// resolves them the same for everyone who clones it, without anyone's
	// manifest.
	GitHosts Hosts `toml:"git_hosts" yaml:"git_hosts" json:"git_hosts"`
	// Dependencies are third-party skills, each pinned to a commit and
	// copied into dir, keyed by the skill name.
	Dependencies map[string]Dependency `toml:"dependencies" yaml:"dependencies" json:"dependencies"`
	// From lists skills repositories (your own, for example) to copy some
	// of their skills from, each pinned to one commit, by an ID of your
	// choice.
	From map[string]From `toml:"from" yaml:"from" json:"from"`
}

// From names skills of a skills repository, copied into the project at a
// pinned commit.
type From struct {
	// Repo is the repository: "owner/repo" or "github:owner/repo" on
	// github.com, "gitlab:group/sub/repo", "codeberg:owner/repo",
	// "<alias>:path" of a host in project.git_hosts, or a full git URL.
	Repo string `toml:"repo" yaml:"repo" json:"repo"`
	// SkillsDir is the directory inside the repository whose
	// subdirectories are the skills. Default: "skills".
	SkillsDir string `toml:"skills_dir" yaml:"skills_dir" json:"skills_dir"`
	// Skills lists the skills to copy, by directory name under skills_dir
	// (no patterns). Required: every copy is committed to the project, so
	// each is named.
	Skills []string `toml:"skills" yaml:"skills" json:"skills"`
	// Commit is the full 40-character lowercase commit SHA to copy the
	// skills at.
	Commit string `toml:"commit" yaml:"commit" json:"commit"`

	// ID is the key of the entry in project.from.
	ID string `toml:"-" yaml:"-" json:"-"`
}

// ProjectSkill is one skill that [project] copies into dir, whichever
// entry it comes from.
type ProjectSkill struct {
	Name   string
	Repo   string
	Path   string // directory with SKILL.md inside the repository
	Commit string
	// Dependency or From is the entry.
	Dependency *Dependency
	From       *From
}

// LoadProject reads and validates the [project] section of the skenv file
// at file.
func LoadProject(file string) (*Project, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	p, err := ParseProject(data, filepath.Ext(file))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	return p, nil
}

// ErrNoProject means the skenv file has no [project] section.
var ErrNoProject = errors.New("no [project] section")

// ParseProject decodes and validates the [project] section of a skenv file
// in the format of ext.
func ParseProject(data []byte, ext string) (*Project, error) {
	doc, err := skenvfile.Parse(data, ext)
	if err != nil {
		return nil, err
	}
	if !doc.Has(skenvfile.Project) {
		return nil, ErrNoProject
	}
	var p Project
	if err := doc.Decode(skenvfile.Project, &p); err != nil {
		return nil, err
	}
	if p.Dir == "" {
		p.Dir = DefaultProjectDir
	}
	if p.MirrorsMode == "" {
		p.MirrorsMode = MirrorSymlink
	}
	p.GitHosts.fillDefaults()
	for name, d := range p.Dependencies {
		d.Name = name
		if d.SkillDir == "" {
			d.SkillDir = "."
		}
		p.Dependencies[name] = d
	}
	for id, f := range p.From {
		f.ID = id
		if f.SkillsDir == "" {
			f.SkillsDir = DefaultSkillsDir
		}
		p.From[id] = f
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

// Validate checks the section: clean relative directories that do not
// overlap, full SHAs, valid skill names that are unique across vendor and
// from entries.
func (p *Project) Validate() error {
	var errs []error
	if !cleanRel(p.Dir) || p.Dir == "." {
		errs = append(errs, fmt.Errorf("project.dir %q must be a relative directory inside the repository", p.Dir))
	}
	dirs := []string{p.Dir}
	for _, m := range p.Mirrors {
		if !cleanRel(m) || m == "." {
			errs = append(errs, fmt.Errorf("project.mirrors: %q must be a relative directory inside the repository", m))
			continue
		}
		for _, d := range dirs {
			if overlaps(d, m) {
				errs = append(errs, fmt.Errorf("project.mirrors: %q overlaps %q; dir and mirrors are separate directories", m, d))
			}
		}
		dirs = append(dirs, m)
	}
	for _, err := range p.GitHosts.validate() {
		errs = append(errs, fmt.Errorf("project.%w", err))
	}
	if !slices.Contains(MirrorModes, p.MirrorsMode) {
		errs = append(errs, fmt.Errorf("project.mirrors_mode %q must be %s", p.MirrorsMode, strings.Join(quoted(MirrorModes), " or ")))
	}
	for _, d := range p.DependencyList() {
		for _, err := range checkDependency("project.dependencies", d, p.GitHosts, "") {
			errs = append(errs, projectHosts(err))
		}
	}
	for _, f := range p.FromList() {
		where := fmt.Sprintf("project.from.%s (%s)", f.ID, f.Repo)
		if !idRe.MatchString(f.ID) {
			errs = append(errs, fmt.Errorf("project.from.%s: the ID must be lowercase letters, digits, \"-\" and \"_\", starting with a letter or digit", f.ID))
		}
		if f.Repo == "" {
			errs = append(errs, fmt.Errorf("project.from.%s: repo is required", f.ID))
		} else if _, err := p.GitHosts.Resolve(f.Repo); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", where, projectHosts(err)))
		}
		if !cleanRel(f.SkillsDir) {
			errs = append(errs, fmt.Errorf("%s: skills_dir %q must be a relative path inside the repository", where, f.SkillsDir))
		}
		if len(f.Skills) == 0 {
			errs = append(errs, fmt.Errorf("%s: skills is required; list the skills to copy", where))
		}
		seen := map[string]bool{}
		for _, n := range f.Skills {
			if err := ValidName(n); err != nil {
				errs = append(errs, fmt.Errorf("%s: skills: %w", where, err))
			} else if seen[n] {
				errs = append(errs, fmt.Errorf("%s: skills lists %q twice", where, n))
			}
			seen[n] = true
		}
		if !shaRe.MatchString(f.Commit) {
			errs = append(errs, fmt.Errorf("%s: commit %q must be a full 40-character lowercase commit SHA", where, f.Commit))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	origin := map[string]string{}
	for _, s := range p.Skills() {
		from := "dependency " + s.Name + " (" + s.Repo + ")"
		if s.From != nil {
			from = "from." + s.From.ID + " (" + s.Repo + ")"
		}
		if prev, ok := origin[s.Name]; ok {
			errs = append(errs, fmt.Errorf("project skill %q is defined twice: %s and %s", s.Name, prev, from))
		}
		origin[s.Name] = from
	}
	return errors.Join(errs...)
}

// projectHosts points an error of Hosts.Resolve at project.hosts, where a
// project declares its hosts.
func projectHosts(err error) error {
	msg := strings.ReplaceAll(err.Error(), "[user.git_hosts.", "[project.git_hosts.")
	if msg == err.Error() {
		return err
	}
	return errors.New(msg)
}

// checkDependency validates one dependency of [user] or [project] (in
// section where): the name, a repo that hosts resolve (a local path
// against base), the skill directory and a full SHA.
func checkDependency(where string, d *Dependency, hosts Hosts, base string) []error {
	var errs []error
	where = fmt.Sprintf("%s.%s", where, quoteKey(d.Name))
	if err := ValidName(d.Name); err != nil {
		errs = append(errs, fmt.Errorf("%s: %w", where, err))
	}
	if d.Repo == "" {
		errs = append(errs, fmt.Errorf("%s: repo is required", where))
	} else if _, err := hosts.ResolveIn(base, d.Repo); err != nil {
		errs = append(errs, fmt.Errorf("%s: %w", where, err))
	}
	if !cleanRel(d.SkillDir) {
		errs = append(errs, fmt.Errorf("%s: skill_dir %q must be a relative path inside the repository (\".\" for the root)", where, d.SkillDir))
	}
	if !shaRe.MatchString(d.Commit) {
		errs = append(errs, fmt.Errorf("%s: commit %q must be a full 40-character lowercase commit SHA", where, d.Commit))
	}
	return errs
}

// overlaps reports whether one of the clean relative paths a and b is
// the other or inside it.
func overlaps(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

func quoted(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = fmt.Sprintf("%q", s)
	}
	return out
}

// DependencyList returns the dependencies sorted by name.
func (p *Project) DependencyList() []*Dependency {
	out := make([]*Dependency, 0, len(p.Dependencies))
	for _, name := range slices.Sorted(maps.Keys(p.Dependencies)) {
		d := p.Dependencies[name]
		out = append(out, &d)
	}
	return out
}

// FromList returns the from entries sorted by ID.
func (p *Project) FromList() []*From {
	out := make([]*From, 0, len(p.From))
	for _, id := range slices.Sorted(maps.Keys(p.From)) {
		f := p.From[id]
		out = append(out, &f)
	}
	return out
}

// Skills lists every skill the section copies, sorted by name. A name
// defined twice appears twice; Validate reports it.
func (p *Project) Skills() []ProjectSkill {
	var out []ProjectSkill
	for _, d := range p.DependencyList() {
		out = append(out, ProjectSkill{Name: d.Name, Repo: d.Repo, Path: d.SkillDir, Commit: d.Commit, Dependency: d})
	}
	for _, f := range p.FromList() {
		for _, n := range f.Skills {
			out = append(out, ProjectSkill{Name: n, Repo: f.Repo, Path: path.Join(f.SkillsDir, n), Commit: f.Commit, From: f})
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Name < out[b].Name })
	return out
}

// Skill returns the skill name of the section.
func (p *Project) Skill(name string) (ProjectSkill, bool) {
	for _, s := range p.Skills() {
		if s.Name == name {
			return s, true
		}
	}
	return ProjectSkill{}, false
}
